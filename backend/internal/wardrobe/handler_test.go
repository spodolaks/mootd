package wardrobe

import "testing"

func TestIsAllowedImageURL(t *testing.T) {
	tests := []struct {
		url     string
		allowed bool
	}{
		// Valid external URLs
		{"https://images.example.com/photo.jpg", true},
		{"http://cdn.shopify.com/products/img.png", true},

		// SSRF targets — must be blocked
		{"http://localhost:8080/admin", false},
		{"http://127.0.0.1:8080/secret", false},
		{"http://10.0.0.1/internal", false},
		{"http://192.168.1.1/router", false},
		{"http://172.16.0.1/private", false},
		{"http://169.254.169.254/latest/meta-data/", false},
		{"http://metadata.google.internal/computeMetadata/v1/", false},
		{"http://[::1]/admin", false},
		{"http://0.0.0.0/", false},

		// Malformed
		{"", false},
		{"not-a-url", false},
		{"ftp://example.com/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isAllowedImageURL(tt.url)
			if got != tt.allowed {
				t.Errorf("isAllowedImageURL(%q) = %v, want %v", tt.url, got, tt.allowed)
			}
		})
	}
}
