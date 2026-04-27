# Vendored OpenAPI specs

This directory holds a **vendored copy** of the canonical specs from
[mootd-contracts](https://github.com/spodolaks/mootd-contracts).

## Why vendored, not a submodule

A solo founder + a small future team don't need the ceremony of git
submodules. The spec is small, rarely changes, and codegen depends
on having it locally available without any clone/init step.

## Update workflow

```bash
# From the repo root
cp ../mootd-contracts/openapi/admin-api.yaml backend/contracts/admin-api.yaml
make gen-admin           # regenerates backend/internal/admin/gen/types.go
go test ./...            # verify nothing broke
git add backend/contracts/admin-api.yaml backend/internal/admin/gen/
git commit -m "spec(admin): bump to mootd-contracts@<sha>"
```

## Drift guard

CI runs `make gen-admin` and fails if the diff is non-empty. This
catches:

1. Hand-edits to the generated files (you'd have to revert + edit
   the spec).
2. Forgetting to rerun codegen after a spec change.

The mootd-admin frontend has its own copy of the same file — both
must be updated together when the spec evolves. The pinned-SHA
discipline lives in commit messages, not tooling.
