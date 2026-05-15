#!/usr/bin/env bash
# Deploy the MOOTD backend to a remote host.
#
# Usage:
#   MOOTD_HOST=root@37.27.35.215 scripts/deploy.sh
#
# The script syncs the current working tree (minus ignored paths) to
# /opt/mootd on the target host, then runs `docker compose up -d --build`
# with the production overlay. It does not touch the server's .env — set
# that up once by hand (see deploy/README.md).
#
# Safe to re-run. rsync copies only what changed; docker compose rebuilds
# only the backend image if source under backend/ changed.

set -euo pipefail

HOST="${MOOTD_HOST:-}"
REMOTE_DIR="${MOOTD_REMOTE_DIR:-/opt/mootd}"

if [[ -z "$HOST" ]]; then
    echo "error: MOOTD_HOST is not set" >&2
    echo "usage: MOOTD_HOST=user@host $0" >&2
    exit 2
fi

# Fail fast if we can't reach the host. The connection test doubles as a
# "authentication works" sanity check.
if ! ssh -o ConnectTimeout=5 -o BatchMode=yes "$HOST" true 2>/dev/null; then
    echo "error: cannot SSH into $HOST (wrong key? host down? first-time login?)" >&2
    echo "tip: run ssh $HOST once interactively to accept the host key" >&2
    exit 1
fi

echo "==> Syncing repo to $HOST:$REMOTE_DIR"
# Excludes cover anything the server doesn't need to build the backend. The
# frontend is deployed separately (see deploy/README.md#frontend).
rsync -az --delete \
    --exclude '.git/' \
    --exclude 'node_modules/' \
    --exclude '.expo/' \
    --exclude '.claude/' \
    --exclude 'app/' \
    --exclude '*.log' \
    --exclude '.env' \
    --exclude '.env.*' \
    ./ "$HOST:$REMOTE_DIR/"

echo "==> Building + restarting backend stack"
ssh "$HOST" "cd $REMOTE_DIR && docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build"

echo "==> Tailing last health-check result"
ssh "$HOST" 'curl -fsS http://localhost:8089/healthz' || {
    echo "error: backend did not respond on localhost:8089" >&2
    exit 1
}
echo
echo "==> Deploy complete"
