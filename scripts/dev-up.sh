#!/usr/bin/env bash
# Brings up Postgres and runs migrations for local development.
#
# NOTE: personal Riot API keys expire every 24h. Before running ingestcli,
# make sure deploy/.secrets/riot-api-key.txt has today's key.
set -euo pipefail
cd "$(dirname "$0")/../deploy"

# Docker Compose v2 is a docker plugin; older installs have the standalone
# docker-compose binary.
if docker compose version >/dev/null 2>&1; then
  compose() { docker compose "$@"; }
else
  compose() { docker-compose "$@"; }
fi

compose up -d postgres
compose up migrate

# Database for Postgres-backed Go tests (see internal/store/storetest).
compose exec -T postgres psql -U tft -d tft -tAc "SELECT 1 FROM pg_database WHERE datname = 'tft_test'" | grep -q 1 \
  || compose exec -T postgres psql -U tft -d tft -c "CREATE DATABASE tft_test"
