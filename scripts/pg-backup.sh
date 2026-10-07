#!/usr/bin/env bash
# Dumps the database with pg_dump (custom format) into BACKUP_DIR and keeps
# the newest BACKUP_KEEP dumps. The connection comes from the standard
# libpq variables (PGHOST, PGUSER, PGPASSWORD, PGDATABASE).
#
#   scripts/pg-backup.sh          one dump, then exit
#   scripts/pg-backup.sh --loop   a dump every BACKUP_INTERVAL_SECONDS (default a day), counted
#                                 from the newest dump so restarts don't add extras;
#                                 a failed dump is logged and retried after 5 minutes
#
# Restore with scripts/pg-restore.sh. Keep a copy off the host too: a dump
# next to the database doesn't survive losing the machine.
set -euo pipefail
# Dumps are private to their owner (the database's contents, even if derived
# from public Riot data, aren't for anyone with host access to read).
umask 077

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

# Seconds until the next dump is due: the interval since the newest one, so a
# container that restarts in a loop doesn't write a dump per restart and
# rotate older good days out.
wait_for_next() {
  local newest age
  newest="$(ls -1t -- "$dir"/tft-*.dump 2>/dev/null | head -n 1 || true)"
  if [ -z "$newest" ]; then
    echo 0
    return
  fi
  age=$(( $(date +%s) - $(stat -c %Y -- "$newest") ))
  if [ "$age" -lt "$interval" ]; then
    echo $(( interval - age ))
  else
    echo 0
  fi
}

if [ "${1:-}" = "--loop" ]; then
  retry=$(( interval < 300 ? interval : 300 ))
  while true; do
    wait="$(wait_for_next)"
    if [ "$wait" -gt 0 ]; then
      echo "pg-backup: newest dump is recent; next in ${wait}s"
      sleep "$wait"
    fi
    if ! backup_once; then
      echo "pg-backup: dump failed; retrying in ${retry}s" >&2
      sleep "$retry"
    fi
  done
else
  backup_once
fi
