// Command seed-surfaces uploads panel and background textures from the
// repo's asset folders into MongoDB, so the running backend can serve
// them to the frontend and include them in LLM outfit prompts.
//
// Usage:
//
//	go run ./cmd/seed-surfaces \
//	  -panels ../app/assets/images/panels \
//	  -backgrounds ../app/assets/images/backgrounds \
//	  -mongo "mongodb://mootd:mootd_dev@localhost:27018/?authSource=admin" \
//	  -db mootd
//
// Metadata format: for each PNG/JPG, an optional sibling `.json` file with
// the same basename supplies name/description/mood/archetype affinity. When
// the sidecar is missing, sensible defaults are derived from the filename
// so the surface is at least usable — just less styled.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/surface"
)

// sidecar is the on-disk format of a surface-metadata .json file.
type sidecar struct {
	Name              string             `json:"name"`
	Description       string             `json:"description,omitempty"`
	MoodTags          []string           `json:"moodTags,omitempty"`
	ArchetypeAffinity map[string]float64 `json:"archetypeAffinity,omitempty"`
}

func main() {
	panelsDir := flag.String("panels", "../app/assets/images/panels", "directory of panel images (+ optional .json sidecars)")
	backgroundsDir := flag.String("backgrounds", "../app/assets/images/backgrounds", "directory of background images (+ optional .json sidecars)")
	mongoURI := flag.String("mongo", os.Getenv("MONGO_URI"), "MongoDB connection string")
	dbName := flag.String("db", envOr("MONGO_DB", "mootd"), "MongoDB database name")
	flag.Parse()

	if *mongoURI == "" {
		log.Fatal("MONGO_URI (or -mongo flag) is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(*mongoURI))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			log.Printf("mongo disconnect: %v", err)
		}
	}()

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("mongo ping: %v", err)
	}

	repo := surface.NewMongoRepository(client, *dbName)

	totalPanels, err := seedDir(ctx, repo, *panelsDir, surface.KindPanel)
	if err != nil {
		log.Fatalf("seed panels: %v", err)
	}
	totalBackgrounds, err := seedDir(ctx, repo, *backgroundsDir, surface.KindBackground)
	if err != nil {
		log.Fatalf("seed backgrounds: %v", err)
	}
	log.Printf("done — %d panel(s), %d background(s) seeded", totalPanels, totalBackgrounds)
}

func seedDir(ctx context.Context, repo *surface.MongoRepository, dir string, kind surface.Kind) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", dir, err)
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			continue
		}

		stem := strings.TrimSuffix(name, filepath.Ext(name))
		imagePath := filepath.Join(dir, name)

		imgData, err := os.ReadFile(imagePath)
		if err != nil {
			log.Printf("  skip %s: read failed: %v", name, err)
			continue
		}

		meta := loadSidecar(filepath.Join(dir, stem+".json"))
		if meta.Name == "" {
			meta.Name = humanize(stem)
		}

		s := surface.Surface{
			ID:                fmt.Sprintf("%s-%s", kind, stem),
			Kind:              kind,
			Name:              meta.Name,
			Description:       meta.Description,
			MoodTags:          meta.MoodTags,
			ArchetypeAffinity: meta.ArchetypeAffinity,
			CreatedAt:         time.Now().UTC(),
		}

		contentType := "image/png"
		if ext == ".jpg" || ext == ".jpeg" {
			contentType = "image/jpeg"
		}

		if err := repo.Upsert(ctx, s, imgData, contentType); err != nil {
			log.Printf("  FAIL %s (%s): %v", s.ID, kind, err)
			continue
		}
		log.Printf("  ok   %s — %s", s.ID, s.Name)
		count++
	}
	return count, nil
}

// loadSidecar returns the parsed JSON metadata at path, or a zero-value
// sidecar if the file is missing or malformed.
func loadSidecar(path string) sidecar {
	var s sidecar
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("  warn %s: malformed sidecar (%v) — using defaults", filepath.Base(path), err)
		return sidecar{}
	}
	return s
}

// humanize converts "studio-bokeh-warm" → "Studio Bokeh Warm" so un-labelled
// surfaces still read as something more than a filename stem in the UI.
func humanize(stem string) string {
	parts := strings.FieldsFunc(stem, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
