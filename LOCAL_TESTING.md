# Local testing — full stack

End-to-end recipe to launch everything locally and exercise each surface.

The full stack:

| Surface | Where it runs | Port | Purpose |
|---|---|---|---|
| Backend (user + admin API) | docker container `mootd-backend` | **8089** | Go REST API at `/v1/*` and `/admin/v1/*` |
| MongoDB | docker container `mootd-mongo` | 27018 | All app + admin data |
| Redis | docker container `mootd-redis` | 6379 | Outfit cache, rate limiting, async jobs |
| rembg (background remover) | docker container `mootd-rembg` | 5001 | Detected-item PNG cleanup |
| Mootd app (web) | Metro / Vite (Expo SDK 54) | **8081** | The user app — sign in + wardrobe + moodboard |
| Admin UI | Next.js 16 dev server | **3001** | Internal admin panel — login, dashboard placeholder |

You'll typically have 3 terminals open: backend (foreground or `-d`), mootd app, admin UI.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Docker Desktop | latest | The backend + MongoDB + Redis + rembg run as containers |
| Node | 20+ | For Mootd app (Metro) and admin UI (Next.js) |
| Go | 1.24+ | Optional — only needed if running backend without docker, or running the bootstrap script directly |
| `jq` | any | Pretty-print API responses (`brew install jq`) |
| `curl` | any | Probing the backend |

Repos used:

```
~/Projects2026/
├── mootd/              ← this repo (backend + mootd RN app)
└── mootd-admin/        ← admin panel (Next.js + plan + issues)
    └── apps/admin/     ← the actual Next.js app
```

If `mootd-admin/` isn't cloned yet:
```bash
cd ~/Projects2026
gh repo clone spodolaks/mootd-admin
```

---

## Initial setup (one-time)

### 1. Build + start the backend stack

From `~/Projects2026/mootd`:

```bash
docker compose up --build -d
docker compose ps
```

You should see four containers `Up (healthy)`:
- `mootd-backend`  → 8089
- `mootd-mongo`    → 27018
- `mootd-redis`    → 6379
- `mootd-rembg`    → 5001

Tail backend logs to confirm it actually booted:

```bash
docker compose logs backend | tail -10
```

Look for `Server listening on :8080` and the warnings about dev secrets (those are expected locally).

Quick sanity check:
```bash
curl -sS http://127.0.0.1:8089/healthz
# {"service":"mootd-backend","status":"ok",...}
```

### 2. Bootstrap the first admin

Create the first administrator account so you can log into the admin UI:

```bash
cd ~/Projects2026/mootd
docker run --rm --network mootd_default \
  -e BOOTSTRAP_ADMIN_EMAIL=admin@local.dev \
  -e BOOTSTRAP_ADMIN_PASSWORD='hunter2hunter2dev' \
  -e MONGO_URI='mongodb://mootd:mootd_dev@mongo:27017/?authSource=admin' \
  -v "$PWD":/workspace -w /workspace/backend \
  golang:1.24-alpine \
  sh -c 'apk add --no-cache git && go run ./cmd/bootstrap-admin'
```

Expected: `✓ bootstrapped admin admin@local.dev (id=adm_…) with role=admin`.

Re-running this command **fails on purpose** if any admin already exists — that's the single-bootstrap guard. To recreate:

```bash
docker compose exec mongo mongosh -u mootd -p mootd_dev --authenticationDatabase admin mootd \
  --eval 'db.admins.deleteMany({}); db.admin_refresh_tokens.deleteMany({})'
# then re-run the bootstrap command above
```

### 3. Install npm dependencies (one-time per repo)

```bash
# Mootd app
cd ~/Projects2026/mootd/app
npm install

# Admin UI
cd ~/Projects2026/mootd-admin/apps/admin
npm install
```

### 4. Configure env (one-time)

`~/Projects2026/mootd/app/.env.local` — points the Mootd app at the local backend (overrides the `.env` production URL):

```bash
cat > ~/Projects2026/mootd/app/.env.local <<'EOF'
EXPO_PUBLIC_API_URL=http://127.0.0.1:8089
EXPO_PUBLIC_DATA_SOURCE=api
EOF
```

`~/Projects2026/mootd-admin/apps/admin/.env.local` — only needed if your backend isn't on `127.0.0.1:8089`. The default works without an env file.

---

## Daily launch (every session)

Three terminals, one per surface:

### Terminal 1 — Backend

```bash
cd ~/Projects2026/mootd
docker compose up -d
docker compose logs -f backend
```

(Or omit `-f` if you don't want to tail logs continuously.)

### Terminal 2 — Mootd app (web)

```bash
cd ~/Projects2026/mootd/app
npx expo start --web --clear
```

→ http://localhost:8081

Want **mobile** instead of web? Same backend, different command:

```bash
EXPO_PUBLIC_API_URL=http://$(ipconfig getifaddr en0):8089 npx expo start --dev-client --clear
```

…then reload Mootd on your phone (it'll reach the backend over your LAN IP, not localhost).

### Terminal 3 — Admin UI

```bash
cd ~/Projects2026/mootd-admin/apps/admin
npm run dev
```

→ http://localhost:3001

You'll be redirected to `/login`. Credentials from step 2:

| Field | Value |
|---|---|
| Email | `admin@local.dev` |
| Password | `hunter2hunter2dev` |
| TOTP | (leave blank — Phase-5 lights this up) |

After login → dashboard with three placeholder KPI cards (Phase 1 lights them with real data).

---

## Test scenarios

### Mootd app (user flow)

1. Open http://localhost:8081.
2. Sign in via mock-login (the dev path) — backend has `ENABLE_MOCK_LOGIN=true` set in `docker-compose.yml`. Single click signs you in as `dev.user@mootd.local`.
3. Build wardrobe → upload a clothing photo → wait for detection (uses real detection API if `DETECTION_API_KEY` is set, otherwise falls back to a stub).
4. Generate moodboard → poll completes in 5–30 s → 3–4 outfit cards with thumbs/swipe/swap/save.
5. Save one → calendar shows today's hero.

### Admin UI

After login at http://localhost:3001, verify:

- **Session persistence**: hard-reload (Cmd+R) — still signed in. SessionStorage holds your tokens.
- **Sign out** (top right) → clears session, bounced to `/login`.
- **Cross-tab sign-out**: open `/login` in a second tab, sign in with the *same* account. The first tab will eventually 401 on its next call and refresh; if you wipe sessionStorage in DevTools (Application → Storage → Clear site data), the page redirects to `/login` immediately.
- **Unknown email** vs **wrong password** at `/login` → identical "invalid email or password" toast (no oracle).
- **Token rotation**: in DevTools → Application → Session Storage you'll see `mootd-admin-session-v1`. Refresh-token rotates on each refresh API call (single-use).

### Backend API (curl, no UI)

Quick admin auth check:

```bash
# Login → save tokens
ADMIN_LOGIN=$(curl -sS -X POST http://127.0.0.1:8089/admin/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@local.dev","password":"hunter2hunter2dev"}')
ADMIN_ACCESS=$(echo "$ADMIN_LOGIN" | jq -r .accessToken)
ADMIN_REFRESH=$(echo "$ADMIN_LOGIN" | jq -r .refreshToken)
echo "$ADMIN_LOGIN" | jq

# Refresh rotates the token
curl -sS -X POST http://127.0.0.1:8089/admin/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  -d "{\"refreshToken\":\"$ADMIN_REFRESH\"}" | jq
```

Quick user-side check:

```bash
# Mock-login (dev only) → user JWT
USER=$(curl -sS -X POST http://127.0.0.1:8089/v1/auth/mock-login \
  -H 'Content-Type: application/json' -d '{"provider":"google"}')
USER_TOKEN=$(echo "$USER" | jq -r .accessToken)

# Hit a protected endpoint
curl -sS http://127.0.0.1:8089/v1/wardrobe/items \
  -H "Authorization: Bearer $USER_TOKEN" | jq
```

---

## Database / Redis introspection

**Mongo shell** (always works regardless of cwd):

```bash
docker exec -it mootd-mongo mongosh -u mootd -p mootd_dev --authenticationDatabase admin mootd
```

Useful queries:
```javascript
db.admins.findOne({}, { passwordHash: 0 })
db.admin_refresh_tokens.find({}, { _id: 0, adminId: 1, createdAt: 1, revokedAt: 1, ip: 1 })
db.users.countDocuments()
db.wardrobe_items.countDocuments({ userId: "user_mock_001" })
db.outfits.countDocuments()
db.moodboards.find({}, { date: 1, "outfit.name": 1 }).limit(10).toArray()
```

**Redis CLI**:

```bash
docker exec -it mootd-redis redis-cli
> keys outfit:cache:*
> keys ratelimit:*
> keys outfit:job:*
```

---

## Stopping everything

```bash
# Backend stack
cd ~/Projects2026/mootd
docker compose down            # keeps data
# OR
docker compose down -v          # nukes data — Mongo + Redis volumes wiped

# Metro / Mootd app
# Ctrl-C in the terminal

# Admin UI
# Ctrl-C in the terminal
```

After `down -v`, you'll need to re-bootstrap the admin (step 2 above).

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| Admin UI shows "Could not reach the admin API" toast | Backend not running. `docker compose up -d backend` |
| Admin login: "invalid email or password" | Wrong creds. Check the email in Mongo: `docker exec mootd-mongo mongosh -u mootd -p mootd_dev --authenticationDatabase admin mootd --eval 'db.admins.findOne({}, {email:1, _id:0})'` — if the email matches what you typed, you don't remember the password; wipe + re-bootstrap |
| `bootstrap-admin` fails with "refusing to bootstrap — N admin(s) already exist" | An admin already exists. Wipe the admins collection (see step 2 above), then re-run |
| `docker compose exec mongo …` → "no configuration file provided: not found" | You're not in `~/Projects2026/mootd`. Either `cd` there or use `docker exec mootd-mongo …` |
| Mootd app: `net::ERR_FAILED` on every API call | Wrong `EXPO_PUBLIC_API_URL`. `cat app/.env.local` should show `http://127.0.0.1:8089`. Restart Metro with `--clear` |
| CORS error on `/v1/auth/google` from web | Local backend has `CORS_ALLOWED_ORIGINS=*` set in `docker-compose.yml`. If the error persists, the request is hitting a stale backend — `docker compose up -d --force-recreate backend` |
| `npm install` errors with `Invalid tag name "#"` | You included a `# comment` on the same line; zsh doesn't strip it. Use just `npm install` without comments |
| Port 3001 / 8081 / 8089 already in use | `lsof -ti:<port> \| xargs kill -9`, then restart the relevant service |
| Backend log shows `FATAL: ADMIN_JWT_SECRET must differ from JWT_SECRET` | Both env vars are set to the same value. Either unset both (uses dev defaults) or set them to distinct values |
| Outfit generation hangs / fails | Check `OUTFIT_PROVIDER` in `docker-compose.yml`. Default is `ollama` (needs Ollama running on the host). Set `OUTFIT_PROVIDER=claude` + `ANTHROPIC_API_KEY=...` for hosted Claude |
| Detection: "no DETECTION_API_KEY" | Set `DETECTION_API_KEY` in `~/Projects2026/mootd/.env`, then `docker compose up -d --force-recreate backend` |

---

## Cheat sheet

```bash
# Start everything (backend in background, app + admin foreground in their own terminals)
docker compose up -d                                         # ~/Projects2026/mootd
cd ~/Projects2026/mootd/app && npx expo start --web --clear
cd ~/Projects2026/mootd-admin/apps/admin && npm run dev

# Stop everything
cd ~/Projects2026/mootd && docker compose down
# Ctrl-C the two npm processes

# Wipe + re-bootstrap an admin
docker compose exec mongo mongosh -u mootd -p mootd_dev --authenticationDatabase admin mootd \
  --eval 'db.admins.deleteMany({}); db.admin_refresh_tokens.deleteMany({})'
docker run --rm --network mootd_default \
  -e BOOTSTRAP_ADMIN_EMAIL=admin@local.dev \
  -e BOOTSTRAP_ADMIN_PASSWORD='hunter2hunter2dev' \
  -e MONGO_URI='mongodb://mootd:mootd_dev@mongo:27017/?authSource=admin' \
  -v "$PWD":/workspace -w /workspace/backend \
  golang:1.24-alpine \
  sh -c 'apk add --no-cache git && go run ./cmd/bootstrap-admin'

# Tail all backend logs
docker compose logs -f backend

# Quick API health
curl -sS http://127.0.0.1:8089/healthz | jq
```

---

## Default credentials reference

These are the dev defaults referenced throughout this doc. Don't ever ship them to prod — `ENVIRONMENT=production` makes the backend refuse to start if any of these stay at defaults.

| Surface | User | Password / token |
|---|---|---|
| Mongo | `mootd` | `mootd_dev` (set in `docker-compose.yml`) |
| Admin (after step 2) | `admin@local.dev` | `hunter2hunter2dev` |
| Mootd app (mock-login) | (no creds — single click signs in) | issued by `/v1/auth/mock-login` |
| `JWT_SECRET` | — | `dev-secret-change-in-production-min-32-chars!!` (insecure default; warning on boot) |
| `ADMIN_JWT_SECRET` | — | `admin-dev-secret-change-in-production-min-32-chars!!` (distinct from above) |

---

## What you're testing today vs Phase 1+

Today (Phase 0):
- ✅ All four backend services launch + healthz responds.
- ✅ Mootd app can sign in via mock-login + use the backend.
- ✅ Admin can log in to the admin UI.
- ✅ Token refresh + sign out work.

Not yet (Phase 1, tracked in [mootd-admin issues #6–#17](https://github.com/spodolaks/mootd-admin/issues?q=label%3Aphase-1)):
- ❌ Admin can see real LLM cost / user list / traces / detection runs.
- ❌ Detection re-run from admin.
- ❌ Prompt inspector.

The dashboard cards are placeholders that show what's coming.
