package admin

import "testing"

func TestParsePromptVersionRef(t *testing.T) {
	cases := []struct {
		ref     string
		name    string
		version int
		wantErr bool
	}{
		{"outfit_system_base@v4", "outfit_system_base", 4, false},
		{"outfit_system_base.creator@v12", "outfit_system_base.creator", 12, false},
		{"outfit_system_base", "", 0, true}, // no @v
		{"@v4", "", 0, true},                // empty name
		{"outfit_system_base@", "", 0, true},
		{"outfit_system_base@4", "", 0, true},  // missing v
		{"outfit_system_base@v0", "", 0, true}, // versions start at 1
		{"outfit_system_base@vx", "", 0, true},
	}
	for _, c := range cases {
		name, version, err := parsePromptVersionRef(c.ref)
		if c.wantErr != (err != nil) {
			t.Errorf("%q: err=%v, wantErr=%v", c.ref, err, c.wantErr)
			continue
		}
		if err == nil && (name != c.name || version != c.version) {
			t.Errorf("%q: got (%q, %d), want (%q, %d)", c.ref, name, version, c.name, c.version)
		}
	}
}
