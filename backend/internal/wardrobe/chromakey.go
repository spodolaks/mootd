package wardrobe

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg" // register JPEG decoder
	"image/png"
	"math"
	"strings"
)

// chromaKeyTarget maps item colour keywords to the RGB background that the
// image generator places behind the item.  Must stay in sync with
// _CONTRAST_BG in clothDetection/app/services/image_generator.py.
var chromaKeyTarget = []struct {
	keywords []string
	bg       [3]float64 // R, G, B  (0-255)
}{
	{[]string{"green", "olive", "khaki", "army", "lime", "emerald", "teal", "sage", "mint"}, [3]float64{255, 0, 255}},       // magenta
	{[]string{"red", "maroon", "burgundy", "crimson", "scarlet", "wine", "cherry", "coral", "rust"}, [3]float64{0, 255, 255}}, // cyan
	{[]string{"blue", "navy", "cobalt", "indigo", "denim", "azure", "royal"}, [3]float64{255, 255, 0}},                        // yellow
	{[]string{"yellow", "gold", "mustard", "amber", "lemon"}, [3]float64{0, 0, 255}},                                          // blue
	{[]string{"pink", "magenta", "fuchsia", "rose", "blush", "salmon"}, [3]float64{0, 255, 0}},                                // green
	{[]string{"orange", "tangerine", "peach", "apricot"}, [3]float64{0, 0, 255}},                                              // blue
	{[]string{"purple", "violet", "plum", "lavender", "lilac", "mauve"}, [3]float64{0, 255, 0}},                               // green
	{[]string{"white", "cream", "ivory", "beige", "off-white", "eggshell", "snow"}, [3]float64{255, 0, 255}},                  // magenta
	{[]string{"black", "charcoal", "jet"}, [3]float64{255, 255, 0}},                                                           // yellow
	{[]string{"grey", "gray", "silver", "ash", "slate", "heather"}, [3]float64{255, 0, 255}},                                  // magenta
	{[]string{"brown", "tan", "chocolate", "coffee", "camel", "taupe", "mocha"}, [3]float64{0, 255, 255}},                     // cyan
}

var defaultBG = [3]float64{0, 255, 0} // green

// pickBGColor returns the expected chroma-key background RGB for an item
// whose primary colour is described by primaryColor.
func pickBGColor(primaryColor string) [3]float64 {
	lower := strings.ToLower(primaryColor)
	for _, entry := range chromaKeyTarget {
		for _, kw := range entry.keywords {
			if strings.Contains(lower, kw) {
				return entry.bg
			}
		}
	}
	return defaultBG
}

// ChromaKeyRemove replaces every pixel close to the target background colour
// with transparent, returning a PNG with an alpha channel.
//
// tolerance is the max Euclidean distance in RGB space (0-441).  A value of
// ~80 works well for solid chroma-key backgrounds.
func ChromaKeyRemove(imgData []byte, primaryColor string, tolerance float64) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, err
	}

	bg := pickBGColor(primaryColor)
	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			// Convert from 16-bit to 8-bit
			r8, g8, b8 := float64(r>>8), float64(g>>8), float64(b>>8)

			dist := math.Sqrt(
				(r8-bg[0])*(r8-bg[0]) +
					(g8-bg[1])*(g8-bg[1]) +
					(b8-bg[2])*(b8-bg[2]),
			)

			if dist < tolerance {
				// Transparent
				dst.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 0})
			} else if dist < tolerance*1.5 {
				// Feather the edge: partial transparency for smooth anti-aliasing
				alpha := uint8(255 * (dist - tolerance) / (tolerance * 0.5))
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r8), uint8(g8), uint8(b8), alpha})
			} else {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r8), uint8(g8), uint8(b8), uint8(a >> 8)})
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
