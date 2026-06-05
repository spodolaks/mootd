# MOOTD — Repository Overview

AI wardrobe app: photo → clothing detection → outfit recommendations.

## Monorepo Structure

```
mootd/
├── app/            # React Native + Expo frontend (SDK 54 / RN 0.83)
├── backend/        # Go REST API (Go 1.24, net/http stdlib)
└── docker-compose.yml  # MongoDB 8.0 + Redis 7 + backend
```

Each component has its own CLAUDE.md with detailed conventions:
- [app/CLAUDE.md](app/CLAUDE.md) — frontend architecture, patterns, and conventions
- [backend/CLAUDE.md](backend/CLAUDE.md) — backend architecture, patterns, and conventions

## Running Locally

```bash
# Start MongoDB + Redis + backend (from repo root)
docker compose up --build

# Start frontend (separate terminal)
cd app && npm start
```

**Ports**: backend → `http://127.0.0.1:8081`, MongoDB → `27018`, Redis → `6379`

## Environment Setup

**Backend**: No `.env` needed in `backend/` — all config is in `docker-compose.yml`. Exceptions: set the following in `.env` at repo root as needed:

| Variable | Purpose |
|----------|---------|
| `DETECTION_API_KEY` | External clothing detection service API key |
| `OUTFIT_PROVIDER` | Outfit generation backend: `ollama`, `claude`, or `openai` |
| `OPENAI_API_KEY` | Required when `OUTFIT_PROVIDER=openai` |
| `ANTHROPIC_API_KEY` | Required when `OUTFIT_PROVIDER=claude` |
| `ANTHROPIC_MODEL` | Claude model to use (e.g. `claude-sonnet-4-20250514`). Required when `OUTFIT_PROVIDER=claude` |
| `REDIS_URL` | Redis connection string (default: `redis://redis:6379` in Docker) |

**Frontend**: `app/.env` (copy from `app/.env.example`):
```bash
EXPO_PUBLIC_DATA_SOURCE=api     # 'mock' for offline dev, 'api' for real backend
EXPO_PUBLIC_API_URL=http://127.0.0.1:8081
```

## Architecture Contract

The frontend and backend communicate via REST. The backend is authoritative for:
- Auth token structure: HS256 JWTs with **15-minute access tokens** and **30-day refresh tokens**. Access token claims: `sub`=userID, `iss`=`"mootd"`. The frontend uses a 401 interceptor to silently refresh expired access tokens.
- Wardrobe item shape: `{ id, userId, category, label, imageUrl, traits, createdAt }`
- Detection service mapping: `family` → `category`, `type` → `label`
- Async outfit generation: `POST /v1/outfits/generate` returns a job ID; poll `GET /v1/outfits/jobs/{id}` for results.

See `backend/README.md` for the full API reference.

## Key Design Decisions

- **Semi-microservice backend**: Each domain is a self-contained package (handler + domain types, plus repository/service where needed) to keep AI context windows small. The HTTP-serving domains are `auth`, `user`, `wardrobe`, `outfit`, `moodboard`, `feedback`, `brands`, `generic`, `surface`, `privacy`, `events`, and `health`, alongside a large `admin/` subsystem (MFA, RBAC, audit log, funnels, retention, eval suite, prompt A/B tests, tier routing, budgets) and support packages (`archetype`, `budget`, `observability`, `shared`). See `backend/CLAUDE.md` for the full map.
- **Repository pattern with mock/API switching**: Frontend uses `EXPO_PUBLIC_DATA_SOURCE` to swap between real API and in-memory mocks. Enables offline development.
- **No shared code between frontend and backend**: TypeScript types in `app/src/domain/` are kept in sync with Go structs in `backend/internal/*/domain.go` — update both when changing API shapes. The admin and user APIs additionally have vendored OpenAPI contracts (`backend/contracts/*.yaml`) that generate reference types (`make gen-user` / `gen-admin`); handlers still return hand-written structs, so the generated types are a drift-detection aid, not the wire source.
- **Pluggable outfit generation**: The `OUTFIT_PROVIDER` env var selects between Ollama (local), Claude, and OpenAI for generating outfit recommendations. All three implement the same `OutfitGenerator` interface.
- **Redis for caching and rate limiting**: Outfit results are cached in Redis. Per-user rate limiting and async job state are also stored in Redis.
- **Refresh token auth flow**: Short-lived access tokens (15 min) with long-lived refresh tokens (30 days). The frontend's `apiClient` intercepts 401 responses and transparently refreshes the access token before retrying the request.
