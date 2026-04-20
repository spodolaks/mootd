# Database migration to a new server

Step-by-step guide for copying the full MOOTD state — users, wardrobe items,
outfits, moodboards, surfaces, refresh tokens, **and all image blobs** — from
one server to another.

Everything persistent lives in the single MongoDB database `mootd`. Images
are stored inside that database as GridFS files (`fs.files` / `fs.chunks`), so
one `mongodump --archive --gzip` is a complete, self-contained migration
artifact. Redis is intentionally **not** migrated — it only holds caches,
rate-limit counters, and async outfit job state, all of which the backend
rebuilds on startup.

The [`migrate-db.sh`](migrate-db.sh) script wraps `mongodump` / `mongorestore`
with sensible defaults for the compose-managed Mongo, but works equally well
against a remote cluster (e.g. Atlas) via `MONGO_URI`.

---

## 1. Prepare the target server

Before dumping, make sure the target can actually receive the data.

1. **Install Docker + Docker Compose** (or have a Mongo cluster reachable).
2. **Clone the repo:**
   ```bash
   git clone <your-fork-url> mootd && cd mootd
   ```
3. **Create `.env`** at the repo root with at least the secrets needed by the
   backend. Treat this as the production config — do **not** reuse dev
   defaults:
   ```bash
   # Auth
   JWT_SECRET=<32+ byte random string>

   # External services
   DETECTION_API_KEY=<your key>
   OUTFIT_PROVIDER=claude          # or ollama / openai
   ANTHROPIC_API_KEY=<your key>
   ANTHROPIC_MODEL=claude-sonnet-4-5
   OPENAI_API_KEY=<your key>       # only if OUTFIT_PROVIDER=openai

   # Hardening
   ENVIRONMENT=production
   ENABLE_MOCK_LOGIN=false
   CORS_ALLOWED_ORIGINS=https://app.example.com
   ```
   > `ENVIRONMENT=production` makes the backend refuse to start with a
   > wildcard CORS list and force-disables `/v1/auth/mock-login`, regardless
   > of `ENABLE_MOCK_LOGIN`.
4. **(Recommended) change the Mongo root password** in `docker-compose.yml`
   (`MONGO_INITDB_ROOT_PASSWORD` and the matching password inside `MONGO_URI`)
   before the first `up`. Changing it after Mongo has initialized requires
   `mongosh` user-management — easier to set it right the first time.
5. **Start only the data stores first** so they're ready to receive the dump:
   ```bash
   docker compose up -d mongo redis
   docker compose ps           # both should be "healthy"
   ```
   Leave the `backend` service stopped for now — restoring into a live
   backend is fine, but starting it before restore means it will briefly run
   against an empty DB.

## 2. Dump the source database

On the **source** server, from the repo root:

```bash
scripts/migrate-db.sh dump /tmp/mootd-prod.archive.gz
```

The script auto-detects the `mootd-mongo` container and runs `mongodump`
inside it, writing the gzipped archive to the host path you passed. If the
source uses different credentials or a non-default container name, override
with environment variables:

```bash
MONGO_CONTAINER=my-mongo \
MONGO_USER=admin MONGO_PASSWORD='…' \
  scripts/migrate-db.sh dump /tmp/mootd-prod.archive.gz
```

Or dump from a remote cluster directly — no docker involved:

```bash
MONGO_URI='mongodb+srv://user:pw@cluster.mongodb.net/?authSource=admin' \
  scripts/migrate-db.sh dump /tmp/mootd-prod.archive.gz
```

Expected output:

```
wrote /tmp/mootd-prod.archive.gz (1234567 bytes)
```

Sanity-check the size — it should be roughly the total size of your images
plus a few MB of BSON. A multi-gigabyte wardrobe produces a multi-gigabyte
archive.

## 3. Copy the archive to the target

Use whatever transport is convenient; the archive is a single file:

```bash
# scp
scp /tmp/mootd-prod.archive.gz user@new-server:/tmp/

# or rsync (resumable, preferred for large dumps)
rsync -aP /tmp/mootd-prod.archive.gz user@new-server:/tmp/
```

For very large archives consider encrypting in transit with a tool you
already trust (the archive contains user data and refresh tokens). `scp` over
SSH is sufficient for most deployments.

## 4. Restore on the target

SSH into the target server and run:

```bash
cd /path/to/mootd
scripts/migrate-db.sh restore /tmp/mootd-prod.archive.gz
```

The script will:
1. Prompt for confirmation (the `mootd` DB on the target is dropped +
   replaced).
2. Stream the archive into `mongorestore --drop --nsInclude=mootd.*` so only
   the `mootd` DB is touched — safe on a shared cluster.

Skip the prompt for automated runs:

```bash
ASSUME_YES=1 scripts/migrate-db.sh restore /tmp/mootd-prod.archive.gz
```

Restore into a remote cluster (Atlas, another VPS, etc.):

```bash
MONGO_URI='mongodb+srv://user:pw@cluster.mongodb.net/?authSource=admin' \
  scripts/migrate-db.sh restore /tmp/mootd-prod.archive.gz
```

## 5. Start the backend

```bash
docker compose up -d backend
docker compose logs -f backend      # watch for "listening on :8080"
curl -fsS http://127.0.0.1:8089/healthz
curl -fsS http://127.0.0.1:8089/readyz
```

`/readyz` returns 200 only when Mongo is reachable, so a green readiness
probe confirms the backend can talk to the restored DB.

## 6. Verify

A quick smoke test covers the full stack — auth, DB reads, and GridFS image
serving — without needing the app:

```bash
# Adjust host/port for your target
API=http://127.0.0.1:8089

# 1. Pick any user from the restored DB
docker exec -it mootd-mongo mongosh \
  -u mootd -p mootd_dev --authenticationDatabase admin \
  --eval 'db.getSiblingDB("mootd").users.find({}, {_id:1, email:1}).limit(3)'

# 2. Count wardrobe items and GridFS files to confirm counts match the source
docker exec -it mootd-mongo mongosh \
  -u mootd -p mootd_dev --authenticationDatabase admin \
  --eval '
    const db = db.getSiblingDB("mootd");
    print("users:",      db.users.countDocuments());
    print("items:",      db.wardrobe_items.countDocuments());
    print("moodboards:", db.moodboards.countDocuments());
    print("gridfs:",     db["fs.files"].countDocuments());
  '

# 3. Fetch one wardrobe image by ID to confirm GridFS bytes survived
# (replace <itemId> with an ID from step 2)
curl -fsS -o /tmp/check.jpg "$API/v1/wardrobe/items/<itemId>/image" \
  -H "Authorization: Bearer <token>"
file /tmp/check.jpg    # should say "JPEG image data" / "PNG image data"
```

Counts should match the source server. If any collection is empty that
shouldn't be, the restore was partial — re-run it, and check the source
archive with `gunzip -t /tmp/mootd-prod.archive.gz` to confirm it isn't
truncated.

## 7. Point the frontend at the new server

Update `app/.env` (or the build-time config for shipped apps):

```bash
EXPO_PUBLIC_API_URL=https://api.new-server.example.com
EXPO_PUBLIC_DATA_SOURCE=api
```

Then rebuild the client as usual.

---

## Troubleshooting

**`mongorestore` fails with `E11000 duplicate key`**
The target DB wasn't empty and you skipped `--drop`. The script always passes
`--drop`; if you ran `mongorestore` manually, add it or drop the `mootd` DB
first (`docker exec mootd-mongo mongosh --eval 'db.getSiblingDB("mootd").dropDatabase()'`).

**`authentication failed`**
The script builds credentials from `MONGO_USER` / `MONGO_PASSWORD` (defaults
`mootd` / `mootd_dev`). Override them, or pass a full `MONGO_URI` that
already encodes the credentials. Remember the target Mongo's root password
may differ from the source.

**`mongodump: command not found` on the host**
Either install the MongoDB Database Tools (`brew install mongodb-database-tools`
/ `apt-get install mongodb-database-tools`), or force the script to exec
inside the container with `USE_DOCKER=1`.

**Users report being logged out after migration**
Expected if `JWT_SECRET` changed — old access tokens no longer validate.
Refresh tokens are stored in Mongo and survive the move, so the frontend's
silent-refresh interceptor recovers on the next request. If you kept
`JWT_SECRET` unchanged, nothing should break.

**Outfit-generation cache misses spike right after migration**
Expected — the Redis cache is empty on the new server. It repopulates as
users generate outfits.
