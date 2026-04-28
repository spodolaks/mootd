// Package buildinfo holds compile-time identity of the running backend.
//
// Variables here are populated via -ldflags at build time:
//
//	go build -ldflags "\
//	  -X mootd/backend/internal/buildinfo.Version=$(cat VERSION) \
//	  -X mootd/backend/internal/buildinfo.SHA=$(git rev-parse --short HEAD) \
//	  -X mootd/backend/internal/buildinfo.BuiltAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
//	  ./cmd/api
//
// Defaults are sentinel "dev" values so a local build (no flags) still
// produces a coherent BuildInfo response without crashing.
package buildinfo

// Version is the human-readable release tag (e.g. "0.2.0"). Defaults to
// "0.0.0-dev" when unset.
var Version = "0.0.0-dev"

// SHA is the short git commit SHA. Defaults to "dev" when unset.
var SHA = "dev"

// BuiltAt is the RFC-3339 build timestamp. Empty when unset (the admin
// UI hides the "built at" caption when this is empty).
var BuiltAt = ""
