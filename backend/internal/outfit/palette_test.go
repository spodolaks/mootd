package outfit

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestHexToRGB(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		wantR         uint8
		wantG         uint8
		wantB         uint8
		wantOK        bool
	}{
		{"lowercase", "#112233", 17, 34, 51, true},
		{"uppercase", "#AABBCC", 170, 187, 204, true},
		{"missing hash", "112233", 0, 0, 0, false},
		{"short", "#123", 0, 0, 0, false},
		{"long", "#11223344", 0, 0, 0, false},
		{"non-hex", "#zzzzzz", 0, 0, 0, false},
		{"empty", "", 0, 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, g, b, ok := hexToRGB(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && (r != tc.wantR || g != tc.wantG || b != tc.wantB) {
				t.Errorf("rgb = (%d,%d,%d), want (%d,%d,%d)", r, g, b, tc.wantR, tc.wantG, tc.wantB)
			}
		})
	}
}

func TestPaletteContainsNear(t *testing.T) {
	tests := []struct {
		name      string
		palette   []string
		candidate string
		thresh    float64
		want      bool
	}{
		{
			name:      "near-duplicate navy → true",
			palette:   []string{"#1E2F5C"},
			candidate: "#1F305D",
			thresh:    40,
			want:      true,
		},
		{
			name:      "navy vs tan → false",
			palette:   []string{"#1E2F5C"},
			candidate: "#D2B48C",
			thresh:    40,
			want:      false,
		},
		{
			name:      "empty palette → false",
			palette:   nil,
			candidate: "#112233",
			thresh:    40,
			want:      false,
		},
		{
			name:      "invalid candidate → false",
			palette:   []string{"#112233"},
			candidate: "bogus",
			thresh:    40,
			want:      false,
		},
		{
			name:      "invalid palette entry is skipped",
			palette:   []string{"garbage", "#1E2F5C"},
			candidate: "#1F305D",
			thresh:    40,
			want:      true,
		},
		{
			name:      "exact match under any threshold",
			palette:   []string{"#808080"},
			candidate: "#808080",
			thresh:    1,
			want:      true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := paletteContainsNear(tc.palette, tc.candidate, tc.thresh)
			if got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExtractDominantColor_SolidRed encodes a 4x4 all-#FF0000 PNG in memory
// and checks the extracted dominant color matches exactly.
func TestExtractDominantColor_SolidRed(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	red := color.NRGBA{R: 0xFF, G: 0x00, B: 0x00, A: 0xFF}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, red)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	got, err := extractDominantColor(buf.Bytes())
	if err != nil {
		t.Fatalf("extractDominantColor: %v", err)
	}
	if got != "#FF0000" {
		t.Errorf("got = %q, want %q", got, "#FF0000")
	}
}

// TestExtractDominantColor_TransparencyIgnored builds a PNG where the
// background is transparent and a green square dominates the opaque pixels.
// The transparent region must not influence the dominant color.
func TestExtractDominantColor_TransparencyIgnored(t *testing.T) {
	const size = 10
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	// Fully transparent background (zero alpha).
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x00})
		}
	}
	// Opaque green block — the only pixels the extractor should vote on.
	green := color.NRGBA{R: 0x00, G: 0xFF, B: 0x00, A: 0xFF}
	for y := 2; y < 8; y++ {
		for x := 2; x < 8; x++ {
			img.SetNRGBA(x, y, green)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	got, err := extractDominantColor(buf.Bytes())
	if err != nil {
		t.Fatalf("extractDominantColor: %v", err)
	}
	if got != "#00FF00" {
		t.Errorf("got = %q, want %q — transparent pixels leaked into the vote", got, "#00FF00")
	}
}

func TestExtractDominantColor_InvalidBytes(t *testing.T) {
	if _, err := extractDominantColor([]byte("not-an-image")); err == nil {
		t.Fatal("expected error decoding non-image bytes, got nil")
	}
}

func TestExtractDominantColor_FullyTransparent(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{A: 0x00})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if _, err := extractDominantColor(buf.Bytes()); err == nil {
		t.Fatal("expected error for fully-transparent PNG, got nil")
	}
}
