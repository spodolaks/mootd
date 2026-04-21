# Production deployment

MOOTD backend runs as a Docker Compose stack behind Caddy, with Cloudflare
in front for TLS / DDoS / caching. This doc is the one-time setup + the
repeatable deploy.

```
Browser → Cloudflare (edge TLS) → Caddy :443 (origin TLS) → backend :8089
                                             ↓
                                          mongo + redis (compose network only)
```

## One-time bootstrap

### 1. Cloudflare DNS

In the dashboard for the zone:

| Type | Name | Content | Proxy |
|------|------|---------|-------|
| A | `api` | server IP | 🟠 Proxied |
| A | `@`   | server IP | 🟠 Proxied |

SSL/TLS → **Full (strict)**. (Use "Full" non-strict only until Caddy has
obtained its first cert, then switch.)

### 2. Server setup (Ubuntu 22.04 / 24.04)

SSH in as root and install Docker + Caddy:

```bash
# Docker
apt-get update
apt-get install -y ca-certificates curl
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
```

### 3. Firewall

Only 22 / 80 / 443 should be open. The backend binds to `127.0.0.1:8089`
(see `docker-compose.production.yml`) so it's unreachable without Caddy.

```bash
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
```

Optional hardening: restrict `80` / `443` to Cloudflare's [published IP
ranges](https://www.cloudflare.com/ips/) so your origin only accepts
traffic that actually came through CF.

### 4. Caddy config

Copy `deploy/Caddyfile` from this repo to `/etc/caddy/Caddyfile` on the
server (the deploy script does this for you on the first run via rsync,
but the file is under `deploy/` and not under `/etc`, so you must move
it by hand once):

```bash
cp /opt/mootd/deploy/Caddyfile /etc/caddy/Caddyfile
systemctl reload caddy
journalctl -u caddy -f   # watch for "certificate obtained successfully"
```

If you deploy to a different hostname, edit the Caddyfile — the only
customisation is the `api.spodolaks.id.lv` block header.

### 5. Secrets — `/opt/mootd/.env`

Never commit this file. Create it once on the server:

```bash
cd /opt/mootd
cat > .env <<EOF
ENVIRONMENT=production
JWT_SECRET=$(openssl rand -hex 32)
ENABLE_MOCK_LOGIN=false
CORS_ALLOWED_ORIGINS=https://spodolaks.id.lv,https://api.spodolaks.id.lv
DETECTION_API_KEY=<your key>
OUTFIT_PROVIDER=openai
OPENAI_API_KEY=<your key>
# Optional, for Claude generator: ANTHROPIC_API_KEY, ANTHROPIC_MODEL
EOF
chmod 600 .env
```

The compose overlay will refuse to start if `JWT_SECRET` or
`CORS_ALLOWED_ORIGINS` are missing — intentional, so you can't ship a
misconfigured box.

### 6. First boot

From your laptop:

```bash
MOOTD_HOST=root@37.27.35.215 ./scripts/deploy.sh
```

The script rsyncs the repo, runs `docker compose up -d --build` with
both compose files, and prints the backend's healthcheck. First build
takes 2–3 minutes; subsequent ones are near-instant if backend/ is
unchanged.

## Deploys after bootstrap

Same command:

```bash
MOOTD_HOST=root@37.27.35.215 ./scripts/deploy.sh
```

rsync ships only the changed files; Docker rebuilds only the backend
image when source under `backend/` changed.

Roll back by deploying an older commit:

```bash
git checkout <sha> && MOOTD_HOST=… ./scripts/deploy.sh
```

## What's _not_ here

**Frontend**: the web build is a separate concern. When you want to host
the Expo web bundle at `https://spodolaks.id.lv`, the plan is
`expo export --platform web` → static files → Cloudflare Pages or a
second Caddy block. Track that as its own task.

**CI-driven deploy**: the script works off your laptop. When you want
to trigger deploys from GitHub Actions on merge to `main`, add a
workflow that loads `MOOTD_HOST` and an SSH key from repo secrets and
runs the same script.

**Backups**: not automated yet. Dump MongoDB + GridFS to object storage
on a schedule before you have real users.
