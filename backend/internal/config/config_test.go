package config

import (
	"errors"
	"testing"
)

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
