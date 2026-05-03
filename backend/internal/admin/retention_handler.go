package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"mootd/backend/internal/shared/response"
)

// GetCohortRetention handles GET /admin/v1/retention.
//
// Query params:
//   - cohortUnit: "day" or "week" (required)
//   - n: integer 1..30, default 7 (number of retention columns)
//
// Live aggregation against the events collection. Read-only.
//
// 30s timeout — the worst case is ~30 cohorts × N events per
// cohort, but it's all hot-path index reads on (createdAt) and
// (userId). On a healthy events collection this returns in
// well under a second.
func (h *Handler) GetCohortRetention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.retentionRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "retention not wired"})
		return
	}

	unit := r.URL.Query().Get("cohortUnit")
	if unit == "" {
		unit = "day"
	}
	if unit != "day" && unit != "week" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "cohortUnit must be 'day' or 'week'"})
		return
	}
	n := 7
	if raw := r.URL.Query().Get("n"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 30 {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "n must be an integer in [1, 30]"})
			return
		}
		n = v
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := h.retentionRepo.Compute(ctx, unit, n)
	if err != nil {
		h.logger.Printf("admin /retention compute: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if out.Cohorts == nil {
		out.Cohorts = []CohortRow{}
	}
	response.WriteJSON(w, http.StatusOK, out)
}
