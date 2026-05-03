package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// Trace replay (P3-03 / mootd-admin#26).
//
// "Take a real production trace and re-run it" — the money-shot
// feature for prompt iteration. v1 scope:
//
//   - Replay uses the **archived prompt** from the trace
//     (systemPrompt + userMessage stamped onto the llm_calls
//     row by mootd@ef7461e — P1-11 / mootd-admin#16). No
//     reconstruction from wardrobe + weather; the prompt
//     itself is the input.
//   - Calls Anthropic directly when the trace's provider was
//     anthropic. Other providers (openai / ollama) return a
//     400 — broaden later if needed; today the only eval-
//     valuable replays are against Claude.
//   - Writes a new llm_calls row with replayOf = original
//     trace id. The trace-detail panel shows replay history
//     and lets the admin diff prompts/responses/cost/latency.
//
// Deferred from the issue's full scope:
//   - Per-replay model override (would need provider-specific
//     model routing logic; deferred)
//   - Per-replay prompt-version override (would interact with
//     P3-01's templates; admins can promote a draft and
//     replay against the new prod instead, which solves the
//     same problem)
// ────────────────────────────────────────────────────────────────────

// replayTrace is the POST /admin/v1/traces/{id}/replay handler.
func (h *Handler) replayTrace(w http.ResponseWriter, r *http.Request, traceID string) {
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	// 1. Load the original trace.
	original, err := h.tracesRepo.FindDetail(ctx, traceID)
	if err != nil {
		h.logger.Printf("admin /traces/%s/replay: load: %v", traceID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if original == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "trace not found"})
		return
	}
	if original.SystemPrompt == "" || original.UserMessage == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "trace predates archived-prompt rollout (mootd@ef7461e); cannot replay",
		})
		return
	}
	if original.Provider != "anthropic" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "replay v1 supports anthropic-provider traces only (was: " + original.Provider + ")",
		})
		return
	}

	// 2. Direct Anthropic call. Same envelope as the eval
	//    judge — minimal HTTP + JSON parsing, no streaming.
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ANTHROPIC_API_KEY not set"})
		return
	}
	model := original.Model
	if model == "" {
		model = os.Getenv("ANTHROPIC_MODEL")
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	startedAt := time.Now()
	respBody, usage, err := callAnthropicReplay(ctx, apiKey, model, original.SystemPrompt, original.UserMessage)
	endedAt := time.Now()
	if err != nil {
		h.logger.Printf("admin /traces/%s/replay: anthropic call: %v", traceID, err)
		response.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "anthropic call failed: " + err.Error()})
		return
	}

	// 3. Persist a new llm_calls row with replayOf set.
	adminID, _ := AdminIDFromContext(r.Context())
	newID := "replay_" + generateAuditID()[len("aud_"):]
	row := bson.M{
		"_id":              newID,
		"traceId":          newID,
		"userId":           original.UserID,
		"provider":         "anthropic",
		"model":            model,
		"feature":          original.Feature,
		"inputTokens":      usage.InputTokens,
		"outputTokens":     usage.OutputTokens,
		"cacheReadTokens":  0,
		"cacheWriteTokens": 0,
		"durationMs":       endedAt.Sub(startedAt).Milliseconds(),
		"status":           "success",
		"costUsd":          approxAnthropicCostUSD(model, usage.InputTokens, usage.OutputTokens),
		"promptVersion":    original.PromptVersion,
		"createdAt":        startedAt.UTC(),
		"systemPrompt":     original.SystemPrompt,
		"userMessage":      original.UserMessage,
		"responseRaw":      respBody,
		"wardrobeItemIds":  original.WardrobeItemIDs,
		"replayOf":         traceID,
	}
	llmCol := h.client().Collection("llm_calls")
	if _, err := llmCol.InsertOne(ctx, row); err != nil {
		h.logger.Printf("admin /traces/%s/replay: persist: %v", traceID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// 4. Audit.
	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "trace.replay",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"originalTraceId": traceID,
				"newTraceId":      newID,
				"model":           model,
			},
		})
	}

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"traceId":  newID,
		"replayOf": traceID,
		"model":    model,
	})
}

// client returns the underlying Mongo client. The trace replay
// handler reaches into llm_calls directly because the admin
// repos today expose only read methods; threading a write path
// through an interface for one feature would require ceremony
// that doesn't pay off until a second writer appears.
func (h *Handler) client() *mongo.Database {
	// Repos store the client + dbName; pull from one we know is
	// wired (the audit one — guaranteed present whenever
	// h.repo is). Defensive nil check belongs at the call site.
	if r, ok := h.repo.(*MongoRepository); ok {
		return r.client.Database(r.dbName)
	}
	return nil
}

type anthropicReplayUsage struct {
	InputTokens  int
	OutputTokens int
}

// callAnthropicReplay POSTs the archived prompt to Anthropic
// and returns the raw text response + token counts. Mirrors
// the AnthropicJudge envelope in evals.go so the two stay
// roughly in sync.
func callAnthropicReplay(ctx context.Context, apiKey, model, systemPrompt, userMessage string) (string, anthropicReplayUsage, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages": []map[string]any{
			{"role": "user", "content": userMessage},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", anthropicReplayUsage{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", anthropicReplayUsage{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", anthropicReplayUsage{}, fmt.Errorf("status=%d body=%s", resp.StatusCode, truncateForLog(raw, 200))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", anthropicReplayUsage{}, fmt.Errorf("decode: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", anthropicReplayUsage{}, fmt.Errorf("empty content")
	}
	// Concatenate all text blocks (Claude can emit several).
	var sb strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), anthropicReplayUsage{
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
	}, nil
}
