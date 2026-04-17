# MOOTD Backend — Architecture & Conventions

Go REST API. Each domain package is self-contained to keep AI context windows manageable.

## Package Layout

```
backend/
├── cmd/api/main.go          # Entry point: config load, DB connect, graceful shutdown
├── internal/
│   ├── app/app.go           # Wires all dependencies, builds middleware stack, registers routes
│   ├── config/config.go     # Env var loading with defaults
│   ├── db/mongo.go          # MongoDB connection (ConnectMongo)
│   ├── auth/                # Google OAuth + JWT issuance + refresh token flow
│   ├── user/                # User profile management
│   ├── wardrobe/            # Clothing detection + item CRUD
│   ├── outfit/              # Outfit generation (async) + recommendations
│   │   ├── domain.go        # Request/response types, OutfitGenerator interface
│   │   ├── handler.go       # HTTP handlers
│   │   ├── service.go       # Business logic (generation orchestration, caching)
│   │   ├── repository.go    # MongoDB persistence
│   │   ├── routes.go        # Route registration
│   │   ├── ollama.go        # OutfitGenerator implementation: Ollama (local)
│   │   ├── claude.go        # OutfitGenerator implementation: Claude (Anthropic)
│   │   └── openai.go        # OutfitGenerator implementation: OpenAI
│   ├── health/              # /healthz + /readyz
│   └── shared/
│       ├── jwt/             # HS256 token generation and validation
│       ├── middleware/      # CORS, Auth (JWT), Logging, RateLimit
│       ├── response/        # WriteJSON helper, strict JSON decoder
│       ├── pagination/      # Cursor-based pagination helpers
│       └── logging/         # Structured logging utilities
```

## Domain Package Convention

Every domain package (`auth`, `user`, `wardrobe`, `outfit`, `health`) follows the same layout:

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
CORS → Logging → RateLimit → [Auth (selective)] → Handler
```

- `middleware.CORS` — applies globally; origin allowlist from `CORS_ALLOWED_ORIGINS`
- `middleware.Logging` — applies globally; logs `METHOD PATH DURATION`
- `middleware.RateLimit` — per-user rate limiting via Redis; applied to expensive endpoints (e.g., outfit generation)
- `middleware.RequireAuth` — applied selectively in route registrations; validates JWT, stores `userID` in `context.Value(middleware.UserIDKey)`

To get user ID in a handler:
```go
userID, ok := middleware.UserIDFromContext(r.Context())
if !ok {
    response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
    return
}
```

## Error Response Convention

All error responses use:
```json
{ "error": "human-readable message" }
```

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
type OutfitGenerator interface {
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
}
```

Three implementations: `OllamaGenerator`, `ClaudeGenerator`, `OpenAIGenerator`. The active implementation is selected at startup based on `OUTFIT_PROVIDER`.

### Async Flow

1. `POST /v1/outfits/generate` — validates request, creates a job in Redis, starts generation in a goroutine, returns `{ jobId }` immediately
2. `GET /v1/outfits/jobs/{id}` — returns job status (`pending`, `running`, `completed`, `failed`) and result when complete
3. Completed outfits are cached in Redis to avoid regeneration for identical inputs

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

Until Go 1.22 path variables are used, extract IDs by trimming the route prefix:
```go
itemID := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/items/")
```

## JSON Helpers

Use `response.WriteJSON(w, status, payload)` for all JSON responses. For decoding requests, use `response.DecodeJSON` which:
- Rejects unknown fields
- Returns 400 on empty body
- Returns structured error messages

## Key Environment Variables

| Variable | Purpose |
|----------|---------|
| `JWT_SECRET` | HMAC signing key — **must change in production** |
| `DETECTION_API_KEY` | External clothing detection service API key |
| `CORS_ALLOWED_ORIGINS` | Comma-separated allowed origins (default: `*`) |
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
