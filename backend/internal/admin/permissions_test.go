package admin

import "testing"

func TestHasPermission_Admin(t *testing.T) {
	cases := []Permission{
		PermUsersPII, PermBudgetsWrite, PermDetectionsRerun, PermSessionsView,
	}
	for _, p := range cases {
		if !HasPermission([]string{string(RoleAdmin)}, p) {
			t.Errorf("admin should have %q", p)
		}
	}
}

func TestHasPermission_Engineer(t *testing.T) {
	roles := []string{string(RoleEngineer)}
	if !HasPermission(roles, PermTracesRerun) {
		t.Error("engineer should have traces:rerun")
	}
	if HasPermission(roles, PermUsersPII) {
		t.Error("engineer should NOT have users:pii")
	}
	if HasPermission(roles, PermBudgetsWrite) {
		t.Error("engineer should NOT have budgets:write")
	}
}

func TestHasPermission_Support(t *testing.T) {
	roles := []string{string(RoleSupport)}
	if !HasPermission(roles, PermUsersPII) {
		t.Error("support should have users:pii (with audit)")
	}
	if HasPermission(roles, PermDetectionsRerun) {
		t.Error("support should NOT have detections:rerun")
	}
}

func TestHasPermission_Readonly(t *testing.T) {
	roles := []string{string(RoleReadonly)}
	if !HasPermission(roles, PermUsersRead) {
		t.Error("readonly should have users:read")
	}
	if HasPermission(roles, PermUsersPII) {
		t.Error("readonly should NOT have users:pii")
	}
	if HasPermission(roles, PermBudgetsWrite) {
		t.Error("readonly should NOT have budgets:write")
	}
}

func TestHasPermission_Curator(t *testing.T) {
	roles := []string{string(RoleCurator)}
	if !HasPermission(roles, PermPromptsRead) {
		t.Error("curator should have prompts:read (archetype-defaults curation)")
	}
	for _, p := range []Permission{
		PermUsersRead, PermUsersPII, PermTracesRead, PermTracesRerun,
		PermPromptsWrite, PermDetectionsRerun, PermSpendRead,
		PermBudgetsWrite, PermRoutingWrite, PermSessionsView,
		PermAdminsManage,
	} {
		if HasPermission(roles, p) {
			t.Errorf("curator should NOT have %q", p)
		}
	}
}

func TestHasPermission_UnknownRole_NoPermissions(t *testing.T) {
	if HasPermission([]string{"superuser"}, PermUsersRead) {
		t.Error("unknown role should grant nothing")
	}
}

func TestHasPermission_MultipleRoles_Union(t *testing.T) {
	// Engineer + Support together should give the union.
	roles := []string{string(RoleEngineer), string(RoleSupport)}
	if !HasPermission(roles, PermTracesRerun) {
		t.Error("engineer ∪ support should have traces:rerun (from engineer)")
	}
	if !HasPermission(roles, PermUsersPII) {
		t.Error("engineer ∪ support should have users:pii (from support)")
	}
}

func TestPermissionsFor_Stable(t *testing.T) {
	// Same input → same output, including order.
	a := PermissionsFor([]string{string(RoleEngineer)})
	b := PermissionsFor([]string{string(RoleEngineer)})
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("order mismatch at %d: %q vs %q", i, a[i], b[i])
		}
	}
}
