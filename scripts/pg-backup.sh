#!/usr/bin/env bash
# Dumps the database with pg_dump (custom format) into BACKUP_DIR and keeps
# the newest BACKUP_KEEP dumps. The connection comes from the standard
# libpq variables (PGHOST, PGUSER, PGPASSWORD, PGDATABASE).
#
#   scripts/pg-backup.sh          one dump, then exit
#   scripts/pg-backup.sh --loop   a dump every BACKUP_INTERVAL_SECONDS (default a day);
#                                 a failed dump is logged and retried next round
#
# Restore with scripts/pg-restore.sh. Keep a copy off the host too: a dump
# next to the database doesn't survive losing the machine.
set -euo pipefail

dir="${BACKUP_DIR:-/backups}"
keep="${BACKUP_KEEP:-7}"
interval="${BACKUP_INTERVAL_SECONDS:-86400}"

backup_once() {
  mkdir -p "$dir"
  local ts final tmp
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  final="$dir/tft-$ts.dump"
  tmp="$dir/.tft-$ts.dump.partial"
  # Written under a partial name and renamed when complete, so a crash never
  # leaves something that looks like a usable dump.
  if ! pg_dump --format=custom --no-owner --file="$tmp"; then
    rm -f -- "$tmp"
    return 1
  fi
  mv -- "$tmp" "$final"
  # Keep the newest $keep dumps.
  ls -1t -- "$dir"/tft-*.dump 2>/dev/null | tail -n +"$((keep + 1))" | xargs -r rm -f --
  echo "pg-backup: wrote $final ($(du -h -- "$final" | cut -f1)), keeping $keep"
}

if [ "${1:-}" = "--loop" ]; then
  while true; do
    backup_once || echo "pg-backup: dump failed; retrying in ${interval}s" >&2
    sleep "$interval"
  done
else
  backup_once
fi
