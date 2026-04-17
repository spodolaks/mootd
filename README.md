# MOOTD

AI wardrobe app: photo → clothing detection → outfit recommendations.

## Monorepo

```
mootd/
├── app/            React Native + Expo (SDK 54 / RN 0.83)
├── backend/        Go 1.24 REST API (net/http stdlib)
├── docker-compose.yml          — MongoDB + Redis + backend (dev)
├── docker-compose.dev.yml      — dev-server overlay (pre-built image)
└── .github/workflows/          — CI + image publish + deploy
```

See [app/CLAUDE.md](app/CLAUDE.md) and [backend/CLAUDE.md](backend/CLAUDE.md) for per-component architecture.

## Local development

```bash
# Backend + infra
docker compose up --build

# Frontend
cd app && npm install && npm start
```

Backend → `http://127.0.0.1:8081`. Seed surface textures once after `compose up`:

```bash
cd backend && go run ./cmd/seed-surfaces \
  -mongo "mongodb://mootd:mootd_dev@localhost:27018/?authSource=admin" -db mootd
```

## CI

`.github/workflows/ci.yml` runs on every push and PR to `main`:

| Job | Runs |
|---|---|
| Backend (Go) | `go vet`, `golangci-lint`, `go test -race`, `go build`, `docker build` |
| Frontend (RN) | `npm ci`, `tsc --noEmit`, `npm run lint`, `npm test` |

## Dev server deploy

### One-time server setup

On a Linux VPS (anything with Docker and docker compose):

```bash
# 1. Clone the repo
sudo mkdir -p /srv/mootd && sudo chown $USER /srv/mootd
git clone https://github.com/<owner>/<repo>.git /srv/mootd
cd /srv/mootd

# 2. Create .env with production secrets (NOT in git)
cp .env.example .env
vi .env   # fill in DETECTION_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY, JWT_SECRET, etc.

# 3. Pull the published backend image (GHCR)
#    If the package is private, authenticate first:
#    echo $GHCR_PAT | docker login ghcr.io -u <github-user> --password-stdin
export BACKEND_IMAGE=ghcr.io/<owner>/<repo>-backend:latest

# 4. Bring up the stack
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d

# 5. Seed surface textures once
docker compose exec backend /app/mootd-api   # (or mount the seed binary; see below)
```

### Ongoing deploys

Push to `main` → GitHub Actions:

1. `publish-backend.yml` builds the backend image and pushes `ghcr.io/<owner>/<repo>-backend:<sha>` + `:latest`.
2. Trigger `Deploy to dev` manually (`Actions → Deploy to dev → Run workflow`). It SSHes into the dev server, `git pull`s, then `docker compose up -d` with the new image.

Switching to auto-deploy: uncomment the `push: branches: [main]` trigger in [.github/workflows/deploy-dev.yml](.github/workflows/deploy-dev.yml).

### Required GitHub secrets

`Settings → Secrets and variables → Actions → New repository secret`:

| Secret | Value |
|---|---|
| `DEV_SSH_HOST` | Dev server hostname or IP |
| `DEV_SSH_USER` | Deploy user (e.g. `deploy`) |
| `DEV_SSH_KEY` | Private SSH key (matching an `authorized_keys` entry) |
| `DEV_SSH_PORT` | Optional; defaults to `22` |

## Production hardening (later)

Before going past dev:

- Parameterise `MONGO_INITDB_ROOT_PASSWORD` in [docker-compose.yml](docker-compose.yml) — don't keep `mootd_dev`.
- Set `JWT_SECRET` to a strong value in `.env`; the backend refuses to start in `ENVIRONMENT=production` without one.
- Swap the exposed ports (`27018`, `6379`, `8081`) for loopback-only binds and put the backend behind a reverse proxy with TLS.
- Rotate any keys present in your local `.env` before sharing the repo.
