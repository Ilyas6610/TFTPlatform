#!/usr/bin/env bash
# Brings up Postgres and runs migrations for local development.
#
# NOTE: personal Riot API keys expire every 24h. Before running ingestcli,
# make sure deploy/.secrets/riot-api-key.txt has today's key.
set -euo pipefail
cd "$(dirname "$0")/../deploy"

docker-compose up -d postgres
docker-compose up migrate

# Database for Postgres-backed Go tests (see internal/store/storetest).
docker-compose exec -T postgres psql -U tft -d tft -tAc "SELECT 1 FROM pg_database WHERE datname = 'tft_test'" | grep -q 1 \
  || docker-compose exec -T postgres psql -U tft -d tft -c "CREATE DATABASE tft_test"
