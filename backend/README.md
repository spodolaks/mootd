# MOOTD Backend

Go REST API for the MOOTD wardrobe app. Handles authentication, user profiles, AI-powered clothing detection, async outfit generation (pluggable LLM providers), and saved moodboards — backed by MongoDB + Redis.

## Package Structure

Each domain is a self-contained package under `internal/` with its own handler, domain types, and routes (plus a repository/service where needed) — making packages independently navigable and AI-context-friendly.

```
backend/
├── cmd/api/main.go              # Entry point, workers, graceful shutdown  (+ ops tools under cmd/)
├── internal/
│   ├── app/app.go               # Dependency wiring + middleware stack
│   ├── config/config.go         # Env var loading + production boot guards
│   ├── db/mongo.go              # MongoDB connection + index bootstrap
│   │   # HTTP-serving domains (handler + domain + routes; repository/service as needed):
│   ├── auth/  user/  wardrobe/  outfit/  moodboard/  feedback/  brands/
│   ├── generic/  surface/  privacy/  events/  health/
│   ├── admin/                   # Admin subsystem (MFA, RBAC, audit, funnels, evals, budgets) + gen/
│   │   # Support packages:
│   ├── archetype/  budget/  observability/  buildinfo/  usergen/
│   └── shared/                  # jwt, middleware, response, pagination, metrics, logging
└── Dockerfile                   # Multi-stage build (golang:1.24-alpine → alpine:3.21)
```

## Middleware Chain

```
metrics → RequestID → Recover → Logging → CORS → global rate limit → mux → [auth, per-route] → handler
```

Auth is applied **selectively per-route** (most `/v1/*` endpoints; exceptions include `/v1/auth/*`, the health checks, and the public moodboard-image route). It validates the Bearer JWT and stores the user ID in the request context. `/admin/v1/*` is served on a separate, IP-allowlisted mux. See `backend/CLAUDE.md` for the full chain.

## API Reference

Base URL (local): `http://127.0.0.1:8081`

---

### Health

#### `GET /healthz`
Liveness check. Always returns 200 while the process is running.

**Response 200**
```json
{ "status": "ok", "service": "mootd-backend", "time_utc": "2025-03-17T10:30:45Z" }
```

#### `GET /readyz`
Readiness check. Returns 503 if MongoDB is unreachable.

**Response 200**
```json
{ "status": "ok", "service": "mootd-backend", "mongo": "mootd" }
```

**Response 503**
```json
{ "status": "not_ready", "service": "mootd-backend", "reason": "mongo_unreachable" }
```

---

### Authentication

No auth required. Returns a signed JWT to use as `Authorization: Bearer <token>` on all protected routes.

#### `POST /v1/auth/mock-login`
Development-only. Issues a real JWT for a hardcoded mock user without Google credentials.

**Request**
```json
{ "provider": "google" }
```

**Response 200**
```json
{
  "accessToken": "<HS256 JWT>",
  "expiresAt": "2025-03-18T10:30:45Z",
  "user": {
    "id": "user_mock_001",
    "email": "dev.user@mootd.local",
    "name": "MOOTD Dev User",
    "avatarUrl": "https://api.dicebear.com/9.x/initials/svg?seed=MD"
  },
  "mode": "mock"
}
```

**Errors**: `400` invalid body / unsupported provider

---

#### `POST /v1/auth/google`
Authenticates via Google OAuth. Verifies the access token with Google's userinfo endpoint, upserts the user in MongoDB (preserving `createdAt` on repeat logins), and issues a mootd JWT.

**Request**
```json
{ "accessToken": "<google-oauth-access-token>" }
```

**Response 200**
```json
{
  "accessToken": "<HS256 JWT>",
  "expiresAt": "2025-03-18T10:30:45Z",
  "user": {
    "id": "<google-sub>",
    "email": "user@example.com",
    "name": "Jane Doe",
    "avatarUrl": "https://lh3.googleusercontent.com/..."
  },
  "mode": "api"
}
```

**Errors**: `400` missing/invalid body, `401` invalid Google token, `500` save/token error

**Tokens**: Login returns a short-lived **access token** (HS256 JWT, 15-minute expiry) plus a long-lived **refresh token** (opaque, 30-day expiry, single-use — rotated on every `/v1/auth/refresh`). Use the access token as `Authorization: Bearer <token>`; when it expires, exchange the refresh token via `POST /v1/auth/refresh` for a new pair.

**Access-token claims**: `sub` (user ID), `email`, `iat`, `exp` (issued-at + 15 min), `iss` (`"mootd"`). Validation rejects any token whose `iss` is not `"mootd"`.

---

### User Profile

Requires `Authorization: Bearer <jwt>`. User identity is extracted from the JWT — no query parameters needed.

#### `GET /v1/user/profile`
Returns the authenticated user's profile.

**Response 200**
```json
{
  "id": "1234567890",
  "email": "user@example.com",
  "name": "Jane Doe",
  "avatarUrl": "https://example.com/avatar.jpg",
  "googleId": "1234567890",
  "createdAt": "2025-03-12T10:30:45Z",
  "updatedAt": "2025-03-12T10:35:00Z"
}
```

**Errors**: `401` missing/invalid JWT, `404` user not found, `500` fetch error

---

#### `PUT /v1/user/profile`
Updates `name` and/or `avatarUrl`. At least one field must be provided.

**Request**
```json
{ "name": "Jane Doe", "avatarUrl": "https://example.com/new-avatar.jpg" }
```

**Response 200** — same shape as GET

**Errors**: `400` invalid body / no fields to update, `401` unauthorized, `404` user not found, `500` update error

---

#### `DELETE /v1/user/profile`
Erases the authenticated user's account and all owned data via the shared cascade (wardrobe items + their images, outfits, moodboards, feedback, and the user document). Equivalent to `DELETE /v1/privacy/self`; both remain wired for compatibility. Idempotent.

**Response**: `204 No Content`

**Errors**: `401` unauthorized, `500` cascade/deletion failure, `503` cascade not configured (dev mode)

---

### Wardrobe

Requires `Authorization: Bearer <jwt>`. All operations are scoped to the authenticated user.

#### `POST /v1/wardrobe/detect`
Submits a clothing photo for AI detection. Internally:
1. Forwards the image to the external detection service (`DETECTION_API_BASE_URL/jobs`)
2. Polls the job every 3 seconds (60s timeout)
3. Persists each detected item to MongoDB
4. Returns the detected items

**Request**: `multipart/form-data` with field `image` (binary, max 10 MB)

**Response 200**
```json
{
  "items": [
    {
      "id": "a1b2c3d4-...",
      "category": "accessory",
      "label": "belt",
      "imageUrl": "https://storage.googleapis.com/...",
      "confidence": 0.91
    }
  ]
}
```

Items with `skipped: true` from the detection service are omitted. The detection service's `family` field maps to `category`; `type` maps to `label` (legacy `category`/`label` fields used as fallback).

**Errors**: `400` image too large / missing field, `401` unauthorized, `422` detection failed, `500` read/save error

---

#### `GET /v1/wardrobe/items`
Returns all wardrobe items for the authenticated user, newest first.

**Response 200**
```json
{
  "items": [
    {
      "id": "a1b2c3d4-...",
      "userId": "1234567890",
      "category": "accessory",
      "label": "belt",
      "imageUrl": "https://storage.googleapis.com/...",
      "traits": { "color": "black", "material": "leather" },
      "createdAt": "2025-03-17T10:30:45Z"
    }
  ]
}
```

Returns `{ "items": [] }` (never `null`) when the wardrobe is empty.

**Errors**: `401` unauthorized, `500` fetch error

---

#### `PATCH /v1/wardrobe/items/{id}`
Updates the traits map for an item. Only the authenticated user's items can be updated (404 if not found or not owned).

**Request**
```json
{ "traits": { "color": "black", "material": "cotton", "size": "M" } }
```

**Response 200**
```json
{ "status": "ok" }
```

**Errors**: `400` invalid body / missing ID, `401` unauthorized, `404` item not found, `500` update error

---

#### `DELETE /v1/wardrobe/items/{id}`
Permanently removes an item. Only the authenticated user's items can be deleted.

**Response**: `204 No Content`

**Errors**: `401` unauthorized, `404` item not found, `500` delete error

---

#### `POST /v1/wardrobe/items/{id}/search`
Runs an external clothing-catalog lookup against the item's stored photo, filtered by brand. Ownership-checked before the image is read (404 if not found or not owned). Results are not persisted.

**Request**
```json
{ "brand": "Nike" }
```

**Response 200**
```json
{
  "results": [
    {
      "id": "prod_123",
      "title": "Nike Sportswear Tee",
      "source": "example-retailer",
      "price": "$29.99",
      "imageUrl": "https://..."
    }
  ]
}
```

Returns `{ "results": [] }` (never `null`) when there are no matches.

**Errors**: `400` missing item ID / missing `brand` / invalid body, `401` unauthorized, `404` item or image not found / not owned, `422` external search failed, `500` internal error

---

#### `GET /v1/wardrobe/items/{id}/image`
Streams the item's stored image bytes (served from GridFS). **Currently public** (item IDs are non-guessable UUIDs, mirroring the moodboard-image route — no `Authorization` header required). Responses set `Cache-Control: public, max-age=31536000, immutable`.

**Response 200**: raw image bytes with the stored `Content-Type`.

**Errors**: `404` image not found, `500` read error

---

### Outfit Generation

Requires `Authorization: Bearer <jwt>`.

#### `GET /v1/outfits`
Synchronous generation — returns recommended outfits directly. Accepts optional `?temperature=&condition=&unit=` weather params.

#### `POST /v1/outfits/generate`
Async generation (LLM-backed). Returns `202 { "jobId": "..." }`. Honours an optional `Idempotency-Key` header (60s dedupe window). When called with `Accept: text/event-stream`, streams per-stage progress over SSE and returns the result on the same connection instead of requiring polling. **Rate-limited** per user: 5/min burst + 50/day.

#### `GET /v1/outfits/jobs/{id}`
Polls an async job. Status is one of `pending | processing | completed | failed`; `outfits` is populated on completion. Ownership-checked (404 if the job belongs to another user).

#### `POST /v1/outfits/feedback`
Records thumbs-up/down and swap feedback for a generated batch (consumed by the training pipeline).

---

### Moodboards

Requires `Authorization: Bearer <jwt>` (except the image route).

#### `GET /v1/moodboards`
Lists the authenticated user's saved moodboards.

#### `POST /v1/moodboards`
Saves a chosen outfit as a moodboard — persists the outfit, the full generated batch, and a client-captured collage PNG (≤5 MiB, stored in GridFS). One board per user per date (unique index).

#### `GET /v1/moodboards/{id}/image`
Returns the rendered collage PNG. **Currently public** (UUID-as-capability, mirroring the wardrobe-image route); moving to signed/authenticated URLs is tracked separately.

---

### Other Endpoints

Index of the remaining surface (detailed shapes live in each domain's `routes.go` / `handler.go`):

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/auth/refresh` | Rotate the access + refresh token pair (refresh is single-use) |
| `POST` | `/v1/auth/logout` | Invalidate the refresh token (always `204`) |
| `POST` | `/v1/events` | Batch-ingest analytics events (auth + per-user rate limited; body ≤128 KB / 500 events, `413` over cap). Invalid events don't poison the batch — `200` with per-event `accepted`/`rejected` outcomes |
| `POST` / `GET` | `/v1/wardrobe/detect-jobs[/{id}]` | Async clothing-detection: submit + poll |
| `POST` | `/v1/wardrobe/items/from-archetype-default` | Claim an archetype-default ("filler") suggestion into the wardrobe |
| `POST` | `/v1/wardrobe/archetype-rejections` | Reject a filler suggestion so it isn't offered again |
| `POST` / `GET` | `/v1/brands` | Save / list per-user brand history |
| `GET` / `POST` | `/v1/generic/items`, `/v1/generic/trigger` | Archetype-default catalog items |
| `DELETE` | `/v1/privacy/self` | Delete the account + cascade user data |
| `GET` | `/v1/privacy/export` | GDPR data export |
| `GET` | `/v1/surfaces/`, `/v1/surfaces/{id}` | Collage panel/background surface assets |
| `GET` | `/v1/health` | Client min-version + maintenance flag (no DB roundtrip) |
| — | `/admin/v1/*` | Admin subsystem — separate, IP-allowlisted (see `backend/internal/admin`) |

---

## Environment Variables

All defaults are defined in `internal/config/config.go` (and a handful read directly in `internal/app/app.go` / `internal/health/handler.go`). Unset values fall back to the listed default; empty strings are treated as unset.

### Core / server

| Variable | Default | Notes |
|----------|---------|-------|
| `ENVIRONMENT` | `development` | Set to `production` to disable dev-only features (mock-login) and enable production boot guards. |
| `HTTP_ADDR` | `:8080` | Server listen address. |
| `MONGO_URI` | `mongodb://mootd:mootd_dev@mongo:27017/?authSource=admin` | MongoDB connection string — change credentials in production. |
| `MONGO_DB` | `mootd` | Database name. |
| `MONGO_CONNECT_TIMEOUT` | `10s` | MongoDB connect timeout (Go duration string). |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful-shutdown grace period (Go duration string). |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection (outfit cache, rate limiting, async job + spend state). |
| `TRUSTED_PROXY_CIDRS` | *(loopback)* | Comma-separated CIDRs of reverse-proxy peers allowed to set `X-Forwarded-For`. Unset → loopback only. |

### Auth & access control

| Variable | Default | Notes |
|----------|---------|-------|
| `JWT_SECRET` | `dev-secret-change-in-production-min-32-chars!!` | HMAC signing key for user JWTs. **Must be set in production (min 32 chars)** — the server refuses to start with the default in production. |
| `ADMIN_JWT_SECRET` | `admin-dev-secret-change-in-production-min-32-chars!!` | HMAC signing key for admin-panel JWTs. **Must be set in production AND differ from `JWT_SECRET`** — the server refuses to start if they match. |
| `GOOGLE_CLIENT_IDS` | *(built-in web client ID)* | Comma-separated allowlist of Google OAuth client IDs accepted by `/v1/auth/google` (audience binding). Set when using a dedicated iOS/Android client. |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated allowed origins. **Before production, set this to an explicit list (e.g. `https://app.example.com,https://admin.example.com`).** When `ENVIRONMENT=production`, the server refuses to start if this is `*` or empty. |
| `ADMIN_ALLOWED_IPS` | *(empty)* | Comma-separated CIDR/IP allowlist gating `/admin/v1/*` and `/metrics`. **Required in production** — an empty list fails open, so the server refuses to start without it when `ENVIRONMENT=production`. |
| `ENABLE_MOCK_LOGIN` | `false` | Set to `true` to enable `POST /v1/auth/mock-login`. Fail-closed: ignored when `ENVIRONMENT=production`. |

### Clothing detection

| Variable | Default | Notes |
|----------|---------|-------|
| `DETECTION_BACKEND` | `singleitem` | Detection backend selector: `singleitem`, `flatlay`, or `legacy`. Unknown values fall back to `singleitem` (which itself falls back to legacy if its URL is unset). |
| `DETECTION_API_BASE_URL` | `http://localhost:8000` | Base URL for the legacy detection service (also used as the fallback backend). |
| `DETECTION_API_KEY` | *(empty)* | API key for the legacy detection service — set in `.env`. Server warns if unset. |
| `SINGLEITEM_BASE_URL` | *(empty)* | Base URL for the single-item detection orchestrator (used when `DETECTION_BACKEND=singleitem`). |
| `SINGLEITEM_API_KEY` | *(empty)* | API key for the single-item orchestrator. |
| `FLATLAY_BASE_URL` | *(empty)* | Base URL for the third-party flat-lay detection service (used when `DETECTION_BACKEND=flatlay`). |
| `FLATLAY_API_KEY` | *(empty)* | API key for the flat-lay service. |
| `BG_REMOVER_BASE_URL` | `http://host.docker.internal:8010` | Background-removal service base URL. |
| `DETECT_MAX_CONCURRENT` | `8` | Max concurrent in-flight detection jobs. |

### Outfit generation

| Variable | Default | Notes |
|----------|---------|-------|
| `OUTFIT_PROVIDER` | `ollama` | Generator backend — a single provider (`ollama`/`claude`/`openai`) or a comma-separated cascade chain (e.g. `claude,openai,ollama`). Unknown entries are dropped. |
| `OUTFIT_MAX_CONCURRENT` | `8` | Max concurrent in-flight async outfit-generation jobs. |
| `OUTFIT_CRITIC_ENABLED` | `false` | `true` enables the optional outfit-critic QA gate. |
| `OUTFIT_PER_ARCHETYPE_PROMPTS` | `false` | `true` enables per-archetype prompt variants. |
| `ANTHROPIC_BASE_URL` | `https://api.anthropic.com` | Anthropic Messages API endpoint (Claude provider). |
| `ANTHROPIC_API_KEY` | *(empty)* | Anthropic API key. Required when the provider chain includes `claude`; server warns if missing. |
| `ANTHROPIC_MODEL` | `claude-sonnet-4-5` | Claude model ID (Claude provider). |
| `ANTHROPIC_VISION` | `true` | `true`/`false` — send item PNGs to Claude for visual reasoning. |
| `OPENAI_BASE_URL` | `https://api.openai.com` | OpenAI API endpoint (OpenAI provider / DALL-E backgrounds). |
| `OPENAI_API_KEY` | *(empty)* | OpenAI API key. Required when the provider chain includes `openai`. |
| `OLLAMA_BASE_URL` | `http://host.docker.internal:11434` | Local Ollama endpoint (Ollama provider). |
| `OLLAMA_MODEL` | `qwen3:14b` | Ollama model used for outfit generation. |

### Client health & ops

| Variable | Default | Notes |
|----------|---------|-------|
| `MIN_CLIENT_VERSION` | *(empty)* | Minimum RN-app version surfaced by `GET /v1/health`; older clients show a blocking update prompt. |
| `MAINTENANCE` | `false` | `true` flips the maintenance flag on `GET /v1/health` (soft banner). |
| `FOUNDER_EMAILS` | *(empty)* | Comma-separated recipients for the admin daily-summary email (requires SMTP configured). |
| `DAILY_SUMMARY_HOUR_UTC` | `7` | Hour (UTC) the daily-summary email is sent. |
| `EVAL_GOLDEN_DIR` | *(built-in)* | Overrides the directory of golden eval fixtures for the eval suite. |
| `SMTP_HOST` | *(empty)* | SMTP server host. When unset, the weekly cost-report / daily-summary email send is disabled (previews still work). |
| `SMTP_PORT` | *(empty)* | SMTP server port. |
| `SMTP_USERNAME` | *(empty)* | SMTP auth username. |
| `SMTP_PASSWORD` | *(empty)* | SMTP auth password. |
| `SMTP_FROM` | *(empty)* | Sender address. Required (with `SMTP_TO`) for email send to be enabled. |
| `SMTP_TO` | *(empty)* | Default recipient address for the weekly cost report. |

> **Ops one-shot tools only** (not read by the API server): `cmd/bootstrap-admin` reads `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD` (min 12 chars) to seed the first admin account.

The server warns on startup if `JWT_SECRET`, `ADMIN_JWT_SECRET`, `DETECTION_API_KEY`, or a required provider API key are unset. When `ENVIRONMENT=production`, the server refuses to start if `JWT_SECRET`/`ADMIN_JWT_SECRET` are unset (or equal), if `CORS_ALLOWED_ORIGINS` is the wildcard default, or if `ADMIN_ALLOWED_IPS` is empty.

## Running Locally

From the repository root:

```bash
# Start MongoDB + backend
docker compose up --build

# Verify
curl -sS http://127.0.0.1:8081/healthz
curl -sS http://127.0.0.1:8081/readyz

# Get a dev token
TOKEN=$(curl -sS -X POST http://127.0.0.1:8081/v1/auth/mock-login \
  -H 'Content-Type: application/json' \
  -d '{"provider":"google"}' | jq -r .accessToken)

# List wardrobe items
curl -sS http://127.0.0.1:8081/v1/wardrobe/items \
  -H "Authorization: Bearer $TOKEN"
```

## Database

MongoDB 8.0. Core collections:

**`users`** — `_id` (string, Google sub), `email`, `name`, `avatarUrl`, `googleId` (unique index), `createdAt`, `updatedAt`

**`wardrobe_items`** — `_id` (UUID v4), `userId` (indexed), `category`, `label`, `imageUrl`, `pngImageUrl?`, `traits` (map), `createdAt` (sorted desc)

Plus per-domain collections including `moodboards` (unique `userId`+`date`), `outfit_feedback`, `outfit_cache`, `generic_items`, `llm_calls` (cost ledger), and the `admin.*` collections. Redis holds the outfit cache, rate-limit counters, async job state, and per-user spend tracking.
