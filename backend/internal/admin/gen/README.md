# Generated admin API types

Auto-generated from `backend/contracts/admin-api.yaml` (vendored from
[mootd-contracts](https://github.com/spodolaks/mootd-contracts)).

**Do not edit by hand.** Run `make gen-admin` from `backend/` to
regenerate.

## Why we don't use these in handlers

Two reasons:

1. **Naming convention friction.** oapi-codegen produces `Id` where
   Go convention is `ID`, `Totp` where Go convention is `TOTP`. We
   could plaster `x-go-name` overrides through the spec but every
   such override is one more thing to forget on a new endpoint.
2. **Pointer-typed optionals.** A `*string` for an optional TOTP is
   precise but ergonomically expensive in handler code (nil-check
   on every read).

Hand-written types in `backend/internal/admin/domain.go`,
`backend/internal/admin/users.go`, etc. continue to be what handlers
return.

## Why we generate them anyway

Drift detection. CI runs `make gen-check` which regenerates and
fails if the resulting types differ from what's committed. So:

- A spec change without a corresponding handler change fails the
  build (the gen types drift from what the spec says, but the
  hand-written ones haven't been updated).
- A handler change without a spec change is caught by code review:
  the wire shape doesn't match anything in the gen package.
- Both pressures keep the spec as the source of truth without
  forcing the handlers to use generated structs directly.

## Forward-looking

If we add a TON more admin endpoints (P3+, P4+) the maintenance
benefit of switching to gen-types-in-handlers will eventually
outweigh the convention friction. At that point a small refactor
+ `x-go-name` overrides in the spec is the right move. Today it
isn't worth the churn.
