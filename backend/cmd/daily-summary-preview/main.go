// Command daily-summary-preview renders a daily summary against
// the running Mongo and prints it to stdout. Useful for manual
// QA without firing SMTP.
//
// Usage:
//
//	MONGO_URI='mongodb://...' MONGO_DB=mootd \
//	go run ./cmd/daily-summary-preview
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/admin"
)

func main() {
	uri := os.Getenv("MONGO_URI")
	dbName := os.Getenv("MONGO_DB")
	if uri == "" {
		fmt.Fprintln(os.Stderr, "MONGO_URI required")
		os.Exit(1)
	}
	if dbName == "" {
		dbName = "mootd"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	overview := admin.NewOverviewMongoRepository(client, dbName)
	builder := admin.NewDailySummaryBuilder(overview, client, dbName)
	summary, err := builder.Build(ctx, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(admin.RenderDailySummaryText(summary))
}
