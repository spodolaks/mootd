# Secret rotation schedule

> Cadence: every 90 days, calendar-driven.
> Owner: on-call admin operator.

This document lives at `mootd/docs/RUNBOOKS/secret-rotation-schedule.md`.
Read alongside the [credential-compromise runbook](./incident-admin-credential-compromise.md)
which covers off-schedule rotations.

---

## What we rotate, and why

| Secret | Cadence | Blast radius if leaked | Notes |
|---|---|---|---|
| `JWT_SECRET` | 90 days | Every user's session — leak = anyone can mint a user JWT and call `/v1/*` as any user | HS256 signing key |
| `ADMIN_JWT_SECRET` | 90 days | Every admin's session — leak = anyone can mint an admin JWT, bypass MFA gate | Must differ from `JWT_SECRET`; backend refuses to start otherwise |
| MongoDB root password (`MONGO_INITDB_ROOT_PASSWORD`) | 90 days | Full DB read/write | Coordinated with `MONGO_URI` rotation |
| `MONGO_URI` | with the above | Full DB read/write | Connection string includes the password |
| `ANTHROPIC_API_KEY` | 90 days | Pay for someone else's LLM calls | Visible in Anthropic console; rotate via dashboard, then update env |
| `OPENAI_API_KEY` | 90 days | Pay for someone else's LLM calls | Same — rotate via OpenAI dashboard |
| `DETECTION_API_KEY` | 90 days | Free use of the detection service | External vendor; coordinate with them |
| `SMTP_PASSWORD` | 180 days | Send mail as us; phishing risk | Lower cadence — leak surface smaller, less automated |
| Recovery codes per admin | When consumed | One-time bypass of MFA | Auto-rotated by the login handler when consumed |

**Not rotated** (intentional):
- TOTP secrets per admin — rotating these breaks every authenticator
  app. Replace only when an admin re-enrolls.
- Per-user budget caps — config, not a secret.
- Per-user routing config — config, not a secret.

---

## Calendar

Add an event to the operator calendar **every 89 days** with a
12-hour reminder. 89 not 90 so we never run a day past the
intended cadence — there's a known operational pain in the
"rotation overdue, in a meeting, will do it tonight" pattern.

Suggested cadence (adjust the start date to your team's
on-call rotation):

| Cycle | Start | Secrets |
|---|---|---|
| Q1 | 2026-01-06 | JWT_SECRET, ADMIN_JWT_SECRET, MongoDB root, MONGO_URI |
| Q2 | 2026-04-07 | ANTHROPIC_API_KEY, OPENAI_API_KEY, DETECTION_API_KEY |
| Q3 | 2026-07-07 | JWT_SECRET, ADMIN_JWT_SECRET, MongoDB root, MONGO_URI |
| Q4 | 2026-10-06 | ANTHROPIC_API_KEY, OPENAI_API_KEY, DETECTION_API_KEY, SMTP_PASSWORD |

Alternating quarters means we rotate auth tokens and external
keys at different cadences — minimises the "everything moves
on the same day" risk where a botched rotation takes
everything down at once.

---

## Procedures

### JWT_SECRET / ADMIN_JWT_SECRET

```bash
NEW=$(openssl rand -hex 32)

# 1. Update in deployment env (DO NOT inline in repo).
#    e.g. fly secrets set ADMIN_JWT_SECRET="$NEW"
#    or kubectl edit secret … with the new value.

# 2. Restart the backend.
docker compose restart backend  # or your deploy mechanism

# Effect: every active session is invalidated. Users + admins
# see a 401 on their next request and re-login. The 401-refresh
# interceptor in the FE handles this gracefully — refresh fails,
# session cleared, redirect to /login.
```

If you control whether downtime is acceptable: do this during
low-usage hours. The admin team feels it; the end-user team
sees a one-tap re-auth on the mobile app.

### MongoDB root password + MONGO_URI

These rotate together because the URI embeds the password.

```bash
# 1. Connect with the OLD credentials.
docker compose exec mongo mongosh --quiet --username mootd --password mootd_dev --authenticationDatabase admin admin <<EOF
db.changeUserPassword("mootd", "<NEW STRONG PASSWORD>");
EOF

# 2. Update both env vars in the deployment:
#    MONGO_INITDB_ROOT_PASSWORD=<NEW>
#    MONGO_URI=mongodb://mootd:<NEW>@mongo:27017/?authSource=admin

# 3. Restart the backend (NOT the mongo container — that would
#    re-read MONGO_INITDB_ROOT_PASSWORD as the bootstrap
#    password and try to create a new root user).
docker compose restart backend

# 4. Verify with a healthcheck.
curl -s http://127.0.0.1:8089/healthz
```

### ANTHROPIC_API_KEY / OPENAI_API_KEY

External vendor flow:

1. In the vendor dashboard, generate a new key.
2. Update env (`ANTHROPIC_API_KEY` / `OPENAI_API_KEY`) in
   deployment.
3. Restart the backend.
4. Smoke-test by triggering an outfit generation
   (POST /v1/outfits/generate) and checking llm_calls for a
   successful row with the new key implicitly working.
5. **Then** delete the old key in the vendor dashboard. The
   restart-then-delete order means a botched env update doesn't
   leave the service unable to call the LLM.

### DETECTION_API_KEY

Same pattern as the LLM keys. Coordinate with the detection
service operator (who controls key issuance on their side).

### SMTP_PASSWORD

If you're using Gmail with an app-password, rotate via
[Google Account → Security → App passwords](https://myaccount.google.com/apppasswords).
Then update `SMTP_PASSWORD` and restart the backend; the next
weekly cron fire validates that the new password works.

If the rotation breaks the cron, the failure mode is "no
weekly report email" — operators notice the gap on the next
Monday and re-rotate. Acceptable because the report endpoint
is also reachable manually via /admin/v1/reports/weekly/send.

---

## Yearly fire drill

Every year, in the calendar slot we'd otherwise rotate
JWT_SECRET, run the **full credential-compromise runbook**
end-to-end on a non-production admin (one we've created
specifically for this drill).

The drill catches:

- Stale shell snippets in
  [the runbook](./incident-admin-credential-compromise.md) that no
  longer work because the schema changed.
- New secrets we forgot to add to the rotation list.
- Operator ergonomic issues (e.g. "the bootstrap-admin tool
  doesn't have --force documented anywhere").

The drill **must include** the user-notification step — write
a real email to a test address and verify it arrives. The
notification path is the most likely to bit-rot because we'd
otherwise never exercise it.

After the drill: open issues for everything that surprised
you. Treat surprise as a defect.

---

## Caddy IP allowlist (P5-03 / mootd-admin#36)

Not a secret per se but adjacent: production deployments should
duplicate the `ADMIN_ALLOWED_IPS` enforcement at the Caddy
reverse-proxy layer. The in-binary middleware is
defense-in-depth.

```caddy
admin.spodolaks.id.lv {
    @blocked not remote_ip 203.0.113.0/24 198.51.100.42 ::1/128
    respond @blocked 403

    reverse_proxy backend:8080
}
```

Update the IP list in two places when the team grows:
1. The Caddyfile (this file)
2. The `ADMIN_ALLOWED_IPS` env var on the backend

A mismatch is fine for short windows during rollout; long-term,
the FE getting a 403 from Caddy vs. a 403 from the backend looks
identical to admins so the symptom isn't misleading.

### Trusted proxies — `TRUSTED_PROXY_CIDRS` (#107)

The backend derives the client IP (for the allowlist above **and**
the auth/brute-force rate limiters) from `X-Forwarded-For` only when
the immediate TCP peer is a trusted proxy. Otherwise XFF is ignored
and `RemoteAddr` is used — so a caller that reaches the backend port
directly (bypassing Caddy) cannot forge `X-Forwarded-For` to satisfy
the allowlist or to mint a fresh rate-limit counter per request.

- Default (env unset): loopback only (`127.0.0.0/8`, `::1/128`) —
  correct for the standard deploy where the backend binds `127.0.0.1`
  behind a same-host Caddy.
- If something other than a local proxy terminates the connection to
  this process (e.g. Caddy on another host, or Cloudflare directly),
  set `TRUSTED_PROXY_CIDRS` to those peer ranges, comma-separated:
  `TRUSTED_PROXY_CIDRS=10.0.0.0/8,172.16.0.0/12`.

Symptom of a too-narrow list: admins 403 even from an allowed IP
(their real IP got replaced by the proxy's because XFF was ignored).
Symptom of a too-wide list: the allowlist/rate-limit become
spoofable again. Keep it to the actual proxy ranges.
