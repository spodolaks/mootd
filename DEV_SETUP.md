# MOOTD — New Laptop Dev Setup

Step-by-step guide for setting up the MOOTD app in developer mode on a new machine, connecting to the shared remote backend at **spodolaks.id.lv**.

> **What this covers:** running the React Native / Expo frontend locally, pointed at the shared remote backend. You do **not** need Docker or Go for this path.

---

## Prerequisites

Install these before you start:

| Tool | Version | Install |
|------|---------|---------|
| Git | any | pre-installed on macOS; `sudo apt install git` on Ubuntu |
| Node.js | **20+** | https://nodejs.org/en/download — use the LTS installer, or `nvm install 20` |
| npm | 10+ | comes with Node |
| Expo Go (optional) | latest | iOS App Store / Google Play — only if you want to test on a physical phone |

Verify your Node version before continuing:

```bash
node -v   # must print v20.x.x or higher
npm -v    # must print 10.x.x or higher
```

---

## Step 1 — Get access to the repo

You need a GitHub account that has been granted read access to `spodolaks/mootd`.

If you don't have the GitHub CLI (`gh`) installed:

```bash
# macOS
brew install gh

# Ubuntu / Debian
sudo apt install gh
```

Authenticate:

```bash
gh auth login
# choose: GitHub.com → HTTPS → authenticate via browser
```

---

## Step 2 — Clone the repository

```bash
# Create the projects directory if it doesn't exist
mkdir -p ~/Projects2026
cd ~/Projects2026

# Clone
gh repo clone spodolaks/mootd
# OR with plain git:
# git clone https://github.com/spodolaks/mootd.git

cd mootd
```

---

## Step 3 — Install frontend dependencies

```bash
cd ~/Projects2026/mootd/app
npm install
```

This takes 1–2 minutes on first run. You should see no errors — warnings about peer dependencies are fine.

---

## Step 4 — Configure the environment

Create a local env file that points the app at the shared remote backend:

```bash
cat > ~/Projects2026/mootd/app/.env.local <<'EOF'
EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089
EXPO_PUBLIC_DATA_SOURCE=api
EOF
```

Verify it looks right:

```bash
cat ~/Projects2026/mootd/app/.env.local
# EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089
# EXPO_PUBLIC_DATA_SOURCE=api
```

> **Why `.env.local` and not `.env`?**  
> `.env` is committed to the repo and points at `127.0.0.1` (local backend). `.env.local` is gitignored and overrides it for your machine. Never edit `.env` directly.

---

## Step 5 — Verify the backend is reachable

```bash
curl -sS http://spodolaks.id.lv:8089/healthz
# Expected: {"service":"mootd-backend","status":"ok", ...}
```

If you get `Connection refused` or a timeout, the remote backend may be down — ping the repo owner.

---

## Step 6 — Start the app

### Web (simplest, no phone needed)

```bash
cd ~/Projects2026/mootd/app
npx expo start --web --clear
```

Open **http://localhost:8081** in your browser.

### Mobile — Expo Go on a physical device

Your phone and laptop must be on **the same Wi-Fi network**.

```bash
cd ~/Projects2026/mootd/app

# Find your laptop's local IP first
ipconfig getifaddr en0        # macOS (Wi-Fi)
# OR
hostname -I | awk '{print $1}' # Linux

# Then start Expo with that IP as the API URL
EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089 npx expo start --clear
```

Scan the QR code with Expo Go (Android) or the Camera app (iOS).  
The app will connect to the remote backend over the internet — your phone's Wi-Fi doesn't matter for the API, only for the Metro bundler connection.

### Mobile — iOS / Android simulator

```bash
cd ~/Projects2026/mootd/app
npx expo start --clear
# Press 'i' for iOS simulator, 'a' for Android emulator
```

Requires Xcode (iOS) or Android Studio (Android) installed and configured.

---

## Step 7 — Sign in

The remote backend runs with `ENABLE_MOCK_LOGIN=true` in dev mode.

1. Open http://localhost:8081 (or the app on your device).
2. Tap **Sign in** → select the mock/dev login option.
3. You're signed in as `dev.user@mootd.local` — no password needed.

> **Google sign-in** requires OAuth credentials configured for your hostname and is only fully functional in the production environment.

---

## Daily workflow

```bash
# Start the app (web)
cd ~/Projects2026/mootd/app
npx expo start --web --clear

# Stop: Ctrl-C in the terminal
```

The remote backend at `spodolaks.id.lv` runs continuously — you don't need to start or stop it.

---

## Switching between remote and local backend

If you later set up the full local backend (see `LOCAL_TESTING.md`), update `.env.local`:

```bash
# Remote (default for new laptops)
echo 'EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089' > ~/Projects2026/mootd/app/.env.local

# Local backend (after running docker compose up --build)
echo 'EXPO_PUBLIC_API_URL=http://127.0.0.1:8089' > ~/Projects2026/mootd/app/.env.local
```

Always restart Metro with `--clear` after changing `.env.local`.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `node -v` shows v18 or lower | Install Node 20 LTS: `nvm install 20 && nvm use 20` |
| `npm install` fails with EACCES | Don't use `sudo npm install`. Fix npm permissions: https://docs.npmjs.com/resolving-eacces-permissions-errors-when-installing-packages-globally |
| `expo: command not found` | Use `npx expo start` instead of `expo start` (local install, not global) |
| App shows "Network request failed" on every call | Check `.env.local` has the right URL. Run `curl http://spodolaks.id.lv:8089/healthz` to verify backend is up |
| Port 8081 already in use | `lsof -ti:8081 \| xargs kill -9`, then restart `npx expo start --web` |
| Metro bundler stuck / stale cache | `npx expo start --web --clear` (the `--clear` flag wipes the Metro cache) |
| QR code scan opens browser, not Expo Go | Make sure Expo Go app is installed. On iOS: use the Camera app, not QR scanner in Safari |
| `gh repo clone` fails with "Repository not found" | You haven't been granted repo access. Ask @spodolaks to add you as a collaborator |
| Changes to `.env.local` not picked up | Restart Metro with `--clear`: `Ctrl-C` then `npx expo start --web --clear` |

---

## What you're connected to

The shared dev server at `spodolaks.id.lv:8089` runs:

| Service | Details |
|---------|---------|
| Go REST API | All `/v1/*` and `/admin/v1/*` endpoints |
| MongoDB 8.0 | Shared dev data — wardrobe items, users, outfits |
| Redis 7 | Outfit job cache, rate limiting |

> **Heads up:** this is a shared dev database. Data you create (wardrobe items, outfits) is visible to everyone using the remote backend. For isolated testing, set up a local backend with `docker compose up --build` (see `LOCAL_TESTING.md`).

---

## Full local backend (optional)

If you need a fully isolated environment or want to work on backend code, follow `LOCAL_TESTING.md` in the repo root. You'll need Docker Desktop and (optionally) Go 1.24+.
