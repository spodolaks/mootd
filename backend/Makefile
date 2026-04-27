# Backend developer commands.
#
# `make help` lists all available targets.

# ---- Codegen ---------------------------------------------------------

.PHONY: gen gen-admin gen-check

# OAPI_CODEGEN_VERSION pins the codegen tool. Bumping is a deliberate
# action — the generated files are committed, so a version change
# would surface as a diff in CI.
OAPI_CODEGEN_VERSION = v2.4.1

# `make gen` runs every codegen pass. Today only admin; user-api
# joins when its spec is backfilled (see issue #1 follow-up).
gen: gen-admin

# `make gen-admin` regenerates Go types for the admin API from the
# vendored spec at backend/contracts/admin-api.yaml. Output path is
# explicit (-o) because oapi-codegen's `output:` config field is
# resolved against the cwd, not the config file location, which
# silently writes to surprising places.
gen-admin:
	@echo "→ regenerating admin types from contracts/admin-api.yaml"
	@mkdir -p internal/admin/gen
	@go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) \
		-package gen \
		-generate types \
		-o internal/admin/gen/types.go \
		contracts/admin-api.yaml

# `make gen-check` regenerates and fails if the result differs from
# what's committed. Used by CI to catch:
#   - spec changes that weren't followed by codegen
#   - hand-edits to generated files
gen-check: gen
	@if ! git diff --quiet -- internal/admin/gen/; then \
		echo "✗ generated admin types are out of sync with the spec"; \
		echo "  run 'make gen-admin' and commit the result"; \
		git --no-pager diff -- internal/admin/gen/ | head -40; \
		exit 1; \
	fi
	@echo "✓ generated types are in sync"

# ---- Test / build ---------------------------------------------------

.PHONY: test build

test:
	go test ./...

build:
	go build ./...

# ---- Help -----------------------------------------------------------

.PHONY: help

help:
	@echo "Backend make targets:"
	@echo "  gen           regenerate code from contracts/*.yaml"
	@echo "  gen-admin     regenerate admin types only"
	@echo "  gen-check     regenerate and fail if diff (CI)"
	@echo "  test          go test ./..."
	@echo "  build         go build ./..."
