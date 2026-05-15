# mootd — full-stack setup on a fresh server

Runbook for bringing up the entire mootd stack (backend, admin panel,
detection orchestrator, mobile app dev) on a new Linux server. Single
source of truth across the four repos — point new operators here.

If you only need to redeploy an already-set-up server, see
[`deploy/README.md`](deploy/README.md) for the backend-specific cycle.

---

## 0 · Architecture map

```
                              ┌──────────────────────────────┐
                              │  Cloudflare (DNS + edge TLS) │
                              └──────────────┬───────────────┘
                                             │
                       ┌─────────────────────┼─────────────────────┐
                       │                     │                     │
                  admin.*               api.*                spodolaks.id.lv
                       │                     │                     │
                       ▼                     ▼                     ▼
              ┌─────────────────────────────────────────────────────────┐
              │  Server (Linux, public IP) — Caddy on :443 origin TLS  │
              │  ┌──────────────┐     ┌─────────────┐                   │
              │  │  admin       │     │  backend    │                   │
              │  │  Next.js     │     │  Go API     │  ← Docker compose │
              │  │  :3001 (lo)  │     │  :8089 (lo) │     "mootd"       │
              │  └──────────────┘     └──────┬──────┘                   │
              │                              │                          │
              │                       ┌──────┴──────┐                   │
              │                       ▼             ▼                   │
              │                  ┌──────────┐  ┌──────────┐             │
              │                  │  mongo   │  │  redis   │ (private)   │
              │                  └──────────┘  └──────────┘             │
              │                                                          │
              │  ┌────────────────────────────────────────────────────┐ │
              │  │  singleItemDetection orchestrator — Docker compose │ │
              │  │  Go API + own Mongo, host port :8090               │ │
              │  └────────────────────────────────────────────────────┘ │
              └─────────────────────────────────────────────────────────┘

Mobile clients (Expo / React Native) talk to api.spodolaks.id.lv
directly. They aren't deployed on the server — they run on dev
machines or in the App Store / Play Store.
```

### The four repos

| Repo | Role | GitHub |
|---|---|---|
| **mootd** | Go REST API (`api.*`) + React Native mobile app | <https://github.com/spodolaks/mootd> |
| **mootd-admin** | Next.js admin panel (`admin.*`) | <https://github.com/spodolaks/mootd-admin> |
| **mootd-contracts** | OpenAPI specs vendored into the two above | <https://github.com/spodolaks/mootd-contracts> |
| **singleItemDetection** | Go orchestrator (detect → describe → ghost-mannequin generate → HITL gate). Optional; mootd can also use `flatlay` or `legacy` backends. | <https://github.com/spodolaks/singleItemDetection> |

---

## 1 · Prerequisites

### Server

- **Linux** (Ubuntu 22.04 or 24.04 tested; Debian 12 works).
- **Public IP**, port 80 + 443 reachable from the internet.
- **≥ 4 GB RAM**, **≥ 2 vCPU**, **≥ 30 GB disk**. The orchestrator's
  pipeline is the heaviest component; double these numbers when you
  enable real Stage 1 detection (Replicate-backed).

### DNS (Cloudflare)

Three A records, all proxied (orange cloud):

| Type | Name | Content | Proxy |
|---|---|---|---|
| A | `api` | server public IP | 🟠 Proxied |
| A | `admin` | server public IP | 🟠 Proxied |
| A | `@` | server public IP | 🟠 Proxied |

SSL/TLS mode: **Full (strict)** once Caddy has its first cert (start
on **Full** to let Caddy obtain certs, then upgrade).

### Accounts + secrets you'll collect along the way

| Secret | Used by | Where to get it |
|---|---|---|
| `OPENAI_API_KEY` | mootd (outfit gen, moodboard textures) | <https://platform.openai.com/api-keys> |
| `ANTHROPIC_API_KEY` | mootd (admin archetype-defaults autodetect, optional outfit critic) | <https://console.anthropic.com/> |
| `REPLICATE_TOKEN` | singleItemDetection (Stage 1 detect+segment, Stage 3 generate) | <https://replicate.com/account/api-tokens> |
| `SID_API_KEYS`, `SID_ADMIN_API_KEYS` | singleItemDetection (auth for public + admin endpoints) | generated values, see step 4 |
| `JWT_SECRET`, `ADMIN_JWT_SECRET` | mootd backend (must differ; backend refuses to start when equal) | generate with `openssl rand -hex 32` |
| `DETECTION_API_KEY` | mootd backend (legacy detection backend; required only if you keep that arm) | inherited from the legacy detection service |
| Cloudflare account | DNS, edge TLS | <https://dash.cloudflare.com/> |

---

## 2 · Server bootstrap

SSH in as root. Install Docker + Caddy + git. (Already documented
in [`deploy/README.md`](deploy/README.md) — repeat here for
self-containedness.)

```bash
# Docker
apt-get update
apt-get install -y ca-certificates curl git
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
  https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" \
  > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Caddy
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | tee /etc/apt/sources.list.d/caddy-stable.list
apt-get update
apt-get install -y caddy

# Non-root user for the application (recommended)
useradd -m -s /bin/bash -G docker mootd
mkdir -p /home/mootd/src && chown -R mootd:mootd /home/mootd/src
```

Open the firewall for HTTP/HTTPS only (Caddy handles both):

```bash
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
```

Switch to the application user for the rest:

```bash
su - mootd
cd /home/mootd/src
```

---

## 3 · Clone all repos

```bash
git clone https://github.com/spodolaks/mootd.git
git clone https://github.com/spodolaks/mootd-admin.git
git clone https://github.com/spodolaks/mootd-contracts.git    # optional on the server; only needed if you regenerate types
git clone https://github.com/spodolaks/singleItemDetection.git
```

Layout after this:

```
/home/mootd/src/
├── mootd/
├── mootd-admin/
├── mootd-contracts/
└── singleItemDetection/
```

`mootd-contracts` is the OpenAPI source of truth. The mootd backend
vendors `mootd-contracts/openapi/admin-api.yaml` into
`mootd/backend/contracts/admin-api.yaml`; mootd-admin vendors it into
`mootd-admin/apps/admin/contracts/admin-api.yaml`. On a stable
deploy, just keep using the vendored copy and pull mootd-contracts
only when you regenerate types.

---

## 4 · Stage A — singleItemDetection orchestrator

The orchestrator is mootd's preferred detection backend
(`DETECTION_BACKEND=singleitem`). Bring it up first because mootd
depends on it.

```bash
cd /home/mootd/src/singleItemDetection
cp .env.example .env
$EDITOR .env
```

**Required env in `singleItemDetection/.env`:**

```bash
# Auth
SID_API_KEYS=$(openssl rand -hex 32)                # public endpoints (/v1/single-item/*)
SID_ADMIN_API_KEYS=$(openssl rand -hex 32)          # admin endpoints (/v1/admin/*) — mootd's HitlProxy needs this

# Replicate (for real Stage 1 detection)
REPLICATE_TOKEN=r8_...
USE_REAL_STAGE1=true                                # without this, the pipeline returns mock data
GROUNDING_DINO_MODEL=adirik/grounding-dino@<sha>    # pin a published version you've verified
SAM3_MODEL=meta/sam-2@<sha>

# Stage 2 (describer) — at least one of:
ANTHROPIC_API_KEY=sk-ant-api03-...
OPENAI_API_KEY=sk-proj-...                          # for ensemble; optional

# Mongo — orchestrator has its OWN mongo (not shared with mootd)
MONGO_URI=mongodb://mongo:27017
MONGO_DB=singleitemdetection
```

Bring it up:

```bash
docker compose up -d
docker compose logs -f orchestrator | grep "http listener up"   # wait for the readiness line
curl -fsS -H "X-API-Key: $SID_API_KEYS" http://localhost:8090/readyz
```

The orchestrator now listens on `:8090` on the loopback. mootd talks
to it via `http://host.docker.internal:8090` (Docker → host loopback;
on Linux you may need to add `extra_hosts: ["host.docker.internal:host-gateway"]`
to the mootd compose — already done in the shipping file).

See [`singleItemDetection/docs/runbook.md`](https://github.com/spodolaks/singleItemDetection/blob/main/docs/runbook.md)
for tuning + observability.

---

## 5 · Stage B — mootd backend (api.spodolaks.id.lv)

```bash
cd /home/mootd/src/mootd
cp .env.example .env       # or hand-create from the secrets in §1
$EDITOR .env
```

**Required env in `mootd/.env`:**

```bash
# JWT (MUST differ — backend refuses to start when they match)
JWT_SECRET=<openssl rand -hex 32>
ADMIN_JWT_SECRET=<openssl rand -hex 32>

# CORS — list the admin hostname so the admin panel can call the API
CORS_ALLOWED_ORIGINS=https://admin.spodolaks.id.lv

# Outfit generation
OPENAI_API_KEY=sk-proj-...
ANTHROPIC_API_KEY=sk-ant-api03-...

# Detection backend selector
DETECTION_BACKEND=singleitem
SINGLEITEM_BASE_URL=http://host.docker.internal:8090
SINGLEITEM_API_KEY=<one value from SID_ADMIN_API_KEYS in singleItemDetection/.env>

# (alternate: synchronous flatlay service)
# DETECTION_BACKEND=flatlay
# FLATLAY_BASE_URL=http://35.222.97.101:8001
# FLATLAY_API_KEY=...

# Legacy detection — only required if you keep the on-host service
DETECTION_API_KEY=...
DETECTION_API_BASE_URL=http://...

# Production posture
ENVIRONMENT=production
ENABLE_MOCK_LOGIN=false
```

Bring it up with the production overlay so mongo + redis stay
behind the compose network and the backend binds to loopback only:

```bash
docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build
docker compose logs -f backend | grep "detection backend"
curl -fsS http://localhost:8089/v1/health
```

**Bootstrap the first admin** (one-off):

```bash
docker compose exec backend \
  /app/bootstrap-admin -email admin@spodolaks.id.lv -password '<min-12-chars>'
```

Save the password somewhere safe — it's the only way back in.

---

## 6 · Stage C — mootd-admin (admin.spodolaks.id.lv)

The admin is a Next.js standalone build packaged as a sibling
Docker service in mootd's compose stack — no separate `docker run`
needed. The Dockerfile (`mootd-admin/apps/admin/Dockerfile`) and
the `admin:` entry in `docker-compose.production.yml` are already
in the repos; the production overlay's build context points at
the sibling repo checkout, so the layout from §3
(`/home/mootd/src/mootd/` + `/home/mootd/src/mootd-admin/`) is
what makes the relative `../mootd-admin/apps/admin/` path resolve.

Bring up the admin alongside the backend:

```bash
cd /home/mootd/src/mootd
docker compose -f docker-compose.yml -f docker-compose.production.yml \
  up -d --build admin
docker logs -f mootd-admin
curl -fsS http://127.0.0.1:3001 -o /dev/null -w "%{http_code}\n"     # 200
```

The image bakes `NEXT_PUBLIC_ADMIN_API_URL` at BUILD time (Next.js
inlines `NEXT_PUBLIC_*` values into the client bundle). The
compose `build.args` reads it from `.env` with a sensible default:

```bash
# mootd/.env
NEXT_PUBLIC_ADMIN_API_URL=https://api.spodolaks.id.lv
ADMIN_BUILD_SHA=$(git -C ../mootd-admin rev-parse --short HEAD)   # optional; shows in the admin top bar
```

Changing the API origin therefore requires a fresh build:

```bash
docker compose -f docker-compose.yml -f docker-compose.production.yml \
  up -d --build --force-recreate admin
```

Local-dev mode (`cd ~/src/mootd-admin && npm --prefix apps/admin run
dev`) is unaffected — the Dockerfile is only consulted by the
production compose overlay.

---

## 7 · Caddy — TLS + reverse proxy

Replace `/etc/caddy/Caddyfile` with the contents below (combining
the existing `api.*` block with a new `admin.*` block):

```caddy
api.spodolaks.id.lv {
    reverse_proxy 127.0.0.1:8089
    request_header X-Forwarded-For {http.request.header.CF-Connecting-IP}
    encode gzip
    log {
        output file /var/log/caddy/api.log
        format json
    }
}

admin.spodolaks.id.lv {
    reverse_proxy 127.0.0.1:3001
    request_header X-Forwarded-For {http.request.header.CF-Connecting-IP}
    encode gzip
    log {
        output file /var/log/caddy/admin.log
        format json
    }
}
```

`mootd/deploy/Caddyfile` in the repo carries the current canonical
version — copy it from there if it's drifted ahead of this doc.

Reload Caddy. It'll request fresh Let's Encrypt certs for any
hostname it doesn't already have:

```bash
sudo systemctl reload caddy
sudo journalctl -u caddy -f         # watch for "certificate obtained" lines
```

Once Caddy holds certs for both hostnames, flip Cloudflare's SSL/TLS
mode to **Full (strict)**.

---

## 8 · Verification (smoke checklist)

After every cold deploy:

```bash
# 1. Backend healthy
curl -fsS https://api.spodolaks.id.lv/v1/health
# → {"status":"ok",...}

# 2. Orchestrator reachable (admin path through the backend)
ADMTOK=$(curl -sS -X POST https://api.spodolaks.id.lv/admin/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@spodolaks.id.lv","password":"<password>"}' \
  | jq -r .accessToken)
curl -fsS https://api.spodolaks.id.lv/admin/v1/hitl-queue \
  -H "Authorization: Bearer $ADMTOK" | jq '.items | length'

# 3. Admin panel loads
curl -fsS -o /dev/null -w "%{http_code}\n" https://admin.spodolaks.id.lv
# → 200

# 4. Detection round-trip (signed-in mobile user)
TOK=$(curl -sS -X POST https://api.spodolaks.id.lv/v1/auth/google \
  -H 'Content-Type: application/json' \
  -d '{"idToken":"..."}' | jq -r .accessToken)
curl -fsS -X POST https://api.spodolaks.id.lv/v1/wardrobe/detect \
  -H "Authorization: Bearer $TOK" \
  -F "image=@sample.jpg" | jq '.items[0] | {category, label, imageUrl}'
# → image URL must look like /v1/wardrobe/items/<id>/image
```

If any step fails, see §11 troubleshooting.

---

## 9 · Mobile app (dev workstation, not the server)

The Expo / React Native app isn't deployed — operators / testers
build it on their laptops and either run it in a simulator or load
it onto a phone via Expo Go.

```bash
git clone https://github.com/spodolaks/mootd.git
cd mootd/app
cp .env.example .env
$EDITOR .env       # set EXPO_PUBLIC_API_URL=https://api.spodolaks.id.lv
npm install
npm start          # opens Metro; press i / a / w
```

Production builds go through EAS Build (`eas build --profile production`)
and ship to the App Store / Play Store separately.

---

## 10 · Day-2 operations

### Redeploy

```bash
# backend
cd /home/mootd/src/mootd && git pull
docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build backend

# orchestrator
cd /home/mootd/src/singleItemDetection && git pull
docker compose up -d --build

# admin
cd /home/mootd/src/mootd-admin && git pull
docker build -t mootd-admin:latest -f apps/admin/Dockerfile .
docker rm -f mootd-admin
docker run -d --name mootd-admin -p 127.0.0.1:3001:3001 \
  -e NEXT_PUBLIC_ADMIN_API_URL=https://api.spodolaks.id.lv \
  --restart unless-stopped mootd-admin:latest
```

### Logs

| Service | Where |
|---|---|
| mootd backend | `docker compose logs -f backend` |
| orchestrator | `cd ~/src/singleItemDetection && docker compose logs -f` |
| admin | `docker logs -f mootd-admin` |
| Caddy | `journalctl -u caddy -f` + `/var/log/caddy/*.log` |
| MongoDB | `docker compose logs -f mongo` |
| Redis | `docker compose logs -f redis` |

### Backups

`docker compose exec mongo mongodump --out /backup/$(date +%F)` to a
mounted volume. The detection-runs collection is the heaviest;
image blobs in GridFS dominate disk usage.

### Switch detection backend

```bash
# in mootd/.env
DETECTION_BACKEND=flatlay         # or singleitem (default), or legacy
docker compose -f docker-compose.yml -f docker-compose.production.yml \
  up -d --force-recreate backend
docker compose logs --tail=20 backend | grep "detection backend"
```

### Rotate a secret

1. Generate a new value (`openssl rand -hex 32`).
2. Update `mootd/.env` (or `singleItemDetection/.env`).
3. `docker compose ... up -d --force-recreate <service>`.
4. For `SINGLEITEM_API_KEY`: also add the new value to
   `singleItemDetection/.env`'s `SID_ADMIN_API_KEYS` (comma-separated
   — old + new for an overlap window) before flipping mootd, then
   remove the old one once mootd's pinned to the new.

---

## 11 · Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `admin.spodolaks.id.lv` → **525** | Caddy on the origin has no block for the hostname, can't get a cert | Add the `admin.*` Caddy block from §7, `systemctl reload caddy` |
| Backend logs `WARNING: ... falls back to legacy` | `SINGLEITEM_BASE_URL` or `FLATLAY_*` env empty when their backend is selected | Set the missing env, recreate backend |
| HITL queue → **401 from orchestrator** | `SINGLEITEM_API_KEY` in mootd's env isn't in `SID_ADMIN_API_KEYS` on the orchestrator | Sync the two; recreate mootd backend |
| Wardrobe items render as blank tiles | Detector returned a `gridfs://` URL the wardrobe handler couldn't fetch | Pre-fixed in `mootd@f12a076`; if recurring, `git log -- backend/internal/wardrobe/single_item_detector.go` to verify the blob-fetch path is in place |
| Admin login succeeds but every nav item is hidden | Login response missing `permissions[]` | Pre-fixed in `mootd@dd408c8`; verify `Login`/`Refresh` handlers include `PermissionsFor(roles)` |
| Outfit gen returns 0 outfits | User's wardrobe is missing one of {top, bottom, footwear} | Backend logs `outfit: skipping ... missing required category (top=... bottom=...)` |
| `orchestrator unreachable` | Orchestrator process died | `cd singleItemDetection && docker compose up -d` |
| Detection times out | Replicate cold-start or upstream model slow | Check `SINGLEITEM_BASE_URL`'s `/readyz`; consider switching `DETECTION_BACKEND=flatlay` as a fallback |

---

## 12 · Reference

- Backend conventions: [`backend/CLAUDE.md`](backend/CLAUDE.md)
- Mobile app conventions: [`app/CLAUDE.md`](app/CLAUDE.md)
- Backend redeploy cycle: [`deploy/README.md`](deploy/README.md)
- Local-dev guide: [`LOCAL_TESTING.md`](LOCAL_TESTING.md)
- Orchestrator architecture: [`singleItemDetection/docs/architecture.md`](https://github.com/spodolaks/singleItemDetection/blob/main/docs/architecture.md)
- Admin API spec: [`mootd-contracts/openapi/admin-api.yaml`](https://github.com/spodolaks/mootd-contracts/blob/main/openapi/admin-api.yaml)

---

**Last reviewed:** see git log for this file. The runbook is meant to
go stale; update it as the deploy shape changes.
