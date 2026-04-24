package admin

import "testing"

func TestHashPassword_RoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		password string
	}{
		{"simple ASCII", "hunter2hunter2"},
		{"unicode", "пaрoль-тест-123"},
		{"very long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash, err := HashPassword(c.password)
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if err := VerifyPassword(hash, c.password); err != nil {
				t.Fatalf("verify correct password failed: %v", err)
			}
			if err := VerifyPassword(hash, c.password+"x"); err == nil {
				t.Fatalf("verify accepted wrong password")
			}
		})
	}
}

func TestHashPassword_DifferentEachCall(t *testing.T) {
	// Fresh salt per call → different hash for the same password.
	h1, _ := HashPassword("hunter2hunter2")
	h2, _ := HashPassword("hunter2hunter2")
	if h1 == h2 {
		t.Fatalf("expected salts to differ; got identical hashes")
	}
}

func TestHashPassword_RejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatalf("expected error on empty password")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	cases := []string{
		"",
		"not-a-phc-string",
		"$bcrypt$v=19$m=65536,t=3,p=4$xxx$yyy", // wrong algorithm
		"$argon2id$broken",
		"$argon2id$v=19$m=65536,t=3,p=4$!!!$!!!", // bad base64
	}
	for _, h := range cases {
		if err := VerifyPassword(h, "anything"); err == nil {
			t.Errorf("expected error for malformed hash %q, got nil", h)
		}
	}
}

func TestVerifyPassword_VersionMismatch(t *testing.T) {
	// Valid PHC structure but wrong argon2 version — must refuse.
	h := "$argon2id$v=18$m=65536,t=3,p=4$YWFhYWFhYWFhYWFhYWFhYQ$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := VerifyPassword(h, "x"); err == nil {
		t.Fatalf("expected version-mismatch error")
	}
}
