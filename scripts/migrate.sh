#!/usr/bin/env bash
# Creates a new pair of migration files: NNNN_name.up.sql / NNNN_name.down.sql
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -z "${1:-}" ]; then
  echo "usage: scripts/migrate.sh <migration_name>" >&2
  exit 1
fi

docker run --rm -v "$(pwd)/internal/db/migrations:/migrations" \
  migrate/migrate:v4.17.1 create -ext sql -dir /migrations -seq "$1"
