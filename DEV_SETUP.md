# MOOTD — New Laptop Dev Setup

Step-by-step guide for setting up the MOOTD app in developer mode on a new machine, connecting to the shared remote backend at **spodolaks.id.lv**.

> **What this covers:** running the React Native / Expo frontend locally on your iPhone and in the browser, pointed at the shared remote backend. You do **not** need Docker or Go for this path.

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| macOS | 13 Ventura+ | required for Xcode 15/16 |
| Xcode | **15 or 16** | Mac App Store (12 GB download — start this first) |
| Node.js | **20+** | https://nodejs.org/en/download — LTS installer, or `nvm install 20` |
| npm | 10+ | comes with Node |
| Git | any | pre-installed on macOS |
| Apple ID | any (free) | for signing the app onto your iPhone |

Verify Node before continuing:

```bash
node -v   # must print v20.x.x or higher
npm -v    # must print 10.x.x or higher
```

> **Note:** The app uses `expo-dev-client` and native modules (`expo-image-picker`, `react-native-reanimated`, etc.) that are not supported by the standard Expo Go app. You need a Xcode build to run it on iPhone.

---

## Step 1 — Install Xcode

1. Open the **Mac App Store** → search for **Xcode** → Install (it is ~12 GB).
2. Once installed, open Xcode once to accept the license and let it install additional components. This takes several minutes.
3. Open **Terminal** and run:

```bash
# Install command-line developer tools
xcode-select --install

# Accept the Xcode licence from the command line (if prompted)
sudo xcodebuild -license accept

# Verify the active Xcode path
xcode-select -p
# Should print: /Applications/Xcode.app/Contents/Developer
```

If `xcode-select -p` shows a different path (e.g. a beta Xcode), fix it:

```bash
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
```

---

## Step 2 — Add your Apple ID to Xcode

You need a free Apple ID (you don't need a paid Apple Developer account) to install the app on your own iPhone.

1. Open **Xcode** → **Settings** (⌘,) → **Accounts** tab.
2. Click **+** in the bottom-left → **Apple ID** → sign in with your Apple ID.
3. Your account appears with the role **Personal Team** (free). That's enough.

---

## Step 3 — Prepare your iPhone

### Enable Developer Mode (required on iOS 16+)

1. Connect your iPhone to your Mac with a USB cable.
2. On the iPhone: **Settings → Privacy & Security → Developer Mode** → toggle **On** → the phone will restart.
3. After restart, confirm enabling Developer Mode when prompted.

### Trust this Mac

1. Unlock your iPhone.
2. A dialog appears: **"Trust This Computer?"** → tap **Trust** → enter your passcode.

If the dialog doesn't appear, unplug and replug the USB cable with the phone unlocked.

---

## Step 4 — Get access to the repo

You need a GitHub account with read access to `spodolaks/mootd`. Ask @spodolaks to add you as a collaborator first.

Install the GitHub CLI if you don't have it:

```bash
brew install gh   # requires Homebrew — https://brew.sh if not installed
```

Authenticate:

```bash
gh auth login
# choose: GitHub.com → HTTPS → authenticate via browser
```

---

## Step 5 — Clone the repository

```bash
mkdir -p ~/Projects2026
cd ~/Projects2026
gh repo clone spodolaks/mootd
cd mootd
```

---

## Step 6 — Install frontend dependencies

```bash
cd ~/Projects2026/mootd/app
npm install
```

Takes 1–2 minutes. Warnings about peer deps are fine; errors are not.

---

## Step 7 — Configure the environment

```bash
cat > ~/Projects2026/mootd/app/.env.local <<'EOF'
EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089
EXPO_PUBLIC_DATA_SOURCE=api
EOF
```

Verify:

```bash
cat ~/Projects2026/mootd/app/.env.local
# EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089
# EXPO_PUBLIC_DATA_SOURCE=api
```

> **Why `.env.local`?** `.env` is committed to the repo and points at a local backend. `.env.local` is gitignored and overrides it. Never edit `.env` directly.

---

## Step 8 — Verify the backend is reachable

```bash
curl -sS http://spodolaks.id.lv:8089/healthz
# Expected: {"service":"mootd-backend","status":"ok", ...}
```

If this times out, the remote backend is down — ping @spodolaks.

---

## Step 9 — Build and install on iPhone

This first build takes **10–20 minutes** — it compiles the entire native iOS project. Subsequent builds are faster (2–5 min).

```bash
cd ~/Projects2026/mootd/app
npx expo run:ios --device
```

What happens:
1. **Prebuild** — Expo generates the native `ios/` Xcode project from your `app.json` config.
2. **Signing** — Expo CLI asks you to select your phone from a list and automatically creates a free provisioning profile using your Apple ID from Xcode.
3. **Build** — Xcode compiles the app.
4. **Install** — the app is pushed to your iPhone and launches automatically.

### If Expo asks you to pick a signing team

Select your **Personal Team** (the one associated with your Apple ID, not a paid org).

### If Xcode can't find your device

Make sure the iPhone is:
- Unlocked
- Connected via USB
- Trusted (Step 3)

Then check Xcode → **Window → Devices and Simulators** — your phone should appear. If it shows "Unavailable", wait for the symbol files to download (first connection, takes a few minutes).

### If "Untrusted Developer" appears on the phone

This happens the first time with a free Apple ID:

1. On iPhone: **Settings → General → VPN & Device Management**
2. Tap your Apple ID under **Developer App**
3. Tap **Trust "your@email.com"** → **Trust**
4. Relaunch the MOOTD app.

> **Free Apple ID caveat:** the provisioning profile expires every **7 days**. When the app stops launching, re-run `npx expo run:ios --device` (it rebuilds and re-signs automatically).

---

## Step 10 — Sign in

The remote backend runs with mock login enabled for development.

1. Open the MOOTD app on your iPhone.
2. Tap **Sign in** → choose the **Dev / Mock login** option.
3. You're signed in as `dev.user@mootd.local` — no password needed.

> Google sign-in requires OAuth credentials bound to a specific hostname and is only fully functional in the production environment.

---

## Daily workflow

After the first build, day-to-day use is fast:

```bash
cd ~/Projects2026/mootd/app

# Start Metro bundler (JS changes update instantly via hot reload)
npx expo start --clear

# If you need to rebuild native code (after adding a new native package):
npx expo run:ios --device
```

The app on your iPhone talks to `spodolaks.id.lv` for all API calls — your laptop just serves the JavaScript bundle. If you close Metro, the app still works for previously-loaded screens but won't pick up JS changes.

The remote backend runs continuously — you don't need to start or stop anything on the server.

---

## Running in the iOS Simulator (no iPhone needed)

If you just want to test in a simulator on your Mac:

```bash
cd ~/Projects2026/mootd/app
npx expo run:ios
# Builds and opens in the default iOS Simulator (no device needed, no signing)
```

Or after a build exists, start Metro and press `i`:

```bash
npx expo start --clear
# press 'i' to open iOS Simulator
```

---

## Running in the browser

If you want to skip the iOS build entirely and just test in Chrome/Safari:

```bash
cd ~/Projects2026/mootd/app
npx expo start --web --clear
```

Open **http://localhost:8081**. Some native features (camera, haptics) won't work in the browser.

---

## Switching between remote and local backend

```bash
# Remote backend (default — always on)
echo 'EXPO_PUBLIC_API_URL=http://spodolaks.id.lv:8089' > ~/Projects2026/mootd/app/.env.local

# Local backend (after running docker compose up --build — see LOCAL_TESTING.md)
echo 'EXPO_PUBLIC_API_URL=http://127.0.0.1:8089' > ~/Projects2026/mootd/app/.env.local
```

Restart Metro with `--clear` after any `.env.local` change. For device builds you'll also need to rerun `npx expo run:ios --device` so the new URL is baked into the native binary.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `xcode-select --install` says "already installed" | Run `sudo xcode-select -s /Applications/Xcode.app/Contents/Developer` to ensure the right Xcode is active |
| `npx expo run:ios` errors: "No Xcode installation found" | Open Xcode app at least once to finish the install, then retry |
| Build fails: "unable to find a destination matching the provided destination specifier" | Your iPhone isn't trusted or Developer Mode is off — follow Step 3 |
| Build fails: "Signing for 'mootd' requires a development team" | Open the generated `ios/mootd.xcworkspace` in Xcode, select the target, set your team under **Signing & Capabilities**, then re-run |
| "Untrusted Developer" on iPhone | Settings → General → VPN & Device Management → tap your Apple ID → Trust |
| App crashes immediately after install | The provisioning profile expired (7-day free limit). Re-run `npx expo run:ios --device` |
| `npm install` fails with EACCES | Don't use `sudo npm install`. Fix npm permissions: https://docs.npmjs.com/resolving-eacces-permissions-errors-when-installing-packages-globally |
| `node -v` shows v18 or lower | `nvm install 20 && nvm use 20` |
| App shows "Network request failed" on every API call | Check `.env.local`: `cat ~/Projects2026/mootd/app/.env.local`. Then `curl http://spodolaks.id.lv:8089/healthz` |
| Metro bundler stuck / stale cache | `Ctrl-C` then `npx expo start --clear` |
| Port 8081 already in use | `lsof -ti:8081 \| xargs kill -9`, then restart |
| `gh repo clone` fails "Repository not found" | Ask @spodolaks to grant collaborator access to your GitHub account |
| Changes to `.env.local` not picked up | Restart Metro with `--clear`, and if on device, rerun `npx expo run:ios --device` |

---

## What you're connected to

The shared dev server at `spodolaks.id.lv:8089` runs:

| Service | Details |
|---------|---------|
| Go REST API | All `/v1/*` and `/admin/v1/*` endpoints |
| MongoDB 8.0 | Shared dev data — wardrobe items, users, outfits |
| Redis 7 | Outfit job cache, rate limiting |

> **Heads up:** this is a shared dev database. Data you create (wardrobe items, outfits) is visible to everyone on the remote backend. For isolated testing, set up a local backend with `docker compose up --build` — see `LOCAL_TESTING.md`.

---

## Full local backend (optional)

If you need a fully isolated environment or want to work on backend code, follow `LOCAL_TESTING.md` in the repo root. You'll need Docker Desktop and (optionally) Go 1.24+.
