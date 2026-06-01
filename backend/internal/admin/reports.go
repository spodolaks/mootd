package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/smtp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ────────────────────────────────────────────────────────────────────
// Weekly cost report (P4-04 / mootd-admin#32).
//
// Two surfaces, one shape:
//
//   GET  /admin/v1/reports/weekly         → render JSON (admin UI preview)
//   POST /admin/v1/reports/weekly/send    → render + email via SMTP
//
// The email body is the same data, formatted as text/plain.
// HTML email is a follow-up — text renders cleanly in every client
// and avoids the rendering-engine quirks that make HTML email a
// time sink.
//
// Cron: a goroutine at boot computes "next Monday 08:00 UTC" and
// sleeps until then. On wake it fires Send and reschedules. SMTP
// failures are logged + retried on the next cycle (no in-band
// retry — a flaky email server is better dealt with by the next
// cycle than by tight retries that flood the inbox).
//
// Scope split:
//
//   - Incidents detection is shallow today: budget-cap breaches
//     visible from the user_budgets cap vs. weekly spend. Cache-
//     rate drops + prompt-version correlations need the per-day
//     trend logic added in mootd-admin#31 — folded in here as
//     well.
//   - Recommendations are template-driven (a few hard-coded
//     "if total spend WoW > +30% then suggest X" rules). LLM-
//     generated narrative is a tempting follow-up but adds an
//     LLM dep to a feature whose value is precisely "see costs
//     in plain text."
// ────────────────────────────────────────────────────────────────────

// WeeklyReport mirrors the wire shape.
type WeeklyReport struct {
	WeekLabel        string                `json:"weekLabel"`
	WeekStart        time.Time             `json:"weekStart"`
	WeekEnd          time.Time             `json:"weekEnd"`
	TotalCostUSD     float64               `json:"totalCostUsd"`
	PriorWeekCostUSD float64               `json:"priorWeekCostUsd,omitempty"`
	DAU              int                   `json:"dau"`
	CostPerDAUUSD    float64               `json:"costPerDauUsd"`
	TopUsers         []WeeklyReportUserRow `json:"topUsers"`
	ByModel          []WeeklyReportFacet   `json:"byModel"`
	ByFeature        []WeeklyReportFacet   `json:"byFeature"`
	Incidents        []string              `json:"incidents,omitempty"`
	Recommendations  []string              `json:"recommendations,omitempty"`
}

// WeeklyReportUserRow is one row of the top-N table.
type WeeklyReportUserRow struct {
	UserID    string  `json:"userId"`
	UserEmail string  `json:"userEmail,omitempty"`
	CostUSD   float64 `json:"costUsd"`
	CallCount int64   `json:"callCount"`
}

// WeeklyReportFacet is one row of a "by X" breakdown.
type WeeklyReportFacet struct {
	Label     string  `json:"label"`
	CostUSD   float64 `json:"costUsd"`
	CallCount int64   `json:"callCount"`
	Share     float64 `json:"share,omitempty"` // 0-1 fraction of total
}

// ReportsRepository builds the weekly report. Implementations
// run aggregations against llm_calls and friends. The interface
// is small so tests can substitute a fake without spinning up
// Mongo.
type ReportsRepository interface {
	Build(ctx context.Context, weekStart, weekEnd time.Time) (*WeeklyReport, error)
}

// ReportsMongoRepository is the production implementation.
type ReportsMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewReportsMongoRepository constructs the repo. No indexes
// required: the report relies on the existing (createdAt) +
// (userId, createdAt) indexes that mootd-admin#34 ensures on
// llm_calls.
func NewReportsMongoRepository(client *mongo.Client, dbName string) *ReportsMongoRepository {
	return &ReportsMongoRepository{client: client, dbName: dbName}
}

func (r *ReportsMongoRepository) llmCallsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("llm_calls")
}

func (r *ReportsMongoRepository) usersCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("users")
}

// Build runs the aggregations for `weekStart` (inclusive) to
// `weekEnd` (exclusive). All errors are wrapped with the stage
// name so log-trolling identifies which pipeline failed.
func (r *ReportsMongoRepository) Build(ctx context.Context, weekStart, weekEnd time.Time) (*WeeklyReport, error) {
	report := &WeeklyReport{
		WeekStart: weekStart,
		WeekEnd:   weekEnd,
		WeekLabel: isoWeekLabel(weekStart),
	}

	// 1. Total cost + DAU.
	totals, err := r.totalsFor(ctx, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("totals: %w", err)
	}
	report.TotalCostUSD = totals.cost
	report.DAU = totals.dau
	if totals.dau > 0 {
		report.CostPerDAUUSD = totals.cost / float64(totals.dau)
	}

	// 2. Prior week for WoW delta.
	prior, _ := r.totalsFor(ctx, weekStart.Add(-7*24*time.Hour), weekStart)
	if prior != nil {
		report.PriorWeekCostUSD = prior.cost
	}

	// 3. Top 10 users by cost.
	topUsers, err := r.topUsersFor(ctx, weekStart, weekEnd, 10)
	if err != nil {
		return nil, fmt.Errorf("topUsers: %w", err)
	}
	report.TopUsers = topUsers

	// 4. By-model breakdown.
	byModel, err := r.facetFor(ctx, weekStart, weekEnd, "$model")
	if err != nil {
		return nil, fmt.Errorf("byModel: %w", err)
	}
	for i := range byModel {
		if report.TotalCostUSD > 0 {
			byModel[i].Share = byModel[i].CostUSD / report.TotalCostUSD
		}
	}
	report.ByModel = byModel

	// 5. By-feature breakdown.
	byFeature, err := r.facetFor(ctx, weekStart, weekEnd, "$feature")
	if err != nil {
		return nil, fmt.Errorf("byFeature: %w", err)
	}
	for i := range byFeature {
		if report.TotalCostUSD > 0 {
			byFeature[i].Share = byFeature[i].CostUSD / report.TotalCostUSD
		}
	}
	report.ByFeature = byFeature

	// 6. Heuristic incidents + recommendations.
	report.Incidents = detectIncidents(report)
	report.Recommendations = buildRecommendations(report)

	return report, nil
}

type weeklyTotals struct {
	cost float64
	dau  int
}

func (r *ReportsMongoRepository) totalsFor(ctx context.Context, start, end time.Time) (*weeklyTotals, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": start, "$lt": end},
			"status":    "success",
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"cost":  bson.M{"$sum": "$costUsd"},
			"users": bson.M{"$addToSet": "$userId"},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		Cost  float64  `bson:"cost"`
		Users []string `bson:"users"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &weeklyTotals{}, nil
	}
	return &weeklyTotals{cost: rows[0].Cost, dau: len(rows[0].Users)}, nil
}

func (r *ReportsMongoRepository) topUsersFor(ctx context.Context, start, end time.Time, n int) ([]WeeklyReportUserRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": start, "$lt": end},
			"status":    "success",
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$userId",
			"cost":  bson.M{"$sum": "$costUsd"},
			"calls": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "cost", Value: -1}}}},
		{{Key: "$limit", Value: int64(n)}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		ID    string  `bson:"_id"`
		Cost  float64 `bson:"cost"`
		Calls int64   `bson:"calls"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []WeeklyReportUserRow{}, nil
	}

	// Best-effort email join. If users.email lookups error, we
	// surface IDs only — better than failing the whole report.
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	emails := r.emailsForIDs(ctx, ids)

	out := make([]WeeklyReportUserRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, WeeklyReportUserRow{
			UserID:    row.ID,
			UserEmail: emails[row.ID],
			CostUSD:   row.Cost,
			CallCount: row.Calls,
		})
	}
	return out, nil
}

func (r *ReportsMongoRepository) emailsForIDs(ctx context.Context, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	cur, err := r.usersCol().Find(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
	)
	if err != nil {
		return out
	}
	defer cur.Close(ctx)
	var rows []struct {
		ID    string `bson:"_id"`
		Email string `bson:"email"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return out
	}
	for _, row := range rows {
		out[row.ID] = row.Email
	}
	return out
}

func (r *ReportsMongoRepository) facetFor(ctx context.Context, start, end time.Time, field string) ([]WeeklyReportFacet, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": start, "$lt": end},
			"status":    "success",
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   field,
			"cost":  bson.M{"$sum": "$costUsd"},
			"calls": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "cost", Value: -1}}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		Label string  `bson:"_id"`
		Cost  float64 `bson:"cost"`
		Calls int64   `bson:"calls"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]WeeklyReportFacet, 0, len(rows))
	for _, row := range rows {
		out = append(out, WeeklyReportFacet{
			Label:     row.Label,
			CostUSD:   row.Cost,
			CallCount: row.Calls,
		})
	}
	return out, nil
}

// ────────────────────────────────────────────────────────────────────
// Heuristic incidents + recommendations.
// ────────────────────────────────────────────────────────────────────

func detectIncidents(r *WeeklyReport) []string {
	var out []string
	// Total spend WoW spike.
	if r.PriorWeekCostUSD > 0 {
		delta := r.TotalCostUSD - r.PriorWeekCostUSD
		ratio := delta / r.PriorWeekCostUSD
		if ratio >= 0.30 {
			out = append(out, fmt.Sprintf("Total spend up %.0f%% WoW ($%.2f → $%.2f)",
				ratio*100, r.PriorWeekCostUSD, r.TotalCostUSD))
		}
		if ratio <= -0.30 {
			out = append(out, fmt.Sprintf("Total spend down %.0f%% WoW ($%.2f → $%.2f)",
				ratio*100, r.PriorWeekCostUSD, r.TotalCostUSD))
		}
	}
	// Top user concentration.
	if len(r.TopUsers) > 0 && r.TotalCostUSD > 0 {
		share := r.TopUsers[0].CostUSD / r.TotalCostUSD
		if share >= 0.40 {
			out = append(out, fmt.Sprintf(
				"Single user accounts for %.0f%% of weekly spend (%s, $%.2f)",
				share*100, r.TopUsers[0].UserID, r.TopUsers[0].CostUSD))
		}
	}
	// One model dominating.
	if len(r.ByModel) > 0 && r.ByModel[0].Share >= 0.95 {
		out = append(out, fmt.Sprintf(
			"%.0f%% of cost on a single model (%s) — consider provider diversity",
			r.ByModel[0].Share*100, r.ByModel[0].Label))
	}
	return out
}

func buildRecommendations(r *WeeklyReport) []string {
	var out []string
	if r.TotalCostUSD == 0 {
		out = append(out, "No LLM activity this week — verify telemetry isn't broken before reading the rest.")
		return out
	}
	if r.PriorWeekCostUSD > 0 && r.TotalCostUSD > r.PriorWeekCostUSD*1.30 {
		out = append(out, "Investigate the WoW cost spike: drill into the top 3 users + check the byFeature breakdown for outliers.")
	}
	if len(r.TopUsers) > 0 && r.TopUsers[0].CostUSD > 5.00 {
		out = append(out, fmt.Sprintf("Review %s's flow — they're trending hot at $%.2f/wk; confirm intentional usage.",
			r.TopUsers[0].UserID, r.TopUsers[0].CostUSD))
	}
	// Cost-per-DAU floor signals price-sensitivity wins from caching.
	if r.CostPerDAUUSD > 0.50 {
		out = append(out, fmt.Sprintf("Cost-per-DAU is $%.2f — check Anthropic prompt-cache hit rate on /overview; budget caps + cache misses both compound here.",
			r.CostPerDAUUSD))
	}
	return out
}

// ────────────────────────────────────────────────────────────────────
// SMTP sender.
// ────────────────────────────────────────────────────────────────────

// SMTPConfig captures everything net/smtp needs. Loaded from env
// at boot. When SMTPHost is empty the sender returns
// ErrSMTPNotConfigured and the caller logs the rendered email
// instead of sending.
type SMTPConfig struct {
	Host       string // e.g. "smtp.gmail.com"
	Port       string // e.g. "587"
	Username   string
	Password   string
	FromAddr   string // header from
	ToAddr     string // recipient (founder)
	ServerName string // for TLS — defaults to Host
}

// ErrSMTPNotConfigured is returned by SendWeeklyReport when no
// SMTP host is configured.
var ErrSMTPNotConfigured = errors.New("admin: SMTP not configured")

// SendWeeklyReport renders + sends the report via plain-auth
// SMTP over STARTTLS. Returns nil on success, ErrSMTPNotConfigured
// when the host isn't set, or a wrapped error on transport
// failure.
func SendWeeklyReport(cfg SMTPConfig, r *WeeklyReport) error {
	if cfg.Host == "" {
		return ErrSMTPNotConfigured
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.ServerName == "" {
		cfg.ServerName = cfg.Host
	}
	body := RenderWeeklyReportText(r)
	subject := fmt.Sprintf("[mootd] Weekly cost report — %s", r.WeekLabel)

	msg := buildEmailMessage(cfg.FromAddr, cfg.ToAddr, subject, body)

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	addr := cfg.Host + ":" + cfg.Port
	return smtp.SendMail(addr, auth, cfg.FromAddr, []string{cfg.ToAddr}, msg)
}

// buildEmailMessage assembles RFC-5322-ish headers + body. We
// keep it deliberately minimal — no Content-Type beyond text/plain
// because rich rendering is a separate concern.
func buildEmailMessage(from, to, subject, body string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.Bytes()
}

// RenderWeeklyReportText renders the report as plain-text suitable
// for email. Exported so tests + the admin "preview" endpoint can
// share the format.
func RenderWeeklyReportText(r *WeeklyReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mootd weekly cost report — %s\n", r.WeekLabel)
	fmt.Fprintf(&b, "%s through %s (UTC)\n",
		r.WeekStart.Format("Mon Jan 2"), r.WeekEnd.Add(-time.Second).Format("Mon Jan 2"))
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	// Summary.
	fmt.Fprintf(&b, "Total spend          $%.2f\n", r.TotalCostUSD)
	if r.PriorWeekCostUSD > 0 {
		delta := r.TotalCostUSD - r.PriorWeekCostUSD
		ratio := delta / r.PriorWeekCostUSD * 100
		fmt.Fprintf(&b, "Prior week spend     $%.2f  (%+.0f%% WoW)\n", r.PriorWeekCostUSD, ratio)
	}
	fmt.Fprintf(&b, "DAU                  %d\n", r.DAU)
	fmt.Fprintf(&b, "Cost per DAU         $%.2f\n", r.CostPerDAUUSD)
	b.WriteString("\n")

	// Top users.
	b.WriteString("Top users by spend\n")
	b.WriteString(strings.Repeat("─", 50) + "\n")
	if len(r.TopUsers) == 0 {
		b.WriteString("(no activity this week)\n")
	}
	for i, u := range r.TopUsers {
		ident := u.UserEmail
		if ident == "" {
			ident = u.UserID
		}
		fmt.Fprintf(&b, "%2d. %-30s  $%6.2f  (%d calls)\n", i+1, truncateForReport(ident, 30), u.CostUSD, u.CallCount)
	}
	b.WriteString("\n")

	// By model.
	b.WriteString("By model\n")
	b.WriteString(strings.Repeat("─", 50) + "\n")
	for _, f := range r.ByModel {
		fmt.Fprintf(&b, "  %-30s  $%6.2f  (%5.1f%%, %d calls)\n",
			truncateForReport(f.Label, 30), f.CostUSD, f.Share*100, f.CallCount)
	}
	b.WriteString("\n")

	// By feature.
	b.WriteString("By feature\n")
	b.WriteString(strings.Repeat("─", 50) + "\n")
	for _, f := range r.ByFeature {
		fmt.Fprintf(&b, "  %-30s  $%6.2f  (%5.1f%%, %d calls)\n",
			truncateForReport(f.Label, 30), f.CostUSD, f.Share*100, f.CallCount)
	}
	b.WriteString("\n")

	// Incidents.
	if len(r.Incidents) > 0 {
		b.WriteString("Incidents\n")
		b.WriteString(strings.Repeat("─", 50) + "\n")
		for _, inc := range r.Incidents {
			fmt.Fprintf(&b, "  • %s\n", inc)
		}
		b.WriteString("\n")
	}

	// Recommendations.
	if len(r.Recommendations) > 0 {
		b.WriteString("Recommendations\n")
		b.WriteString(strings.Repeat("─", 50) + "\n")
		for _, rec := range r.Recommendations {
			fmt.Fprintf(&b, "  • %s\n", rec)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func truncateForReport(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ────────────────────────────────────────────────────────────────────
// Time helpers.
// ────────────────────────────────────────────────────────────────────

// LastCompletedISOWeek returns Monday-Monday UTC bounds for the
// most recently completed week relative to `now`. Used as the
// default for the GET endpoint when no `week=` is supplied.
func LastCompletedISOWeek(now time.Time) (start, end time.Time) {
	now = now.UTC()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday is "7" so Monday is "1"
	}
	// Monday of the current week.
	thisMon := time.Date(now.Year(), now.Month(), now.Day()-(weekday-1), 0, 0, 0, 0, time.UTC)
	end = thisMon // exclusive
	start = thisMon.Add(-7 * 24 * time.Hour)
	return start, end
}

// ParseISOWeekLabel turns "2026-W18" into the Monday-start UTC
// time. Returns an error if malformed. ISO weeks have weird edge
// cases (year 0001-W01 etc.); we use Go's time package's
// ISOWeek logic to stay consistent.
func ParseISOWeekLabel(label string) (time.Time, error) {
	parts := strings.Split(label, "-W")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid week label %q (expected YYYY-Www)", label)
	}
	var year, week int
	if _, err := fmt.Sscanf(parts[0], "%d", &year); err != nil {
		return time.Time{}, fmt.Errorf("invalid year in %q: %w", label, err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &week); err != nil {
		return time.Time{}, fmt.Errorf("invalid week in %q: %w", label, err)
	}
	if week < 1 || week > 53 {
		return time.Time{}, fmt.Errorf("week %d out of range (1-53)", week)
	}
	// Find a date guaranteed to fall in this ISO week, then walk
	// back to the Monday. ISO week 1 contains January 4th — same
	// algorithm Go's standard examples use.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	_, isoWeek := jan4.ISOWeek()
	mondayOfWeek1 := jan4.AddDate(0, 0, -(int(jan4.Weekday())+6)%7)
	monday := mondayOfWeek1.AddDate(0, 0, (week-isoWeek)*7)
	return monday, nil
}

func isoWeekLabel(monday time.Time) string {
	year, week := monday.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// ────────────────────────────────────────────────────────────────────
// Cron scheduler.
// ────────────────────────────────────────────────────────────────────

// StartWeeklyReportCron spawns a goroutine that fires Send every
// Monday at 08:00 UTC. Caller passes a build+send closure so the
// goroutine doesn't need access to handler internals.
//
// On Send error, log + continue to the next cycle. There's no
// retry on the same week — flaky email is better dealt with by
// the admin manually triggering POST /reports/weekly/send than
// by tight retry loops that would flood inboxes.
//
// Returns immediately; the caller's lifecycle owns the goroutine
// (it stops when ctx is cancelled).
func StartWeeklyReportCron(
	ctx context.Context,
	logger interface{ Printf(string, ...any) },
	now func() time.Time,
	send func(context.Context) error,
) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	go func() {
		for {
			next := nextMonday0800UTC(now())
			delay := next.Sub(now())
			logger.Printf("reports: weekly cron next fire at %s (in %s)", next.Format(time.RFC3339), delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if err := send(ctx); err != nil {
				logger.Printf("reports: weekly send failed: %v", err)
			} else {
				logger.Printf("reports: weekly report sent")
			}
		}
	}()
}

// nextMonday0800UTC returns the next Monday-08:00-UTC strictly
// after `now`. Exposed for testing.
func nextMonday0800UTC(now time.Time) time.Time {
	now = now.UTC()
	target := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	// Days until Monday: 1 - weekday (mod 7). Then if the result
	// is 0 (we're already Monday) and we're past 08:00, add 7.
	delta := (8 - weekday) % 7 // 8 because Monday is 1 in Go's weekday
	target = target.AddDate(0, 0, delta)
	if !target.After(now) {
		target = target.AddDate(0, 0, 7)
	}
	return target
}

// SortFacetsByCostDesc is a small utility tests reach for.
func SortFacetsByCostDesc(rows []WeeklyReportFacet) {
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CostUSD > rows[j].CostUSD
	})
}
