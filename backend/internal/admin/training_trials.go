package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// Training-trial review records (singleItemDetection #36, multi-item).
//
// The orchestrator owns the per-trial describer output (jobs +
// structured descriptions); see training_proxy.go. What the
// orchestrator does NOT have is a list of trials or a notion of
// "this trial's review is finished" — trial ids are minted client-
// side and only known to the orchestrator once a job is POSTed.
//
// So mootd-admin keeps its own lightweight `training_trials`
// collection: one row per trial an admin started, recording who/when,
// the review status, and (on submit) the winning pick per attribute.
// This is what powers the training list page and the "Submit review"
// action. The picks themselves are still written to the orchestrator's
// lockedAttributes (via the shared /items/{id}/attributes patch) — this
// row is the admin-side index + audit trail over them.
// ────────────────────────────────────────────────────────────────────

const (
	TrainingStatusInReview  = "in_review"
	TrainingStatusSubmitted = "submitted"
)

// Training-record provenance (mootd-admin#126). Trials an admin ran
// manually carry no source (treated as "trial"); records auto-captured
// from HITL attribute corrections carry "hitl" — excluded from the
// manual review list but included in exports.
const (
	TrainingSourceTrial = "trial"
	TrainingSourceHITL  = "hitl"
)

// TrainingTrial is one row in the training_trials collection. The _id
// is the client-minted trial id (also the key the orchestrator poll
// uses), so create is naturally idempotent on it.
type TrainingTrial struct {
	ID          string            `bson:"_id" json:"trialId"`
	Label       string            `bson:"label,omitempty" json:"label,omitempty"`
	Status      string            `bson:"status" json:"status"`
	CreatedBy   string            `bson:"createdBy" json:"createdBy"`
	CreatedAt   time.Time         `bson:"createdAt" json:"createdAt"`
	SubmittedBy string            `bson:"submittedBy,omitempty" json:"submittedBy,omitempty"`
	SubmittedAt *time.Time        `bson:"submittedAt,omitempty" json:"submittedAt,omitempty"`
	Picks       map[string]string `bson:"picks,omitempty" json:"picks,omitempty"`
	// CustomValues holds the human-entered override text for the
	// "custom" picks (path → value). The marker in Picks is "custom";
	// the actual text lives here so reopening a submitted trial shows
	// exactly what was typed without re-reading the orchestrator.
	CustomValues map[string]string `bson:"customValues,omitempty" json:"customValues,omitempty"`
	PickCount    int               `bson:"pickCount" json:"pickCount"`
	AttrCount    int               `bson:"attrCount" json:"attrCount"`

	// ── Training-data snapshot (Phase 1, mootd-admin#124) ──────────
	// The picks above record *which side won* each attribute; on their
	// own they can't reconstruct a (chosen, rejected) training pair —
	// the actual values lived only on the orchestrator, which mutates
	// them on submit and has no retention guarantee we control. So we
	// snapshot both describers' full structured descriptions here at
	// submit time. With these + Picks + CustomValues, Phase 2's export
	// can materialise DPO/SFT pairs entirely offline, never re-reading
	// the orchestrator. All omitempty: trials submitted before this
	// feature simply carry no snapshot.
	ClaudeDescription map[string]any `bson:"claudeDescription,omitempty" json:"claudeDescription,omitempty"`
	GemmaDescription  map[string]any `bson:"gemmaDescription,omitempty" json:"gemmaDescription,omitempty"`
	SourceImageURL    string         `bson:"sourceImageUrl,omitempty" json:"sourceImageUrl,omitempty"`
	ClaudeRequestID   string         `bson:"claudeRequestId,omitempty" json:"claudeRequestId,omitempty"`
	GemmaRequestID    string         `bson:"gemmaRequestId,omitempty" json:"gemmaRequestId,omitempty"`
	// Source provenance: empty/"trial" = manually run; "hitl" =
	// auto-captured from a HITL attribute correction (#126).
	Source string `bson:"source,omitempty" json:"source,omitempty"`

	// ── Label quality (Phase 4, mootd-admin#127) ───────────────────
	// When a second admin re-reviews a submitted trial, we keep the
	// first review's picks canonical and record how much the two
	// reviewers agreed — a label-noise signal exports can threshold on.
	ReviewCount    int      `bson:"reviewCount,omitempty" json:"reviewCount,omitempty"`
	Agreement      *float64 `bson:"agreement,omitempty" json:"agreement,omitempty"`
	SecondReviewer string   `bson:"secondReviewer,omitempty" json:"secondReviewer,omitempty"`
}

// TrainingSubmitInput bundles everything a submit records. Carrying the
// snapshot fields (see TrainingTrial) alongside the picks keeps the
// repository signature stable as the captured provenance grows.
type TrainingSubmitInput struct {
	Picks             map[string]string
	CustomValues      map[string]string
	AttrCount         int
	ClaudeDescription map[string]any
	GemmaDescription  map[string]any
	SourceImageURL    string
	ClaudeRequestID   string
	GemmaRequestID    string
	Source            string
}

// TrainingTrialQuery filters the list endpoint. Empty status returns
// every trial; cursor pages on _id descending (trial ids embed a
// base36 millisecond timestamp, so _id-desc is newest-first).
type TrainingTrialQuery struct {
	Status string
	Cursor string
	Limit  int
}

// TrainingTrialPage is the wire shape for GET /admin/v1/training/submissions.
type TrainingTrialPage struct {
	Trials     []TrainingTrial `json:"trials"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

// TrainingTrialsRepository is the persistence contract for the review
// records. Wired as an optional dependency (see WithTrainingTrials) so
// the in-memory test repo doesn't have to satisfy it — same pattern as
// the other Handler optionals.
type TrainingTrialsRepository interface {
	CreateTrainingTrial(ctx context.Context, t TrainingTrial) error
	ListTrainingTrials(ctx context.Context, q TrainingTrialQuery) ([]TrainingTrial, string, error)
	GetTrainingTrial(ctx context.Context, id string) (*TrainingTrial, error)
	SubmitTrainingTrial(ctx context.Context, id, submittedBy string, in TrainingSubmitInput, at time.Time) (*TrainingTrial, error)
	// StreamSubmittedTrainingTrials yields submitted trials in
	// submittedAt order (oldest first, so an incremental export with
	// `since` is stable), invoking fn per row. Stops after max rows
	// (0 = unbounded) or when fn returns an error. Powers the export
	// endpoint (mootd-admin#125) — streamed, not materialised, so a
	// large corpus can't OOM the box.
	StreamSubmittedTrainingTrials(ctx context.Context, since time.Time, max int, fn func(TrainingTrial) error) (int, error)
	// RecordReReview stores a second reviewer's agreement against the
	// canonical (first) review WITHOUT touching its picks (Phase 4,
	// mootd-admin#127). Returns the updated record.
	RecordReReview(ctx context.Context, id, secondReviewer string, agreement float64, at time.Time) (*TrainingTrial, error)
}

// pickAgreement scores how much two reviewers agreed: the fraction of
// attribute paths (the union of both reviewers' picks) where they chose
// the same kind — and, for "custom", the same typed value. A path only
// one reviewer touched counts as a disagreement. Empty on both sides is
// full agreement (1.0). Used for the label-noise metric exports can
// threshold on.
func pickAgreement(picksA, customA, picksB, customB map[string]string) float64 {
	paths := map[string]struct{}{}
	for p := range picksA {
		paths[p] = struct{}{}
	}
	for p := range picksB {
		paths[p] = struct{}{}
	}
	if len(paths) == 0 {
		return 1
	}
	matches := 0
	for p := range paths {
		ka, oka := picksA[p]
		kb, okb := picksB[p]
		if !oka || !okb || ka != kb {
			continue
		}
		if ka == "custom" && customA[p] != customB[p] {
			continue
		}
		matches++
	}
	return float64(matches) / float64(len(paths))
}

// ── MongoRepository implementation ──────────────────────────────────

func (r *MongoRepository) trainingTrialsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("training_trials")
}

// ensureTrainingTrialIndexes declares the (status, _id) index the list
// query rides. Idempotent — called from ensureIndexes at startup.
func (r *MongoRepository) ensureTrainingTrialIndexes(ctx context.Context) error {
	_, err := r.trainingTrialsCol().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}, {Key: "_id", Value: -1}},
		Options: options.Index().SetName("training_trials_status_id"),
	})
	return err
}

func (r *MongoRepository) CreateTrainingTrial(ctx context.Context, t TrainingTrial) error {
	_, err := r.trainingTrialsCol().InsertOne(ctx, t)
	return err
}

func (r *MongoRepository) GetTrainingTrial(ctx context.Context, id string) (*TrainingTrial, error) {
	var doc TrainingTrial
	err := r.trainingTrialsCol().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (r *MongoRepository) ListTrainingTrials(ctx context.Context, q TrainingTrialQuery) ([]TrainingTrial, string, error) {
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	filter := bson.M{}
	// The manual review list excludes HITL-ingested records (#126):
	// they're auto-captured and already reviewed, so they'd bury the
	// trials an admin actually started. $ne also matches docs with no
	// source field (the manual trials). Exports read every source.
	filter["source"] = bson.M{"$ne": TrainingSourceHITL}
	if q.Status != "" {
		filter["status"] = q.Status
	}
	if q.Cursor != "" {
		// _id desc ordering → "next page" is ids strictly below the
		// last one returned.
		filter["_id"] = bson.M{"$lt": q.Cursor}
	}

	cur, err := r.trainingTrialsCol().Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetLimit(int64(limit+1))) // +1 to detect a next page
	if err != nil {
		return nil, "", err
	}
	var all []TrainingTrial
	if err := cur.All(ctx, &all); err != nil {
		return nil, "", err
	}
	next := ""
	if len(all) > limit {
		next = all[limit-1].ID
		all = all[:limit]
	}
	return all, next, nil
}

// SubmitTrainingTrial flips a trial to submitted, recording the picks
// snapshot + reviewer. Upserts: a trial created before this feature
// (or one whose create call was lost) is still submittable — the row
// is created with createdBy/createdAt from the submit if missing.
func (r *MongoRepository) SubmitTrainingTrial(ctx context.Context, id, submittedBy string, in TrainingSubmitInput, at time.Time) (*TrainingTrial, error) {
	picks := in.Picks
	if picks == nil {
		picks = map[string]string{}
	}
	customValues := in.CustomValues
	if customValues == nil {
		customValues = map[string]string{}
	}
	set := bson.M{
		"status":       TrainingStatusSubmitted,
		"submittedBy":  submittedBy,
		"submittedAt":  at,
		"picks":        picks,
		"customValues": customValues,
		"pickCount":    len(picks),
		"attrCount":    in.AttrCount,
	}
	// Only write snapshot fields the caller actually supplied, so a
	// re-submit without a snapshot can't blank an earlier one.
	if in.ClaudeDescription != nil {
		set["claudeDescription"] = in.ClaudeDescription
	}
	if in.GemmaDescription != nil {
		set["gemmaDescription"] = in.GemmaDescription
	}
	if in.SourceImageURL != "" {
		set["sourceImageUrl"] = in.SourceImageURL
	}
	if in.ClaudeRequestID != "" {
		set["claudeRequestId"] = in.ClaudeRequestID
	}
	if in.GemmaRequestID != "" {
		set["gemmaRequestId"] = in.GemmaRequestID
	}
	if in.Source != "" {
		set["source"] = in.Source
	}
	update := bson.M{
		"$set": set,
		"$setOnInsert": bson.M{
			"createdBy":   submittedBy,
			"createdAt":   at,
			"reviewCount": 1,
		},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)
	var doc TrainingTrial
	err := r.trainingTrialsCol().FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// RecordReReview records a second reviewer's agreement against the
// canonical first review. It deliberately leaves picks/customValues and
// the original submittedBy untouched — the first review stays the gold
// the export reconstructs from; this only annotates label quality.
func (r *MongoRepository) RecordReReview(ctx context.Context, id, secondReviewer string, agreement float64, at time.Time) (*TrainingTrial, error) {
	update := bson.M{
		"$set": bson.M{
			"agreement":      agreement,
			"secondReviewer": secondReviewer,
			"reviewCount":    2,
			"updatedAt":      at,
		},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc TrainingTrial
	err := r.trainingTrialsCol().FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// StreamSubmittedTrainingTrials iterates submitted trials oldest-first
// (submittedAt asc, _id asc as tiebreak) so an incremental export keyed
// on `since` is deterministic. Rows are decoded one at a time off the
// cursor — memory stays flat regardless of corpus size.
func (r *MongoRepository) StreamSubmittedTrainingTrials(ctx context.Context, since time.Time, max int, fn func(TrainingTrial) error) (int, error) {
	filter := bson.M{"status": TrainingStatusSubmitted}
	if !since.IsZero() {
		filter["submittedAt"] = bson.M{"$gte": since}
	}
	opts := options.Find().SetSort(bson.D{{Key: "submittedAt", Value: 1}, {Key: "_id", Value: 1}})
	if max > 0 {
		opts.SetLimit(int64(max))
	}
	cur, err := r.trainingTrialsCol().Find(ctx, filter, opts)
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	n := 0
	for cur.Next(ctx) {
		var t TrainingTrial
		if err := cur.Decode(&t); err != nil {
			return n, err
		}
		if err := fn(t); err != nil {
			return n, err
		}
		n++
	}
	return n, cur.Err()
}

// ── HTTP handlers ───────────────────────────────────────────────────

// trainingTrialsReady gates the submissions endpoints on the store
// being wired (graceful 503, mirroring hitlReady).
func (h *Handler) trainingTrialsReady(w http.ResponseWriter) bool {
	if h.trainingTrials == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "training trials store not wired",
		})
		return false
	}
	return true
}

// TrainingSubmissionsRouter dispatches /admin/v1/training/submissions[/{id}[/submit]].
// Route-level auth is traces:read (the list + detail are reads); the
// mutating create + submit gate inline on traces:rerun, matching the
// process POST.
func (h *Handler) TrainingSubmissionsRouter(w http.ResponseWriter, r *http.Request, tail string) {
	if !h.trainingTrialsReady(w) {
		return
	}
	if tail == "" {
		switch r.Method {
		case http.MethodGet:
			h.listTrainingTrials(w, r)
		case http.MethodPost:
			h.createTrainingTrial(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	idPart, action := tail, ""
	if idx := strings.Index(tail, "/"); idx >= 0 {
		idPart, action = tail[:idx], tail[idx+1:]
	}
	id, err := url.PathUnescape(idPart)
	if err != nil || id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trial id"})
		return
	}

	switch action {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getTrainingTrial(w, r, id)
	case "submit":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.submitTrainingTrial(w, r, id)
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown training submission action"})
	}
}

func (h *Handler) listTrainingTrials(w http.ResponseWriter, r *http.Request) {
	q := TrainingTrialQuery{
		Status: r.URL.Query().Get("status"),
		Cursor: r.URL.Query().Get("cursor"),
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			q.Limit = n
		}
	}
	trials, next, err := h.trainingTrials.ListTrainingTrials(r.Context(), q)
	if err != nil {
		h.logger.Printf("admin training: list trials: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	if trials == nil {
		trials = []TrainingTrial{}
	}
	response.WriteJSON(w, http.StatusOK, TrainingTrialPage{Trials: trials, NextCursor: next})
}

func (h *Handler) createTrainingTrial(w http.ResponseWriter, r *http.Request) {
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}
	var body struct {
		TrialID string `json:"trialId"`
		Label   string `json:"label"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	body.TrialID = strings.TrimSpace(body.TrialID)
	if body.TrialID == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "trialId required"})
		return
	}
	adminID, _ := AdminIDFromContext(r.Context())
	rec := TrainingTrial{
		ID:        body.TrialID,
		Label:     strings.TrimSpace(body.Label),
		Status:    TrainingStatusInReview,
		CreatedBy: adminID,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.trainingTrials.CreateTrainingTrial(r.Context(), rec); err != nil {
		// Idempotent create: a duplicate trial id just returns the
		// existing record rather than erroring (the FE may retry).
		if existing, gErr := h.trainingTrials.GetTrainingTrial(r.Context(), body.TrialID); gErr == nil && existing != nil {
			response.WriteJSON(w, http.StatusOK, existing)
			return
		}
		h.logger.Printf("admin training: create trial %s: %v", body.TrialID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create failed"})
		return
	}
	h.auditTrainingAction(r, "training.trial.create", body.TrialID, map[string]any{"label": rec.Label})
	response.WriteJSON(w, http.StatusCreated, rec)
}

func (h *Handler) getTrainingTrial(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := h.trainingTrials.GetTrainingTrial(r.Context(), id)
	if err != nil {
		h.logger.Printf("admin training: get trial %s: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "lookup failed"})
		return
	}
	if rec == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "trial not found"})
		return
	}
	response.WriteJSON(w, http.StatusOK, rec)
}

func (h *Handler) submitTrainingTrial(w http.ResponseWriter, r *http.Request, id string) {
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}
	var body struct {
		Picks        map[string]string `json:"picks"`
		CustomValues map[string]string `json:"customValues"`
		AttrCount    int               `json:"attrCount"`
		// Phase 1 snapshot (mootd-admin#124) — optional so older
		// clients still submit, but sent by the current FE so the
		// record is self-contained training data.
		ClaudeDescription map[string]any `json:"claudeDescription"`
		GemmaDescription  map[string]any `json:"gemmaDescription"`
		SourceImageURL    string         `json:"sourceImageUrl"`
		ClaudeRequestID   string         `json:"claudeRequestId"`
		GemmaRequestID    string         `json:"gemmaRequestId"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	adminID, _ := AdminIDFromContext(r.Context())

	// Re-review path (Phase 4, mootd-admin#127): if the trial is already
	// submitted by a DIFFERENT admin, this is a second reviewer. Record
	// the pick agreement against the canonical first review instead of
	// overwriting it — the first review stays the gold the export uses.
	if existing, gErr := h.trainingTrials.GetTrainingTrial(r.Context(), id); gErr == nil &&
		existing != nil && existing.Status == TrainingStatusSubmitted &&
		existing.SubmittedBy != "" && existing.SubmittedBy != adminID {
		agreement := pickAgreement(existing.Picks, existing.CustomValues, body.Picks, body.CustomValues)
		rec, err := h.trainingTrials.RecordReReview(r.Context(), id, adminID, agreement, time.Now().UTC())
		if err != nil || rec == nil {
			h.logger.Printf("admin training: re-review trial %s: %v", id, err)
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "re-review failed"})
			return
		}
		h.auditTrainingAction(r, "training.trial.rereview", id, map[string]any{
			"agreement": agreement,
			"pickCount": len(body.Picks),
		})
		response.WriteJSON(w, http.StatusOK, rec)
		return
	}

	rec, err := h.trainingTrials.SubmitTrainingTrial(r.Context(), id, adminID, TrainingSubmitInput{
		Picks:             body.Picks,
		CustomValues:      body.CustomValues,
		AttrCount:         body.AttrCount,
		ClaudeDescription: body.ClaudeDescription,
		GemmaDescription:  body.GemmaDescription,
		SourceImageURL:    body.SourceImageURL,
		ClaudeRequestID:   body.ClaudeRequestID,
		GemmaRequestID:    body.GemmaRequestID,
	}, time.Now().UTC())
	if err != nil {
		h.logger.Printf("admin training: submit trial %s: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "submit failed"})
		return
	}
	h.auditTrainingAction(r, "training.trial.submit", id, map[string]any{
		"pickCount":   len(body.Picks),
		"customCount": len(body.CustomValues),
		"attrCount":   body.AttrCount,
		"snapshot":    body.ClaudeDescription != nil || body.GemmaDescription != nil,
	})
	response.WriteJSON(w, http.StatusOK, rec)
}

// auditTrainingAction writes one admin_audit row per training mutation.
// Best-effort, same contract as auditHitlAction.
func (h *Handler) auditTrainingAction(r *http.Request, action, trialID string, meta map[string]any) {
	if h.repo == nil {
		return
	}
	adminID, _ := AdminIDFromContext(r.Context())
	var adminEmail string
	if a, _ := h.repo.FindByID(r.Context(), adminID); a != nil {
		adminEmail = a.Email
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["trialId"] = trialID
	Audit(r.Context(), h.repo, h.logger, AuditEntry{
		ID:           generateAuditID(),
		AdminID:      adminID,
		AdminEmail:   adminEmail,
		Action:       action,
		TargetEntity: "training/" + trialID,
		Metadata:     meta,
		At:           time.Now().UTC(),
		IP:           clientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
	})
}
