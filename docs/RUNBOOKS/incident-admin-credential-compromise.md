# Runbook — Admin credential compromise

> Triggers: a leaked password / leaked refresh token / lost MFA
> device / suspicious admin activity in the audit log.
>
> Owner: on-call admin operator. ETA to full remediation: 30
> minutes if the rotation tooling is current; 90 minutes
> otherwise.

This runbook lives at `mootd/docs/RUNBOOKS/incident-admin-credential-compromise.md`.
It pairs with the secret-rotation schedule in
[secret-rotation-schedule.md](./secret-rotation-schedule.md).

---

## 1. Decide whether this is real

Five-minute triage before any rotations. The cost of a false
alarm (admin lockout for the legitimate user, broken sessions
for everyone else) is real and we'd rather pay it once than
weekly.

| Signal | Real? |
|---|---|
| User reports phishing / lost laptop / lost phone | Yes |
| Audit log shows actions from an unfamiliar IP | Likely |
| Audit log shows actions outside the user's normal hours | Maybe — check vacation / on-call rotation |
| Failed login bursts in nginx / Caddy logs | Maybe — check whether they succeeded; rate-limit may have caught it |
| Single 401 with weird user-agent | Probably not |

If unclear, escalate to a second operator before running any of
the destructive steps below.

---

## 2. Stop the bleeding (5 minutes)

Goal: end the compromised session without taking down everyone
else.

### Revoke the affected admin's refresh tokens

```bash
# Replace with the affected admin's email.
docker compose exec mongo mongosh --quiet --username mootd --password mootd_dev --authenticationDatabase admin mootd <<'EOF'
const email = "compromised@example.com";
const adm = db.admins.findOne({ email });
if (!adm) { print("no such admin"); quit(); }
const res = db.admin_refresh_tokens.updateMany(
  { adminId: adm._id, revokedAt: { $exists: false } },
  { $set: { revokedAt: new Date() } }
);
print("revoked", res.modifiedCount, "refresh tokens for", email);
EOF
```

Their access token still has up to 15 minutes of life
(`DefaultAdminJWTExpiry`), but every subsequent /refresh attempt
fails. If 15 minutes is too long, rotate `ADMIN_JWT_SECRET` (see §3).

### Disable the account temporarily

```bash
# Same shell.
db.admins.updateOne({ email }, { $set: { disabledAt: new Date() } });
```

The login handler short-circuits to a 401 when `disabledAt` is set
(see [admin/handler.go login flow](../../backend/internal/admin/handler.go)).
Reverse with `$unset: { disabledAt: "" }` once the user is back.

---

## 3. Decide on rotation depth

| Scenario | Rotate ADMIN_JWT_SECRET? | Rotate JWT_SECRET? | Other |
|---|---|---|---|
| Single admin's password leaked | No | No | Force password reset for that admin |
| Admin's refresh token leaked | Optional | No | The revoke in §2 is enough; rotate if unsure |
| Backend host compromised | **Yes** | **Yes** | Rotate every secret in [the schedule](./secret-rotation-schedule.md) |
| Source control compromised | **Yes** | **Yes** | Rotate every secret + audit recent commits |

`ADMIN_JWT_SECRET` rotation is **disruptive** — every active
admin gets logged out and has to sign in again. Don't do it
casually.

---

## 4. Rotate ADMIN_JWT_SECRET (when warranted)

```bash
# Generate a new 64-char secret.
openssl rand -hex 32  # → e.g. 7b2a3f...

# Set the new value in the deployment env (e.g. via Render dashboard,
# fly secrets set, kubectl edit secret) — DO NOT inline into the repo.
# The backend's config.Load refuses to start when ADMIN_JWT_SECRET ==
# JWT_SECRET, so make sure they differ.

# Restart the backend.
docker compose restart backend  # or whatever the deploy mechanism is
```

After restart:
- Every admin sees a 401 on their next /me ping → frontend
  redirects to /login.
- Every refresh-token row in the DB is now signed with the old
  secret and won't validate. They're effectively revoked
  without needing to delete them.

---

## 5. Force password reset

There's no admin password-reset endpoint today (P0 simplicity);
the operator changes the hash directly:

```bash
# Generate a new argon2id hash. The bootstrap-admin script in
# cmd/bootstrap-admin/ does this — easiest path is to re-run it
# with the same email + a fresh password.
go run ./cmd/bootstrap-admin -email compromised@example.com -password "<new strong password>"

# The script's --force flag overwrites an existing record. Without
# --force it refuses to clobber.
go run ./cmd/bootstrap-admin -email compromised@example.com -password "..." -force
```

Communicate the new password to the user out-of-band (Signal,
in-person). Do **not** put it in chat / email / any system the
attacker might also have.

---

## 6. Force MFA re-enrollment

If the lost device is suspected to retain access to the
authenticator app's secret, reset MFA:

```bash
docker compose exec mongo mongosh --quiet --username mootd --password mootd_dev --authenticationDatabase admin mootd <<'EOF'
const email = "compromised@example.com";
db.admins.updateOne(
  { email },
  {
    $set: { mfaEnforced: false },
    $unset: { mfaSecret: "", mfaRecoveryCodes: "" },
  }
);
print("MFA cleared for", email, "— ask them to re-enroll via /settings on next login");
EOF
```

The user must log in (now with password only) and re-enroll
through the Settings page. Their old recovery codes no longer
work.

---

## 7. Audit log review

Pull every action by the affected admin since the suspected
compromise window. The /audit page filters by adminId; for the
shell:

```bash
docker compose exec mongo mongosh --quiet --username mootd --password mootd_dev --authenticationDatabase admin mootd <<'EOF'
const email = "compromised@example.com";
const adm = db.admins.findOne({ email });
const sinceMs = Date.now() - 7 * 24 * 60 * 60 * 1000; // last 7 days
db.admin_audit
  .find({ adminId: adm._id, at: { $gte: new Date(sinceMs) } })
  .sort({ at: -1 })
  .forEach((row) => printjson(row));
EOF
```

Look for:

- `pii.reveal` actions — check the `targetUserId` and email
  the affected end-user if their data was viewed.
- `budget.update` — were caps raised to allow runaway spend?
  Reverse and audit the corresponding /reports/weekly.
- `model_routing.update` — did someone pin a tier to a more
  expensive provider?
- `eval.start` / `report.weekly.send` — usually harmless, but
  the recipient address on the latter could leak data
  externally.
- `rbac.denied` — these are *good* (the gate fired), but
  high volume means someone was probing.

---

## 8. User notification (PII-touch only)

If the audit log shows `pii.reveal` events, the affected end
users have a right to know. Template:

> Subject: Mootd account access notice
>
> We're contacting you because an administrator viewed
> identifiable account information (email / wardrobe photos /
> outfit details) on YYYY-MM-DD. The administrator account has
> since been secured. No payment information or content was
> changed.
>
> If you have any concerns, reply to this email and we'll
> respond within 24 hours.

Keep this honest and specific. "Identifiable information" with
the kinds enumerated; not vague "personal data."

---

## 9. Post-mortem checklist

Within 48 hours:

- [ ] Write a 1-page post-mortem in `docs/INCIDENTS/YYYY-MM-DD.md`.
  Include the timeline, what was rotated, what was audited.
- [ ] If the cause was a phish: forward the phishing email to
  the operator team for awareness.
- [ ] If the cause was a leaked secret in source control:
  audit `git log -p` for any other secrets in the same blast
  radius and rotate them too.
- [ ] Update this runbook with anything that surprised you.
  Runbooks degrade unless they're maintained.

---

## Appendix — the destructive shell snippets in one place

For copy-paste during the incident. **Read each one before running.**

```bash
# Set once.
EMAIL="compromised@example.com"
MONGO="docker compose exec -T mongo mongosh --quiet --username mootd --password mootd_dev --authenticationDatabase admin mootd"

# Revoke refresh tokens
$MONGO --eval "
  const a = db.admins.findOne({email: '$EMAIL'});
  if (!a) { print('no such admin'); quit(); }
  print('revoked: ' + db.admin_refresh_tokens.updateMany({adminId: a._id, revokedAt: {\$exists: false}}, {\$set: {revokedAt: new Date()}}).modifiedCount);
"

# Disable account
$MONGO --eval "db.admins.updateOne({email: '$EMAIL'}, {\$set: {disabledAt: new Date()}});"

# Re-enable account
$MONGO --eval "db.admins.updateOne({email: '$EMAIL'}, {\$unset: {disabledAt: ''}});"

# Clear MFA (force re-enrollment)
$MONGO --eval "
  db.admins.updateOne({email: '$EMAIL'}, {
    \$set: {mfaEnforced: false},
    \$unset: {mfaSecret: '', mfaRecoveryCodes: ''}
  });
"

# Audit-log review (last 7 days)
$MONGO --eval "
  const a = db.admins.findOne({email: '$EMAIL'});
  db.admin_audit.find({adminId: a._id, at: {\$gte: new Date(Date.now() - 7*24*60*60*1000)}}).sort({at: -1}).forEach(printjson);
"
```
