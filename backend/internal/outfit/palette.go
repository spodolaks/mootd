package outfit

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sync"
)

// extractDominantColor returns the dominant opaque color of an image as a
// "#RRGGBB" hex string. It ignores near-transparent pixels so PNG cutouts
// vote only with garment pixels, not the removed background.
//
// Algorithm:
//   - Downsample by stride so at most ~4k pixels are examined.
//   - Quantize each opaque pixel into one of 8^3 = 512 buckets.
//   - Return the centroid of the most-populated bucket.
func extractDominantColor(data []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	total := b.Dx() * b.Dy()
	if total == 0 {
		return "", errors.New("empty image")
	}
	const maxSamples = 4000
	stride := 1
	if total > maxSamples {
		stride = int(math.Ceil(math.Sqrt(float64(total) / float64(maxSamples))))
	}

	const levels = 8 // 8x8x8 = 512 buckets
	type bucket struct{ r, g, bl, n uint64 }
	buckets := make(map[int]*bucket, 128)

	for y := b.Min.Y; y < b.Max.Y; y += stride {
		for x := b.Min.X; x < b.Max.X; x += stride {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			r8, g8, bl8 := uint8(r>>8), uint8(g>>8), uint8(bl>>8)
			key := (int(r8)/(256/levels))*levels*levels +
				(int(g8)/(256/levels))*levels +
				(int(bl8) / (256 / levels))
			bkt := buckets[key]
			if bkt == nil {
				bkt = &bucket{}
				buckets[key] = bkt
			}
			bkt.r += uint64(r8)
			bkt.g += uint64(g8)
			bkt.bl += uint64(bl8)
			bkt.n++
		}
	}
	if len(buckets) == 0 {
		return "", errors.New("no opaque pixels")
	}
	var best *bucket
	for _, bkt := range buckets {
		if best == nil || bkt.n > best.n {
			best = bkt
		}
	}
	return fmt.Sprintf("#%02X%02X%02X",
		best.r/best.n, best.g/best.n, best.bl/best.n), nil
}

// paletteContainsNear reports whether hex is within euclidean RGB distance
// `thresh` of any color already in palette. Used to dedupe near-duplicates
// (e.g. navy shirt + denim jeans both vote #2A3A5C) so the 4-chip strip
// reads as a real palette, not three-shades-of-blue.
func paletteContainsNear(palette []string, candidate string, thresh float64) bool {
	cr, cg, cb, ok := hexToRGB(candidate)
	if !ok {
		return false
	}
	for _, p := range palette {
		pr, pg, pb, ok := hexToRGB(p)
		if !ok {
			continue
		}
		dr := float64(cr) - float64(pr)
		dg := float64(cg) - float64(pg)
		db := float64(cb) - float64(pb)
		if math.Sqrt(dr*dr+dg*dg+db*db) < thresh {
			return true
		}
	}
	return false
}

func hexToRGB(s string) (r, g, b uint8, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	raw, err := hex.DecodeString(s[1:])
	if err != nil || len(raw) != 3 {
		return 0, 0, 0, false
	}
	return raw[0], raw[1], raw[2], true
}

// attachPalettes samples the dominant color of each unique wardrobe item
// referenced by the outfits and assigns a per-outfit Palette of up to 4
// deduped colors (in render/item order).
//
// Extraction runs concurrently with a per-item timeout inherited from ctx.
// Items that fail to load or decode are silently dropped from the palette —
// the card renders fewer chips instead of surfacing errors.
func (s *Service) attachPalettes(ctx context.Context, outfits []Outfit) []Outfit {
	if len(outfits) == 0 || s.wardrobe == nil {
		return outfits
	}
	needed := make(map[string]struct{})
	for _, o := range outfits {
		for _, id := range o.Items {
			needed[id] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return outfits
	}

	colorByID := make(map[string]string, len(needed))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var fetchFails, decodeFails, extracts int
	var failMu sync.Mutex
	for id := range needed {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					s.logger.Printf("outfit: palette extract panic for item %s: %v", id, r)
				}
			}()
			// Prefer the background-removed PNG variant if it exists — it's always
			// in PNG format (stdlib-decodable) and its transparent pixels let the
			// extractor ignore background noise naturally. Fall back to the
			// original upload if the PNG variant isn't stored yet.
			data, ct, err := s.wardrobe.GetImage(ctx, id+"-png")
			if err != nil || len(data) == 0 {
				data, ct, err = s.wardrobe.GetImage(ctx, id)
			}
			if err != nil || len(data) == 0 {
				failMu.Lock()
				fetchFails++
				failMu.Unlock()
				s.logger.Printf("outfit: palette: fetch image for %s failed: %v (bytes=%d)", id, err, len(data))
				return
			}
			hexColor, err := extractDominantColor(data)
			if err != nil {
				failMu.Lock()
				decodeFails++
				failMu.Unlock()
				s.logger.Printf("outfit: palette: decode for %s failed (bytes=%d, type=%s): %v", id, len(data), ct, err)
				return
			}
			mu.Lock()
			colorByID[id] = hexColor
			mu.Unlock()
			failMu.Lock()
			extracts++
			failMu.Unlock()
		}(id)
	}
	wg.Wait()
	s.logger.Printf("outfit: palette: %d items, extracted=%d fetchFails=%d decodeFails=%d",
		len(needed), extracts, fetchFails, decodeFails)

	const dedupThreshold = 40.0
	const maxChips = 4
	for i := range outfits {
		var palette []string
		for _, id := range outfits[i].Items {
			c := colorByID[id]
			if c == "" {
				continue
			}
			if paletteContainsNear(palette, c, dedupThreshold) {
				continue
			}
			palette = append(palette, c)
			if len(palette) >= maxChips {
				break
			}
		}
		outfits[i].Palette = palette
	}
	return outfits
}
