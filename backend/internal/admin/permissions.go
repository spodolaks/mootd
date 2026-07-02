package admin

// Permission is a fine-grained capability check (P5-01 /
// mootd-admin#34).
//
// We deliberately keep permissions as plain strings rather than
// typed constants so:
//
//   - JWT claims travel as []string without a custom marshaller.
//   - Frontend permission checks are string equality, not enum
//     matching — easier to add a new permission without a
//     coordinated FE/BE deploy.
//
// The trade-off: typos at call sites aren't compile-time errors.
// We mitigate by keeping every permission string declared as a
// const here so call sites can use the const + IDE rename catches
// drift.
type Permission = string

const (
	// User-related.
	PermUsersRead  Permission = "users:read"  // list + detail (no PII reveal)
	PermUsersPII   Permission = "users:pii"   // reveal email + photos + outfit labels
	PermUsersPurge Permission = "users:purge" // GDPR-style account purge

	// Tracing / observability.
	PermTracesRead  Permission = "traces:read"
	PermTracesRerun Permission = "traces:rerun" // run a trace again with a new prompt

	// Training-data export (mootd-admin#125). Scoped separately from
	// traces:rerun (which authorises *running* trials): export ships
	// the accumulated DPO/SFT corpus off the box, so it's a distinct,
	// audited capability — see docs/SECURITY.md export-exfil risk.
	PermTrainingExport Permission = "training:export"

	// Prompts + eval.
	PermPromptsRead  Permission = "prompts:read"
	PermPromptsWrite Permission = "prompts:write"

	// Archetype-default wardrobe items (curator surface). Split
	// off from prompts:write so the curator role can edit defaults
	// without inheriting prompt-template / A-B-test write access.
	PermDefaultsWrite Permission = "defaults:write"

	// Detection.
	PermDetectionsRerun Permission = "detections:rerun"

	// Cost / billing.
	PermSpendRead    Permission = "spend:read"
	PermBudgetsWrite Permission = "budgets:write"

	// Admin self-service / RBAC management.
	PermRoutingWrite Permission = "routing:write"
	PermReportsSend  Permission = "reports:send"
	PermAdminsManage Permission = "admins:manage"

	// Session replay (P5-05 / mootd-admin#38).
	PermSessionsView Permission = "sessions:view"
)

// rolePermissions is the canonical mapping. Edit this table to
// rebalance roles. The map's value is the *complete* set granted
// to a role; we don't union across roles.
//
// Design intent:
//   - admin: gets everything. The unscoped role.
//   - engineer: full traces + prompts + reruns; no PII; no
//     budget edits (cost decisions belong to admin).
//   - support: read users + PII (with audit) + read spend; no
//     trace tooling, no rerun.
//   - readonly: pure read-only, no PII reveal. Useful for
//     external auditors / dashboards.
var rolePermissions = map[Role]map[Permission]bool{
	RoleAdmin: {
		PermUsersRead:       true,
		PermUsersPII:        true,
		PermUsersPurge:      true,
		PermTracesRead:      true,
		PermTracesRerun:     true,
		PermTrainingExport:  true,
		PermPromptsRead:     true,
		PermPromptsWrite:    true,
		PermDefaultsWrite:   true,
		PermDetectionsRerun: true,
		PermSpendRead:       true,
		PermBudgetsWrite:    true,
		PermRoutingWrite:    true,
		PermReportsSend:     true,
		PermAdminsManage:    true,
		PermSessionsView:    true,
	},
	RoleEngineer: {
		PermUsersRead:       true,
		PermTracesRead:      true,
		PermTracesRerun:     true,
		PermTrainingExport:  true,
		PermPromptsRead:     true,
		PermPromptsWrite:    true,
		PermDefaultsWrite:   true,
		PermDetectionsRerun: true,
		PermSpendRead:       true,
	},
	RoleSupport: {
		PermUsersRead: true,
		PermUsersPII:  true,
		PermSpendRead: true,
	},
	RoleReadonly: {
		PermUsersRead:  true,
		PermTracesRead: true,
		PermSpendRead:  true,
		// No PII, no rerun, no writes.
	},
	RoleCurator: {
		// Prompt + archetype-defaults curation. prompts:write lets
		// a curator author/version/promote prompt templates, run
		// A/B tests, and start eval runs (eval-before-promote
		// shares the permission); defaults:write authorises add /
		// edit / delete on archetype defaults. Still no traces,
		// users, spend, or governance surfaces.
		PermPromptsRead:   true,
		PermPromptsWrite:  true,
		PermDefaultsWrite: true,
	},
}

// HasPermission reports whether any of the supplied roles grants
// the given permission. Used by middleware + handlers.
func HasPermission(roles []string, perm Permission) bool {
	for _, r := range roles {
		set, ok := rolePermissions[Role(r)]
		if !ok {
			continue
		}
		if set[perm] {
			return true
		}
	}
	return false
}

// PermissionsFor returns the set of permissions a list of roles
// is granted. The frontend reads this from /admin/v1/me to hide
// nav items + buttons the admin lacks permission for.
func PermissionsFor(roles []string) []Permission {
	got := map[Permission]bool{}
	for _, r := range roles {
		for p := range rolePermissions[Role(r)] {
			got[p] = true
		}
	}
	out := make([]Permission, 0, len(got))
	for p := range got {
		out = append(out, p)
	}
	// Stable order — easier to inspect on the wire and to compare
	// across requests in tests.
	sortStrings(out)
	return out
}

// sortStrings is a tiny helper to avoid importing "sort" just for
// permission sorting. Bubble-sort over <20 elements is fine.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
