#!/usr/bin/env bash
# Brings up Postgres and runs migrations for local development.
#
# NOTE: personal Riot API keys expire every 24h. Before running ingestcli,
# make sure deploy/.secrets/riot-api-key.txt has today's key.
set -euo pipefail
cd "$(dirname "$0")/../deploy"

docker-compose up -d postgres
docker-compose up migrate
