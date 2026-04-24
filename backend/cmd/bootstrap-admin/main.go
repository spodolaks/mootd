// Command bootstrap-admin creates the first administrator record so the
// Mootd admin panel has someone to log in as. Fails loudly if any admin
// already exists — a restart of a misconfigured production deploy must
// never silently create a second "admin" account.
//
// Usage:
//
//	BOOTSTRAP_ADMIN_EMAIL=you@example.com \
//	BOOTSTRAP_ADMIN_PASSWORD='<min-12-chars>' \
//	MONGO_URI='mongodb://...' \
//	go run ./cmd/bootstrap-admin
//
// Environment variables — all required:
//
//	BOOTSTRAP_ADMIN_EMAIL       the email address (login credential)
//	BOOTSTRAP_ADMIN_PASSWORD    the password (min 12 chars)
//	MONGO_URI                   same Mongo the backend uses
//	MONGO_DB                    database name; defaults to "mootd"
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/admin"
)

func main() {
	logger := log.New(os.Stderr, "[bootstrap-admin] ", log.LstdFlags)

	email := strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	mongoURI := os.Getenv("MONGO_URI")
	dbName := envOr("MONGO_DB", "mootd")

	if email == "" {
		logger.Fatal("BOOTSTRAP_ADMIN_EMAIL is required")
	}
	if password == "" {
		logger.Fatal("BOOTSTRAP_ADMIN_PASSWORD is required")
	}
	if len(password) < 12 {
		logger.Fatal("BOOTSTRAP_ADMIN_PASSWORD must be at least 12 characters")
	}
	if mongoURI == "" {
		logger.Fatal("MONGO_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		logger.Fatalf("mongo connect: %v", err)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()
	if err := client.Ping(ctx, nil); err != nil {
		logger.Fatalf("mongo ping: %v", err)
	}

	repo, err := admin.NewMongoRepository(ctx, client, dbName)
	if err != nil {
		logger.Fatalf("admin repo init: %v", err)
	}

	count, err := repo.CountAdmins(ctx)
	if err != nil {
		logger.Fatalf("count admins: %v", err)
	}
	if count > 0 {
		logger.Fatalf("refusing to bootstrap — %d admin(s) already exist. Use the admin panel to add more.", count)
	}

	hash, err := admin.HashPassword(password)
	if err != nil {
		logger.Fatalf("hash password: %v", err)
	}

	now := time.Now().UTC()
	newAdmin := admin.Admin{
		ID:           generateID(),
		Email:        email,
		PasswordHash: hash,
		Roles:        []admin.Role{admin.RoleAdmin},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctx, newAdmin); err != nil {
		logger.Fatalf("create admin: %v", err)
	}

	fmt.Printf("✓ bootstrapped admin %s (id=%s) with role=admin\n", email, newAdmin.ID)
	fmt.Println("  MFA is not enrolled yet — P5-02 will add TOTP on first login.")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// generateID returns a 16-byte random hex ID — same shape as other IDs
// in the backend but without taking a dep on the shared package (keeps
// the bootstrap binary small and self-contained).
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "adm_" + hex.EncodeToString(b)
}
