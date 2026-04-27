package admin

import "go.mongodb.org/mongo-driver/v2/mongo/options"

// findOpts is a tiny wrapper so callers don't have to import the
// options package directly when building a SetSort+SetLimit chain.
// Pure ergonomics — no behaviour change vs calling
// options.Find() directly.
func findOpts() *options.FindOptionsBuilder {
	return options.Find()
}
