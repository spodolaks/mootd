package admin

import (
	"context"
	"fmt"
	"net/smtp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ────────────────────────────────────────────────────────────────────
// Daily founder summary email (mootd-admin#99).
//
// Replaces the morning "is the app working" admin-panel ritual.
// Cron fires at a configured UTC hour (default 07:00) and sends
// a plain-text email with overnight numbers to every address in
// FOUNDER_EMAILS.
//
// Independent from the weekly cost report (P4-04) on purpose:
//   - daily granularity, dashboard scope (last 24h)
//   - top-3 users by cost surfaces individual hot spots while
//     they're fresh
//   - shorter format: 6 numbers + 3 line items, fits in a
//     phone-screen preview
//
// Reuses OverviewRepository for spend / DAU and a small inline
// query for top-3 by cost. Best-effort: a Mongo failure on any
// one piece logs + skips that section instead of suppressing
// the whole email.
// ────────────────────────────────────────────────────────────────────

// DailySummary is the rendered shape of one daily report. The
// whole struct is also handy as a JSON preview surface (see
// the optional admin /reports/daily endpoint, follow-up).
type DailySummary struct {
	// Window is the half-open interval the numbers cover. Always
	// 24h ending at the cron's "now" — set by the builder.
	WindowStart time.Time `json:"windowStart"`
	WindowEnd   time.Time `json:"windowEnd"`

	DAU           int64   `json:"dau"`
	Generations   int64   `json:"generations"`
	Errors        int64   `json:"errors"`
	SpendUSD      float64 `json:"spendUsd"`
	PriorSpendUSD float64 `json:"priorSpendUsd"`
	NewSignups    int64   `json:"newSignups"`

	TopUsersByCost []DailyTopUser `json:"topUsersByCost,omitempty"`
}

// DailyTopUser is one entry in the top-3 by cost list.
type DailyTopUser struct {
	UserID  string  `json:"userId"`
	Email   string  `json:"email,omitempty"`
	CostUSD float64 `json:"costUsd"`
	Calls   int64   `json:"calls"`
}

// DailySummaryBuilder packages the dependencies the cron + the
// (eventual) preview endpoint need to assemble a DailySummary.
// Constructed once at boot from the same OverviewRepository
// the dashboard uses.
type DailySummaryBuilder struct {
	overview OverviewRepository
	client   *mongo.Client
	dbName   string
}

// NewDailySummaryBuilder constructs the builder. Holds a Mongo
// client directly (not just the repo) because the top-users
// query is an aggregation that doesn't fit OverviewRepository's
// existing surface and isn't worth promoting to a method until
// a second caller wants it.
func NewDailySummaryBuilder(overview OverviewRepository, client *mongo.Client, dbName string) *DailySummaryBuilder {
	return &DailySummaryBuilder{overview: overview, client: client, dbName: dbName}
}

// Build assembles the report for the trailing 24h relative to
// `now`. Best-effort: a sub-section that fails to load logs
// and is omitted; the email still goes out.
func (b *DailySummaryBuilder) Build(ctx context.Context, now time.Time) (*DailySummary, error) {
	now = now.UTC()
	windowEnd := now
	windowStart := now.Add(-24 * time.Hour)
	priorStart := windowStart.Add(-24 * time.Hour)

	out := &DailySummary{
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}

	// Headline numbers via the existing overview repo.
	if spend, count, err := b.overview.PeriodMetrics(ctx, windowStart, windowEnd); err == nil {
		out.SpendUSD = spend
		out.Generations = count // approximate — counts every llm_call, not just successful gen
	}
	if priorSpend, _, err := b.overview.PeriodMetrics(ctx, priorStart, windowStart); err == nil {
		out.PriorSpendUSD = priorSpend
	}
	if dau, err := b.overview.ApproxDAUBetween(ctx, windowStart, windowEnd); err == nil {
		out.DAU = dau
	}

	// Errors + signups via direct queries (no overview helper
	// exists for these scoped to a window).
	if errCount, signupCount, _, err := b.sinceMetrics(ctx, windowStart, windowEnd); err == nil {
		out.Errors = errCount
		out.NewSignups = signupCount
	}

	// Top-3 users by cost in the window.
	top, err := b.topUsersByCost(ctx, windowStart, windowEnd, 3)
	if err == nil {
		out.TopUsersByCost = top
	}

	return out, nil
}

// sinceMetrics is a local copy of OverviewMongoRepository.SinceMetrics
// for builders that don't necessarily hold the concrete repo. We
// only need errors + signup counts here; spend is covered by
// PeriodMetrics already.
func (b *DailySummaryBuilder) sinceMetrics(ctx context.Context, since, end time.Time) (int64, int64, float64, error) {
	if !since.Before(end) {
		return 0, 0, 0, nil
	}
	llmPipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": since, "$lt": end},
			"status":    "error",
		}}},
		{{Key: "$count", Value: "n"}},
	}
	cur, err := b.client.Database(b.dbName).Collection("llm_calls").Aggregate(ctx, llmPipeline)
	if err != nil {
		return 0, 0, 0, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		N int64 `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return 0, 0, 0, err
	}
	var errCount int64
	if len(rows) > 0 {
		errCount = rows[0].N
	}

	signupCount, err := b.client.Database(b.dbName).Collection("events").CountDocuments(ctx, bson.M{
		"name":      "signed_up",
		"createdAt": bson.M{"$gte": since, "$lt": end},
	})
	if err != nil {
		signupCount = 0
	}
	return errCount, signupCount, 0, nil
}

// topUsersByCost returns the top-N userIds by total spend in
// the window. Email join is best-effort.
func (b *DailySummaryBuilder) topUsersByCost(ctx context.Context, start, end time.Time, n int) ([]DailyTopUser, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": start, "$lt": end},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$userId",
			"spend": bson.M{"$sum": "$costUsd"},
			"calls": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.M{"spend": -1}}},
		{{Key: "$limit", Value: int64(n)}},
	}
	cur, err := b.client.Database(b.dbName).Collection("llm_calls").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	type row struct {
		ID    string  `bson:"_id"`
		Spend float64 `bson:"spend"`
		Calls int64   `bson:"calls"`
	}
	var rows []row
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	out := make([]DailyTopUser, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.ID == "" {
			continue
		}
		ids = append(ids, r.ID)
		out = append(out, DailyTopUser{
			UserID:  r.ID,
			CostUSD: r.Spend,
			Calls:   r.Calls,
		})
	}

	// Email join — best-effort.
	if emails, err := b.overview.EmailsForUserIDs(ctx, ids); err == nil {
		for i := range out {
			if e, ok := emails[out[i].UserID]; ok {
				out[i].Email = e
			}
		}
	}
	return out, nil
}

// RenderDailySummaryText composes the plain-text email body.
// Exported so a future preview endpoint can render the same
// shape on demand without going through SMTP.
func RenderDailySummaryText(s *DailySummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mootd daily summary — %s (UTC)\n", s.WindowEnd.Format("Mon Jan 2"))
	fmt.Fprintf(&b, "Window: %s → %s\n",
		s.WindowStart.Format("15:04"), s.WindowEnd.Format("15:04 Mon Jan 2"))
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	fmt.Fprintf(&b, "DAU                  %d\n", s.DAU)
	fmt.Fprintf(&b, "LLM calls            %d\n", s.Generations)
	fmt.Fprintf(&b, "Errors               %d\n", s.Errors)
	fmt.Fprintf(&b, "New signups          %d\n", s.NewSignups)

	if s.PriorSpendUSD > 0 {
		ratio := (s.SpendUSD/s.PriorSpendUSD - 1) * 100
		fmt.Fprintf(&b, "Spend                $%.2f  (%+.0f%% vs prior 24h)\n", s.SpendUSD, ratio)
	} else {
		fmt.Fprintf(&b, "Spend                $%.2f\n", s.SpendUSD)
	}

	if len(s.TopUsersByCost) > 0 {
		b.WriteString("\nTop users by cost:\n")
		for i, u := range s.TopUsersByCost {
			ident := u.Email
			if ident == "" {
				ident = u.UserID
			}
			fmt.Fprintf(&b, "  %d. %-30s $%.4f  (%d calls)\n", i+1, ident, u.CostUSD, u.Calls)
		}
	}

	b.WriteString("\nFull dashboard: https://mootd.app/admin\n")
	return b.String()
}

// SendDailySummary emails the rendered summary to every address
// in `recipients`. Returns the first SMTP error encountered;
// later recipients still receive their copy because
// smtp.SendMail accepts a multi-recipient slice in one envelope.
//
// We pass the recipient list explicitly (rather than trusting
// cfg.ToAddr like SendWeeklyReport does) because the daily
// summary is intentionally multi-recipient — the founder is
// the primary, but a CTO / on-call engineer might be CCed.
func SendDailySummary(cfg SMTPConfig, summary *DailySummary, recipients []string) error {
	if cfg.Host == "" {
		return ErrSMTPNotConfigured
	}
	if len(recipients) == 0 {
		return ErrSMTPNotConfigured
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.ServerName == "" {
		cfg.ServerName = cfg.Host
	}
	body := RenderDailySummaryText(summary)
	subject := fmt.Sprintf("[mootd] Daily summary — %s", summary.WindowEnd.Format("Mon Jan 2"))

	// Build a single message with all recipients in the To:
	// header so each one sees the same envelope (no per-
	// recipient personalisation today).
	toHeader := strings.Join(recipients, ", ")
	msg := buildEmailMessage(cfg.FromAddr, toHeader, subject, body)

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	addr := cfg.Host + ":" + cfg.Port
	return smtp.SendMail(addr, auth, cfg.FromAddr, recipients, msg)
}

// ────────────────────────────────────────────────────────────────────
// Cron scheduler.
// ────────────────────────────────────────────────────────────────────

// StartDailySummaryCron spawns a goroutine that fires `send`
// every day at hourUTC. Same pattern as the weekly cron — log
// + continue on send failure, no in-band retry. Caller's ctx
// owns the goroutine lifecycle.
//
// Returns immediately. `now` is overridable for tests.
func StartDailySummaryCron(
	ctx context.Context,
	logger interface{ Printf(string, ...any) },
	now func() time.Time,
	send func(context.Context) error,
	hourUTC int,
) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if hourUTC < 0 || hourUTC > 23 {
		hourUTC = 7 // default to 07:00 UTC
	}
	go func() {
		for {
			next := nextDailyAtUTC(now(), hourUTC)
			delay := next.Sub(now())
			logger.Printf("daily-summary cron next fire at %s (in %s)", next.Format(time.RFC3339), delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if err := send(ctx); err != nil {
				logger.Printf("daily-summary send failed: %v", err)
			} else {
				logger.Printf("daily-summary sent")
			}
		}
	}()
}

// nextDailyAtUTC returns the next instant on or strictly after
// `now` whose hour-of-day is hourUTC and minute is 0. Exposed
// for testing.
func nextDailyAtUTC(now time.Time, hourUTC int) time.Time {
	now = now.UTC()
	target := time.Date(now.Year(), now.Month(), now.Day(), hourUTC, 0, 0, 0, time.UTC)
	if !target.After(now) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

// ParseFounderEmails splits a comma-separated env var into a
// trimmed, deduped list. Empty / whitespace-only entries dropped.
func ParseFounderEmails(s string) []string {
	if s == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range strings.Split(s, ",") {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		key := strings.ToLower(e)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}
