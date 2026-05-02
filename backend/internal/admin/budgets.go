package admin

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Budget defaults applied when a user has no override row in
// `user_budgets`. Captured here (not in env) so they're discoverable
// from the admin API + obvious in code review. Adjusting these
// numbers is a code change, not a config change — operators should
// reach for per-user overrides instead.
//
// Rationale on the values:
//   - Daily $2.00: typical Mootd outfit-generation runs $0.05–$0.20
//     per call. $2/day is comfortably above a heavy-user day (10
//     generations) but well under abuse territory (a runaway loop
//     that's hammering the LLM).
//   - Monthly $30.00: 15× the daily cap, leaving headroom for users
//     who batch their usage on a few intense weekends rather than
//     spreading it evenly. P4-02 enforcement (mootd-admin#30) will
//     stop generation at the cap, not throttle gradually.
const (
	DefaultDailyBudgetUSD   = 2.00
	DefaultMonthlyBudgetUSD = 30.00
)

// UserBudget is the wire shape for /admin/v1/users/{id}/budget.
// Mirrors the user_budgets collection shape, with `isDefault` set
// when the response carries the system defaults rather than a saved
// override. Enforcement (auto-downgrade / hard-stop) is mootd-admin#30.
type UserBudget struct {
	UserID     string  `json:"userId"        bson:"userId"`
	DailyUSD   float64 `json:"dailyUSD"      bson:"dailyUSD"`
	MonthlyUSD float64 `json:"monthlyUSD"    bson:"monthlyUSD"`
	IsDefault  bool    `json:"isDefault"     bson:"-"`
	SetBy      string  `json:"setBy,omitempty"  bson:"setBy,omitempty"`
	// SetAt is a *time.Time pointer so JSON omitempty actually
	// drops the field when unset. Go's encoding/json doesn't honour
	// omitempty for the zero-value time.Time (it'd render as
	// "0001-01-01T00:00:00Z" — confusing for callers).
	SetAt  *time.Time `json:"setAt,omitempty"  bson:"setAt,omitempty"`
	Reason string     `json:"reason,omitempty" bson:"reason,omitempty"`

	// P4-02 (mootd-admin#30) live state, populated at GET-time
	// from the budget tracker (Redis). Both bson tags are "-"
	// because these aren't persisted on user_budgets — they're
	// computed on demand.
	TodaySpendUSD  float64    `json:"todaySpendUSD" bson:"-"`
	SuspendedUntil *time.Time `json:"suspendedUntil,omitempty" bson:"-"`
}

// UserBudgetsRepository owns persistence for user_budgets.
type UserBudgetsRepository interface {
	// GetForUser returns the user's saved budget, or
	// (defaults, true, nil) when no override exists. The boolean
	// distinguishes "default returned" from "real override returned"
	// without callers having to inspect SetAt.
	GetForUser(ctx context.Context, userID string) (UserBudget, bool, error)

	// Upsert replaces the user's budget. Caller is expected to
	// validate the values before calling and to write an audit
	// entry afterward — this repo keeps a narrow scope.
	Upsert(ctx context.Context, b UserBudget) error
}

// UserBudgetsMongoRepository implements UserBudgetsRepository
// against the shared cluster. user_budgets is keyed by userId; one
// row per user. Indexed on (userId) for the primary lookup.
type UserBudgetsMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewUserBudgetsMongoRepository constructs the repo and ensures the
// (userId) unique index exists.
func NewUserBudgetsMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*UserBudgetsMongoRepository, error) {
	r := &UserBudgetsMongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("user_budgets_user_unique"),
	}); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *UserBudgetsMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("user_budgets")
}

// GetForUser returns the user's override, or the system defaults
// when no row exists. Returns (UserBudget, isDefault, error).
func (r *UserBudgetsMongoRepository) GetForUser(ctx context.Context, userID string) (UserBudget, bool, error) {
	if userID == "" {
		return UserBudget{}, false, errors.New("admin: userID required")
	}
	var doc UserBudget
	err := r.col().FindOne(ctx, bson.M{"userId": userID}).Decode(&doc)
	if err == nil {
		doc.UserID = userID
		doc.IsDefault = false
		return doc, false, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return UserBudget{
			UserID:     userID,
			DailyUSD:   DefaultDailyBudgetUSD,
			MonthlyUSD: DefaultMonthlyBudgetUSD,
			IsDefault:  true,
		}, true, nil
	}
	return UserBudget{}, false, err
}

// Upsert replaces the user_budgets row for a user. Caller writes
// the audit entry afterward — separating concerns keeps the repo
// boundary narrow.
func (r *UserBudgetsMongoRepository) Upsert(ctx context.Context, b UserBudget) error {
	if b.UserID == "" {
		return errors.New("admin: userID required")
	}
	// Always stamp setAt server-side; trust nothing from the wire.
	now := time.Now().UTC()
	b.SetAt = &now
	b.IsDefault = false
	_, err := r.col().ReplaceOne(ctx,
		bson.M{"userId": b.UserID},
		b,
		options.Replace().SetUpsert(true),
	)
	return err
}
