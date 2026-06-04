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

**JWT claims**: `sub` (user ID), `email`, `iat`, `exp` (+24h), `iss` (`"mootd"`)

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

| Variable | Default | Notes |
|----------|---------|-------|
| `HTTP_ADDR` | `:8080` | Server listen address |
| `MONGO_URI` | `mongodb://mootd:mootd_dev@mongo:27017/?authSource=admin` | Change credentials in production |
| `MONGO_DB` | `mootd` | Database name |
| `MONGO_CONNECT_TIMEOUT` | `10s` | |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown grace period |
| `JWT_SECRET` | `dev-secret-change-in-production-min-32-chars!!` | **Must be changed in production** (min 32 chars) |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated list of allowed origins. **Before deploying to production, set this to an explicit list (e.g. `https://app.example.com,https://admin.example.com`).** When `ENVIRONMENT=production`, the server refuses to start if this is `*` or empty. |
| `DETECTION_API_BASE_URL` | `http://35.188.207.123:8080` | External clothing detection service |
| `DETECTION_API_KEY` | *(required)* | API key for detection service — set in `.env` |

The server warns on startup if `JWT_SECRET` or `DETECTION_API_KEY` are unset. When `ENVIRONMENT=production`, the server also refuses to start if `JWT_SECRET` is unset or `CORS_ALLOWED_ORIGINS` is left as the wildcard default.

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
