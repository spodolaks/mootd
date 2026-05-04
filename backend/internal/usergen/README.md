# Generated user-facing API types

Auto-generated from `backend/contracts/user-api.yaml` (vendored from
[mootd-contracts](https://github.com/spodolaks/mootd-contracts), per
mootd-admin#41).

**Do not edit by hand.** Run `make gen-user` from `backend/` to
regenerate.

## Why we don't use these in handlers

Same reasons as `internal/admin/gen/`:

1. **Naming convention friction.** oapi-codegen produces `Id` where
   Go convention is `ID`. We could plaster `x-go-name` overrides
   through the spec but each one is a thing to forget on a new
   endpoint.
2. **Pointer-typed optionals.** Hand-written types use bare `string`
   with `omitempty`; the generator emits `*string`. The latter is
   precise but ergonomically expensive in handler code.

Hand-written types in `backend/internal/{auth,user,wardrobe,outfit,
moodboard,events,brands,generic,surface,privacy,health}/domain.go`
continue to be what handlers return.

## Why we generate them anyway

Drift detection. CI's `make gen-check` regenerates and fails the
build if the result differs from what's committed. So:

- A spec change without a corresponding handler change fails CI on
  the next backfill (the regenerated types won't match the YAML).
- A handler change without a spec update fails the *next* round-trip
  but at least it's caught before a downstream client breaks.

Flow:

1. Edit `mootd-contracts/openapi/user-api.yaml`, push.
2. Vendor: `cp ../mootd-contracts/openapi/user-api.yaml
   backend/contracts/user-api.yaml`.
3. Regenerate: `make gen-user`. Commit `internal/usergen/types.go`.
4. Update the matching hand-written struct in the relevant domain
   package.
5. Update the handler.

## Versioning

The spec's `info.version` is the contract version. Bump it on every
shape-affecting change (additions are minor, removals/renames are
major). Treat the major-version bump as a downstream-breaking event:
RN app + any future client get a heads-up release before the change
ships.
