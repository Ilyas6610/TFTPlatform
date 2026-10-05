-- game_name/tag_line are nullable: a PUUID discovered organically as another
-- participant in an ingested match (see internal/ingest/crawler.go) is
-- inserted with only a puuid, and its Riot ID is backfilled lazily later via
-- account-v1. The unique index below only applies once both are known.
CREATE TABLE accounts (
    puuid           TEXT PRIMARY KEY,
    game_name       TEXT,
    tag_line        TEXT,
    routing_region  TEXT NOT NULL,
    last_fetched_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_accounts_riot_id ON accounts(game_name, tag_line)
    WHERE game_name IS NOT NULL AND tag_line IS NOT NULL;

CREATE TABLE summoners (
    puuid            TEXT PRIMARY KEY REFERENCES accounts(puuid) ON DELETE CASCADE,
    platform_region  TEXT NOT NULL,
    summoner_id      TEXT,
    profile_icon_id  INTEGER,
    summoner_level   INTEGER,
    last_fetched_at  TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_summoners_platform ON summoners(platform_region);
