#!/usr/bin/env bash
# Restores a dump made by scripts/pg-backup.sh into a database.
#
#   scripts/pg-restore.sh <dump-file> [database]
#
# The connection comes from the standard libpq variables (PGHOST, PGUSER,
# PGPASSWORD); database defaults to PGDATABASE. The database must exist.
# Existing objects are dropped and recreated (--clean), so point it at a
# scratch database first to check a dump before replacing a live one.
set -euo pipefail

dump="${1:?usage: pg-restore.sh <dump-file> [database]}"
db="${2:-${PGDATABASE:?name the target database as the second argument or set PGDATABASE}}"

pg_restore --no-owner --clean --if-exists --exit-on-error --dbname="$db" -- "$dump"
echo "pg-restore: restored $dump into $db"
