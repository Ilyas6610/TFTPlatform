#!/bin/sh
# Keeps match data and meta stats fresh in deployment: re-seeds the apex
# ladders every SEED_INTERVAL, crawls a bounded batch of queued players every
# CRAWL_INTERVAL, and recomputes meta stats after each crawl. Every step is
# bounded by ingestcli's own request budget, so this stays within a
# personal key's rate limits; failures are logged and retried next cycle.
set -u

PLATFORMS="${PLATFORMS:-na1}"
SEED_INTERVAL="${SEED_INTERVAL:-21600}"   # seconds (6h)
CRAWL_INTERVAL="${CRAWL_INTERVAL:-300}"   # seconds (5m)

last_seed=0
while true; do
  now=$(date +%s)
  if [ $((now - last_seed)) -ge "$SEED_INTERVAL" ]; then
    for p in $PLATFORMS; do
      ingestcli seed-leaderboard --platform "$p" || echo "ingest-loop: seed $p failed" >&2
    done
    last_seed=$now
  fi
  ingestcli crawl-queue || echo "ingest-loop: crawl failed" >&2
  ingestcli aggregate || echo "ingest-loop: aggregate failed" >&2
  sleep "$CRAWL_INTERVAL"
done
