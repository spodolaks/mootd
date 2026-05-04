# Redis posture (mootd#55)

The backend uses Redis for three jobs:

| Surface | What's there | Fallback when Redis is down |
|---|---|---|
| Outfit cache | `outfit:cache:*` keys (60s TTL) keyed by wardrobe + weather hash | Mongo `outfit_cache` collection (slower) |
| Per-user rate limit | `ratelimit:user:*` sliding-window counters | In-memory token bucket — **per-instance only** |
| Async outfit jobs | `outfit:job:*` (write-through) | Mongo `outfit_jobs` (always source-of-truth; Redis is the cache) |
| Idempotency keys | `outfit:idem:*` (60s TTL) | None — duplicate generations would re-bill (mootd#42) |

Every fallback "works" in the sense that requests still complete, but **two of them silently change behaviour**:

- **Rate limit fallback** drops from cluster-wide to per-instance. A user being limited on instance A can hit instance B with a fresh budget. In production this is a real abuse vector.
- **Idempotency fallback** disappears entirely — the second click pays the second LLM bill.

## Posture

**Production**: Redis is **required**. The backend treats Redis as a hard dependency:

- `/readyz` returns **503** with `{"reason":"redis_unreachable"}` when Redis is down.
- The reverse proxy (Caddy) depools any instance whose `/readyz` is failing.
- `mootd_redis_up` Prometheus gauge alerts ops within seconds.

**Non-production** (`ENVIRONMENT≠production`): Redis is optional. `/readyz` reports the current state in the body but doesn't fail readiness. This keeps `docker compose up` working when a developer disables Redis for some reason (rare, but allowed).

## Knobs

| Env var | Default | Meaning |
|---|---|---|
| `ENVIRONMENT` | `development` | Setting to `production` flips Redis to required. |
| `REDIS_URL` | `redis://redis:6379` | Connection string. Empty/invalid → backend boots without Redis (still required-fails in production). |

## Verifying

```bash
# /readyz reflects Redis state
curl -sS http://127.0.0.1:8089/readyz | jq
# {"status":"ready","mongo":"mootd","redis":"up"}

# Stop Redis, retry
docker compose stop redis
curl -sS -w "%{http_code}\n" http://127.0.0.1:8089/readyz
# 503 in production, 200 elsewhere — body shows redis="down"

# Prometheus gauge
curl -sS http://127.0.0.1:8089/metrics | grep mootd_redis_up
# mootd_redis_up 0
```
