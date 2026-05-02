package app

import (
	"context"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/budget"
)

// budgetReaderAdapter wraps admin.UserBudgetsRepository to satisfy
// budget.BudgetReader. The translation:
//
//   - admin.UserBudget carries the wire shape (with isDefault +
//     audit metadata).
//   - budget.Cap carries only the fields the enforcer cares about
//     (daily/monthly + isDefault).
//
// Lives in app/ so admin/ doesn't need to know about budget/ —
// same one-way-dep pattern as detection_run_adapter.
type budgetReaderAdapter struct {
	repo *admin.UserBudgetsMongoRepository
}

func newBudgetReaderAdapter(repo *admin.UserBudgetsMongoRepository) *budgetReaderAdapter {
	return &budgetReaderAdapter{repo: repo}
}

func (a *budgetReaderAdapter) BudgetForUser(ctx context.Context, userID string) (budget.Cap, error) {
	if a == nil || a.repo == nil {
		return budget.Cap{}, nil
	}
	row, isDefault, err := a.repo.GetForUser(ctx, userID)
	if err != nil {
		return budget.Cap{}, err
	}
	return budget.Cap{
		UserID:     row.UserID,
		DailyUSD:   row.DailyUSD,
		MonthlyUSD: row.MonthlyUSD,
		IsDefault:  isDefault,
	}, nil
}

// outfitBudgetEnforcerAdapter wraps a budget.Enforcer to satisfy
// outfit.BudgetEnforcer. The shapes diverge because outfit/ doesn't
// import budget/ (one-way dep): the enforcer's Reason struct is
// flattened into a map[string]any at this boundary so the outfit
// handler can render it without unwrapping a concrete type.
type outfitBudgetEnforcerAdapter struct {
	e *budget.Enforcer
}

func newOutfitBudgetEnforcerAdapter(e *budget.Enforcer) *outfitBudgetEnforcerAdapter {
	return &outfitBudgetEnforcerAdapter{e: e}
}

func (a *outfitBudgetEnforcerAdapter) Check(ctx context.Context, userID string, estimatedUSD float64) (bool, map[string]any, error) {
	if a == nil || a.e == nil {
		return true, nil, nil
	}
	decision, reason, err := a.e.Check(ctx, userID, estimatedUSD)
	if err != nil {
		return true, nil, err
	}
	allow := decision == budget.Allow
	if allow {
		return true, nil, nil
	}
	out := map[string]any{
		"code":          reason.Code,
		"message":       reason.Message,
		"dailyCapUSD":   reason.DailyCapUSD,
		"todaySpendUSD": reason.TodaySpendUSD,
		"estimatedUSD":  reason.EstimatedUSD,
	}
	if reason.SuspendedUntil != nil {
		out["suspendedUntil"] = reason.SuspendedUntil.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return false, out, nil
}
