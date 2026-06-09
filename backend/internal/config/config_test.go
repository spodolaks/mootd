package config

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateAdminAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		env     string
		wantErr error
	}{
		{"production empty is rejected", "", "production", ErrAdminAllowlistEmptyInProduction},
		{"production whitespace/commas only is rejected", " , ,", "production", ErrAdminAllowlistEmptyInProduction},
		{"production with a CIDR is accepted", "10.0.0.0/8", "production", nil},
		{"production with multiple entries is accepted", "203.0.113.4/32, 10.0.0.0/8", "production", nil},
		{"development empty is allowed", "", "development", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAdminAllowlist(tt.raw, tt.env); !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateAdminAllowlist(%q, %q) = %v, want %v", tt.raw, tt.env, err, tt.wantErr)
			}
		})
	}
}

func TestParseOutfitProviders(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantClean   []string
		wantDropped []string
	}{
		{
			name:      "single provider",
			raw:       "claude",
			wantClean: []string{"claude"},
		},
		{
			// The regression this fix targets: the documented production chain
			// must survive normalisation instead of being coerced to ollama.
			name:      "full cascade chain",
			raw:       "claude,openai,ollama",
			wantClean: []string{"claude", "openai", "ollama"},
		},
		{
			name:      "case-insensitive and trimmed",
			raw:       " Claude , OpenAI ",
			wantClean: []string{"claude", "openai"},
		},
		{
			name:        "unknown entries are dropped and reported",
			raw:         "claude,foo,ollama",
			wantClean:   []string{"claude", "ollama"},
			wantDropped: []string{"foo"},
		},
		{
			name:      "duplicates removed, order preserved",
			raw:       "ollama,claude,ollama",
			wantClean: []string{"ollama", "claude"},
		},
		{
			name:      "trailing commas / empties ignored",
			raw:       "claude,,",
			wantClean: []string{"claude"},
		},
		{
			name:        "all invalid yields empty (caller falls back)",
			raw:         "foo,bar",
			wantClean:   nil,
			wantDropped: []string{"foo", "bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clean, dropped := parseOutfitProviders(tt.raw)
			if !reflect.DeepEqual(clean, tt.wantClean) {
				t.Errorf("providers = %#v, want %#v", clean, tt.wantClean)
			}
			if !reflect.DeepEqual(dropped, tt.wantDropped) {
				t.Errorf("dropped = %#v, want %#v", dropped, tt.wantDropped)
			}
		})
	}
}

func TestValidateCORSOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		env     string
		wantErr error
	}{
		{
			name:    "production with wildcard is rejected",
			origins: []string{"*"},
			env:     "production",
			wantErr: ErrCORSWildcardInProduction,
		},
		{
			name:    "production with wildcard mixed in is rejected",
			origins: []string{"https://app.example.com", "*"},
			env:     "production",
			wantErr: ErrCORSWildcardInProduction,
		},
		{
			name:    "production with empty list is rejected",
			origins: nil,
			env:     "production",
			wantErr: ErrCORSWildcardInProduction,
		},
		{
			name:    "production with explicit origins is accepted",
			origins: []string{"https://app.example.com", "https://admin.example.com"},
			env:     "production",
			wantErr: nil,
		},
		{
			name:    "development with wildcard is allowed",
			origins: []string{"*"},
			env:     "development",
			wantErr: nil,
		},
		{
			name:    "development with empty list is allowed",
			origins: nil,
			env:     "development",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCORSOrigins(tt.origins, tt.env)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateCORSOrigins(%v, %q) = %v, want %v", tt.origins, tt.env, err, tt.wantErr)
			}
		})
	}
}
