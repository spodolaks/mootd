#!/usr/bin/env bash
# Dump or restore the full MOOTD MongoDB database, including GridFS image blobs.
#
# The `mootd` database holds every piece of persistent state the backend owns:
# users, refresh tokens, wardrobe items, outfits, moodboards, surfaces, plus
# the `fs.files`/`fs.chunks` GridFS collections that back every wardrobe,
# moodboard, and surface image. A single `mongodump --archive --gzip` of that
# database is a complete, self-contained migration artifact.
#
# Redis is intentionally not migrated — it only holds caches, rate-limit
# counters, and async outfit job state, all of which the backend rebuilds.
#
# Usage:
#   scripts/migrate-db.sh dump    [OUTPUT_FILE]   # default: ./mootd-YYYYMMDD-HHMMSS.archive.gz
#   scripts/migrate-db.sh restore INPUT_FILE
#
# Environment overrides (all optional):
#   MONGO_CONTAINER   docker container running mongod        (default: mootd-mongo)
#   MONGO_HOST        host for direct mongo connections      (default: 127.0.0.1)
#   MONGO_PORT        host port for direct mongo connections (default: 27018)
#   MONGO_DB          database name                          (default: mootd)
#   MONGO_USER        root username                          (default: mootd)
#   MONGO_PASSWORD    root password                          (default: mootd_dev)
#   MONGO_URI         full connection string; overrides host/port/user/password
#   USE_DOCKER        "1" to exec mongodump/mongorestore inside $MONGO_CONTAINER
#                     instead of using a locally installed client. Auto-detected
#                     when unset: prefers docker if the container is running.
#
# Examples:
#   # On the source server — produce a migration file
#   scripts/migrate-db.sh dump /tmp/mootd-prod.archive.gz
#
#   # Copy the archive to the new server, then restore into its Mongo
#   scripts/migrate-db.sh restore /tmp/mootd-prod.archive.gz
#
#   # Restore directly against a remote Atlas cluster
#   MONGO_URI='mongodb+srv://user:pw@cluster/?authSource=admin' \
#     scripts/migrate-db.sh restore /tmp/mootd-prod.archive.gz

set -euo pipefail

MONGO_CONTAINER="${MONGO_CONTAINER:-mootd-mongo}"
MONGO_HOST="${MONGO_HOST:-127.0.0.1}"
MONGO_PORT="${MONGO_PORT:-27018}"
MONGO_DB="${MONGO_DB:-mootd}"
MONGO_USER="${MONGO_USER:-mootd}"
MONGO_PASSWORD="${MONGO_PASSWORD:-mootd_dev}"

die() { echo "error: $*" >&2; exit 1; }

# Decide whether to run the mongo tools inside the docker container or on the
# host. Container mode avoids requiring a local mongodump install and works
# against the compose-managed instance out of the box.
detect_mode() {
  if [[ -n "${USE_DOCKER:-}" ]]; then
    echo "$USE_DOCKER"
    return
  fi
  if command -v docker >/dev/null 2>&1 \
      && docker ps --format '{{.Names}}' | grep -qx "$MONGO_CONTAINER"; then
    echo "1"
  else
    echo "0"
  fi
}

# Build the connection args for mongodump/mongorestore. When MONGO_URI is set
# we pass it through verbatim so users can point at any cluster (Atlas, a
# different host, etc). Otherwise we synthesize one from the discrete vars.
build_conn_args() {
  if [[ -n "${MONGO_URI:-}" ]]; then
    printf -- '--uri=%s' "$MONGO_URI"
    return
  fi
  # --host/--port rather than --uri so credentials with special chars don't
  # need URL-encoding.
  printf -- '--host=%s --port=%s --username=%s --password=%s --authenticationDatabase=admin' \
    "$MONGO_HOST" "$MONGO_PORT" "$MONGO_USER" "$MONGO_PASSWORD"
}

run_mongo_tool() {
  local tool="$1"; shift
  local mode; mode="$(detect_mode)"
  if [[ "$mode" == "1" ]]; then
    # -i keeps stdin open so archive streams over the docker exec pipe. No -t
    # because we don't want a TTY mangling the binary stream.
    docker exec -i "$MONGO_CONTAINER" "$tool" "$@"
  else
    command -v "$tool" >/dev/null 2>&1 \
      || die "$tool not found on host; install mongodb-database-tools or set USE_DOCKER=1"
    "$tool" "$@"
  fi
}

cmd_dump() {
  local out="${1:-}"
  if [[ -z "$out" ]]; then
    out="./mootd-$(date +%Y%m%d-%H%M%S).archive.gz"
  fi
  local out_dir; out_dir="$(cd "$(dirname "$out")" && pwd)"
  local out_abs="$out_dir/$(basename "$out")"

  # shellcheck disable=SC2046
  # We write the archive to stdout and capture it on the host, so the output
  # file lands where the operator runs the script — works identically whether
  # mongodump runs inside the container or on the host.
  run_mongo_tool mongodump \
    $(build_conn_args) \
    --db="$MONGO_DB" \
    --archive \
    --gzip \
    > "$out_abs"

  local bytes; bytes="$(wc -c < "$out_abs" | tr -d ' ')"
  echo "wrote $out_abs ($bytes bytes)"
  echo
  echo "To restore on the target server:"
  echo "  scripts/migrate-db.sh restore $(basename "$out_abs")"
}

cmd_restore() {
  local in="${1:-}"
  [[ -n "$in" ]] || die "restore requires an input archive path"
  [[ -f "$in" ]] || die "archive not found: $in"

  echo "About to restore '$in' into database '$MONGO_DB'."
  echo "Existing collections in '$MONGO_DB' on the target will be DROPPED and replaced."
  if [[ -z "${ASSUME_YES:-}" ]]; then
    read -r -p "Continue? [y/N] " reply
    [[ "$reply" =~ ^[Yy]$ ]] || die "aborted"
  fi

  # --drop replaces each collection atomically. --nsInclude scopes the restore
  # to the mootd DB even if the archive happens to contain other namespaces,
  # which keeps this safe to run against a shared cluster.
  # shellcheck disable=SC2046
  run_mongo_tool mongorestore \
    $(build_conn_args) \
    --archive \
    --gzip \
    --drop \
    --nsInclude="${MONGO_DB}.*" \
    < "$in"

  echo "restore complete"
}

usage() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

main() {
  [[ $# -ge 1 ]] || usage 1
  case "$1" in
    dump)    shift; cmd_dump "$@" ;;
    restore) shift; cmd_restore "$@" ;;
    -h|--help|help) usage 0 ;;
    *) usage 1 ;;
  esac
}

main "$@"
