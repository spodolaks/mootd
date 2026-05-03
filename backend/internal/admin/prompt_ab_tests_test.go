package admin

import "testing"

func TestUserBucketPct_DeterministicAcrossCalls(t *testing.T) {
	a := UserBucketPct("user-123", "outfit_system_base")
	b := UserBucketPct("user-123", "outfit_system_base")
	if a != b {
		t.Errorf("non-deterministic: %d vs %d", a, b)
	}
}

func TestUserBucketPct_DifferentTemplatesIndependent(t *testing.T) {
	// Same user, different templates → buckets shouldn't be
	// strictly correlated. Sample a handful of users + check
	// at least some land in different deciles for the two
	// templates.
	differs := 0
	for i := 0; i < 100; i++ {
		u := "user-" + string(rune(i%26+'a')) + string(rune(i/26+'a'))
		ba := UserBucketPct(u, "outfit_system_base")
		bb := UserBucketPct(u, "outfit_safety")
		if ba/10 != bb/10 {
			differs++
		}
	}
	if differs < 50 {
		t.Errorf("expected substantial independence, got %d/100 different deciles", differs)
	}
}

func TestUserBucketPct_RangeAllValid(t *testing.T) {
	for i := 0; i < 1000; i++ {
		u := "u" + string(rune(i%26+'a'))
		got := UserBucketPct(u, "x")
		if got < 0 || got >= 100 {
			t.Errorf("out of range: %d", got)
		}
	}
}

func TestUserBucketPct_EmptyUserAlwaysProduction(t *testing.T) {
	// Anonymous calls bucket 0 → IsCandidateUser returns
	// false for any non-zero trafficPct.
	if UserBucketPct("", "x") != 0 {
		t.Error("empty user should bucket 0")
	}
}

func TestIsCandidateUser_NoTest_AlwaysFalse(t *testing.T) {
	if IsCandidateUser("u", nil) {
		t.Error("nil test → never candidate")
	}
}

func TestIsCandidateUser_RoughDistribution(t *testing.T) {
	// 50/50 split should produce something close to 50% over a
	// reasonable sample. Loose bound — flake-avoidant.
	test := &ABTest{TemplateName: "outfit_system_base", TrafficPct: 50, Status: ABTestActive}
	candidate := 0
	for i := 0; i < 1000; i++ {
		// Use a varied user pool.
		u := "user-" + string(rune(i)) + string(rune(i*3))
		if IsCandidateUser(u, test) {
			candidate++
		}
	}
	// Expect ~500; allow ±100 either way (10pp drift on 1000
	// samples is well within hash-distribution noise).
	if candidate < 400 || candidate > 600 {
		t.Errorf("expected ~50%% candidate, got %d/1000", candidate)
	}
}

func TestIsCandidateUser_EndedTestIgnored(t *testing.T) {
	test := &ABTest{TemplateName: "x", TrafficPct: 100, Status: ABTestEnded}
	if IsCandidateUser("u", test) {
		t.Error("ended test should not route any user to candidate")
	}
}
