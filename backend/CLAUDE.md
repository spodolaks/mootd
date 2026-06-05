# MOOTD Backend — Architecture & Conventions

Go REST API. Each domain package is self-contained to keep AI context windows manageable.

## Package Layout

```
backend/
├── cmd/
│   ├── api/main.go          # Entry point: config load, DB connect, workers, graceful shutdown
│   └── …                    # Ops/one-shot tools: bootstrap-admin, seed-surfaces, eval, hash-curator, daily-summary-preview
├── internal/
│   ├── app/app.go           # Wires all dependencies, builds the middleware stack, registers routes (large)
│   ├── config/config.go     # Env var loading with defaults + production boot guards
│   ├── db/mongo.go          # MongoDB connection + central index bootstrap (EnsureIndexes)
│   │
│   │  # ── HTTP-serving domains (handler + domain + routes; repository/service where needed) ──
│   ├── auth/                # Google OAuth + JWT issuance + refresh-token flow
│   ├── user/                # User profile management + account-deletion cascade
│   ├── wardrobe/            # Clothing detection (sync + async jobs) + item CRUD
│   ├── outfit/              # Outfit generation (async, pluggable LLM providers) + cache + job store
│   │   ├── generator.go     # Generator interface (Name + Generate) — provider-agnostic
│   │   ├── {ollama,openai,claude}_generator.go  # Provider implementations
│   │   ├── cascade.go       # Ordered provider fallback + per-provider health cooldown
│   │   ├── prompts.go       # System/user prompt construction + injection hardening (v3)
│   │   ├── cache.go / redis_cache.go            # Result cache (Mongo + Redis), 24h TTL
│   │   └── jobs.go          # Async job store (Mongo source-of-truth + Redis cache) + stale sweeper
│   ├── moodboard/           # Saved moodboards (chosen outfit + collage PNG) + image serving
│   ├── feedback/            # Outfit feedback events (thumbs / swaps) for training
│   ├── brands/              # Per-user brand history
│   ├── generic/             # Archetype-default ("filler") catalog items
│   ├── surface/             # Collage panel/background surface assets
│   ├── privacy/             # GDPR data export + account deletion
│   ├── events/              # Client analytics event ingestion (SDK lifecycle)
│   ├── health/              # /healthz + /readyz + /v1/health (client min-version / maintenance)
│   ├── admin/               # Admin subsystem: MFA/TOTP, RBAC, audit log, funnels, retention,
│   │   │                    #   eval suite, prompt A/B tests, tier routing, budgets, reports
│   │   └── gen/             # Generated wire types (DO NOT EDIT — `make gen-admin`)
│   │
│   │  # ── Support packages (no HTTP surface) ──
│   ├── archetype/           # Style-archetype profiles + scoring
│   ├── budget/              # Per-user LLM spend tracking / reservation
│   ├── observability/       # LLM cost ledger (llm_calls) + price table
│   ├── buildinfo/           # Build metadata
│   ├── usergen/             # Generated user-API types (DO NOT EDIT — `make gen-user`)
│   └── shared/
│       ├── jwt/             # HS256 token generation and validation
│       ├── middleware/      # RequestID, Recover, Logging, CORS, RateLimit, auth, admin IP allowlist
│       ├── response/        # JSON helpers + typed error envelope (WriteJSONErrWithCode)
│       ├── pagination/      # Cursor-based pagination helpers
│       ├── metrics/         # Prometheus instrumentation
│       └── logging/         # slog-backed structured logger
```

## Domain Package Convention

The HTTP-serving domains — `auth`, `user`, `wardrobe`, `outfit`, `moodboard`, `feedback`, `brands`, `generic`, `surface`, `privacy`, `events`, `health`, and the `admin` subsystem — broadly follow the same layout, with `repository.go`/`service.go` present only where a domain needs them (e.g. `auth`/`user`/`wardrobe` keep their logic in `handler.go`; `outfit`/`privacy` add a `service.go`; `outfit` persists via `cache.go`/`jobs.go` rather than a `repository.go`):

```
domain/
├── domain.go       # Request/response types and domain models (no business logic)
├── handler.go      # HTTP handlers — HTTP concerns only, delegates to service/repo
├── service.go      # Business logic (present in outfit; other packages may add as needed)
├── repository.go   # Repository interface + MongoRepository implementation
└── routes.go       # Route registration (called by app/app.go)
```

When adding a new endpoint, follow this pattern:
1. Define request/response types in `domain.go`
2. Add method to `Repository` interface and implement on `MongoRepository` in `repository.go`
3. Add business logic in `service.go` (or handler directly for simple CRUD)
4. Add handler method to `Handler` in `handler.go`
5. Register route in `routes.go`
6. Wire the new handler in `app/app.go`

## Middleware Chain

```
metrics.Instrument → RequestID → Recover → Logging → CORS → globalRateLimit
  → [admin IP allowlist on /admin/v1/*] → mux → [Auth (selective, per-route)] → Handler
```

- `metrics.Instrument` — wraps the root handler once; per-route Prometheus duration histogram (route label normalised by `routeLabel` to bound cardinality)
- `RequestID` — honours an inbound `X-Request-ID` (capped at 64 chars) or mints a UUIDv4; echoed in the response and surfaced by `Recover` + the error envelope
- `Recover` — converts a panic into a JSON 500 (logs reqID + userID + stack); the Sentry/DataDog hook point
- `Logging` — logs `METHOD PATH STATUS DURATION`
- `CORS` — origin allowlist from `CORS_ALLOWED_ORIGINS` (refuses `*`/empty when `ENVIRONMENT=production`)
- `globalRateLimit` — global Redis limiter; additional per-route limiters apply to expensive endpoints (outfit generation is 5/min burst + 50/day per user)
- `/admin/v1/*` and `/metrics` are served on a separate mux; admin is gated by an IP allowlist
- **Auth is applied selectively per-route** (not globally) via `RequireAuth`; it validates the JWT and stores `userID` in `context.Value(middleware.UserIDKey)`

To get user ID in a handler:
```go
userID, ok := middleware.UserIDFromContext(r.Context())
if !ok {
    response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
    return
}
```

## Error Response Convention

A typed JSON error envelope is available via `response.WriteJSONErrWithCode`,
emitting `{ "error", "code", "requestId" }` with a stable machine-readable
`code` (see `response.ErrorCode`). This is the preferred form for new
handlers. Many existing handlers still return the older minimal shape:
```json
{ "error": "human-readable message" }
```
and a few method-guards use plain-text `http.Error`; standardising on the
typed envelope is in progress.

Standard status codes:
- `400` — bad request (invalid body, missing field, validation)
- `401` — unauthorized (missing/invalid JWT, or missing credential)
- `404` — not found (item not found or not owned by caller)
- `422` — unprocessable (external service call failed)
- `429` — rate limited (per-user limit exceeded)
- `500` — internal error (DB failure, unexpected)

## Repository Pattern

Each domain exposes a `Repository` interface. The `MongoRepository` struct implements it. Add new data operations by:
1. Adding the method signature to the `Repository` interface
2. Implementing it on `MongoRepository`

Ownership enforcement in queries: always include `userId` in the MongoDB filter alongside `_id` to prevent users from accessing other users' data.

## Cursor-Based Pagination

List endpoints use cursor-based pagination via the `shared/pagination` package:

- Clients send `?cursor=<opaque>&limit=N`
- The cursor encodes the last item's sort key (typically `createdAt` + `_id`)
- Responses include `nextCursor` (empty string when no more results)
- Default limit: 20, max limit: 100

Use the pagination helpers when adding new list endpoints to keep the pattern consistent.

## Outfit Generation

The outfit package uses a pluggable generator pattern:

```go
type Generator interface {
    Name() string
    Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error)
}
```

Three implementations: `OllamaGenerator`, `ClaudeGenerator`, `OpenAIGenerator` (Claude uses Anthropic tool-use with an enum of valid item IDs + optional vision, which makes ID hallucination structurally impossible). Selection is driven by `OUTFIT_PROVIDER`; a `CascadeGenerator` can chain providers with per-provider health cooldowns, and each call returns `*Usage` for the cost ledger.

### Async Flow

1. `POST /v1/outfits/generate` — validates the request, creates a job (MongoDB is the source of truth, Redis a write-through cache), starts generation in a goroutine bounded by a concurrency semaphore (`OUTFIT_MAX_CONCURRENT`, default 8), and returns `{ jobId }` (202). When the client sends `Accept: text/event-stream`, the same endpoint streams per-stage progress over SSE instead.
2. `GET /v1/outfits/jobs/{id}` — returns job status (`pending`, `processing`, `completed`, `failed`) and the result when complete. Ownership-checked (404 on mismatch); a stale-job sweeper fails jobs stuck in `processing` past ~10 min.
3. Completed outfits are cached (24h TTL) to avoid regenerating identical inputs.

## Auth: Refresh Token Flow

The auth package issues two tokens on login:
- **Access token**: 15-minute expiry, used in `Authorization: Bearer` header
- **Refresh token**: 30-day expiry, stored in MongoDB, used to obtain new access tokens

Endpoints:
- `POST /v1/auth/refresh` — accepts `{ refreshToken }`, returns new access + refresh token pair
- Refresh tokens are single-use: each refresh issues a new refresh token and invalidates the old one

## Redis Integration

Redis is used for:
- **Outfit cache**: keyed by hash of wardrobe items + generation params, TTL-based expiry
- **Rate limiting**: per-user sliding window counters for expensive endpoints
- **Async job store**: job status and results for outfit generation jobs

Connection is configured via `REDIS_URL` env var.

## HTTP Method Dispatch Pattern

For routes with multiple methods on the same path (e.g., PATCH + DELETE on `/v1/wardrobe/items/{id}`), use a dispatcher:

```go
func (h *Handler) Item(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodPatch:
        h.updateItem(w, r)
    case http.MethodDelete:
        h.deleteItem(w, r)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}
```

## ID Extraction from URL

Routing is plain `http.ServeMux` with prefix registration; handlers currently extract IDs by trimming the route prefix:
```go
itemID := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/items/")
```
This predates Go 1.22 routing. The project is on **Go 1.24**, so new routes may instead use method+`{id}` patterns and `r.PathValue("id")`; the existing prefix-trim routes have not been migrated.

## JSON Helpers

Use `response.WriteJSON(w, status, payload)` for all JSON responses. For decoding requests, use `response.DecodeJSON` which:
- Rejects unknown fields
- Returns 400 on empty body
- Returns structured error messages

## Key Environment Variables

| Variable | Purpose |
|----------|---------|
| `JWT_SECRET` | HMAC signing key — **must change in production** |
| `ADMIN_JWT_SECRET` | HMAC signing key for admin-panel JWTs. **Must be set in production AND must differ from `JWT_SECRET`** — the backend refuses to start if they match (prevents user-token replay as admin token). |
| `DETECTION_API_KEY` | External clothing detection service API key |
| `CORS_ALLOWED_ORIGINS` | Comma-separated allowed origins (default: `*`). **Must be set to an explicit origin list in production** — the server refuses to start when `ENVIRONMENT=production` and the list is `*` or empty. |
| `MONGO_URI` | MongoDB connection string |
| `DETECTION_API_BASE_URL` | External detection service base URL |
| `OUTFIT_PROVIDER` | Outfit generation backend: `ollama`, `claude`, or `openai` |
| `OPENAI_API_KEY` | OpenAI API key (required when `OUTFIT_PROVIDER=openai`) |
| `ANTHROPIC_API_KEY` | Anthropic API key (required when `OUTFIT_PROVIDER=claude`) |
| `ANTHROPIC_MODEL` | Claude model ID (required when `OUTFIT_PROVIDER=claude`) |
| `REDIS_URL` | Redis connection string (default: `redis://redis:6379`) |

See `README.md` for the full list and defaults.

## Testing an Endpoint Locally

```bash
# Get a dev token
TOKEN=$(curl -sS -X POST http://127.0.0.1:8081/v1/auth/mock-login \
  -H 'Content-Type: application/json' \
  -d '{"provider":"google"}' | jq -r .accessToken)

# Hit a protected endpoint
curl -sS http://127.0.0.1:8081/v1/wardrobe/items \
  -H "Authorization: Bearer $TOKEN"

# Generate an outfit (async)
JOB=$(curl -sS -X POST http://127.0.0.1:8081/v1/outfits/generate \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"occasion":"casual"}' | jq -r .jobId)

# Poll for result
curl -sS http://127.0.0.1:8081/v1/outfits/jobs/$JOB \
  -H "Authorization: Bearer $TOKEN"
```

## Spec-driven types (admin API only, today)

The admin API has a canonical OpenAPI spec at
[mootd-contracts/openapi/admin-api.yaml](https://github.com/spodolaks/mootd-contracts/blob/main/openapi/admin-api.yaml),
vendored into `backend/contracts/admin-api.yaml`. Run `make gen-admin`
to regenerate `internal/admin/gen/types.go` from it.

Hand-written types in `internal/admin/{domain,users,overview,traces}.go`
remain the structs handlers return — the generated package is
**reference + drift detection** only. `make gen-check` regenerates
and diffs the output, but it is **not currently wired into CI**, so
spec/codegen drift is not enforced automatically — run it locally
after changing a contract. (Wiring it into CI is tracked separately.)

When changing an admin endpoint shape:
1. Edit `mootd-contracts/openapi/admin-api.yaml`, push.
2. Update the vendored copy: `cp ../mootd-contracts/openapi/admin-api.yaml backend/contracts/admin-api.yaml`
3. Run `make gen-admin`, commit the new `internal/admin/gen/types.go`.
4. Update the matching hand-written struct in the admin package.
5. Update the corresponding handler.

The user-facing API (`/v1/*`) is not yet spec-driven — see issue
[#1](https://github.com/spodolaks/mootd-admin/issues/1) for the
backfill plan.

## Build & Deploy

```bash
# From repo root
docker compose build backend
docker compose up -d backend

# View logs
docker compose logs -f backend
```

The Dockerfile is a two-stage build: `golang:1.24-alpine` compiles a statically-linked binary; `alpine:3.21` runs it as unprivileged `appuser`.

## API Reference

See [README.md](README.md) for complete endpoint documentation.
