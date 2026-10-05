CREATE TABLE league_entries (
    id               BIGSERIAL PRIMARY KEY,
    puuid            TEXT NOT NULL REFERENCES accounts(puuid) ON DELETE CASCADE,
    platform_region  TEXT NOT NULL,
    queue_type       TEXT NOT NULL DEFAULT 'RANKED_TFT',
    tier             TEXT NOT NULL,
    rank             TEXT,
    league_points    INTEGER NOT NULL,
    wins             INTEGER NOT NULL,
    losses           INTEGER NOT NULL,
    hot_streak       BOOLEAN NOT NULL DEFAULT false,
    fetched_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (puuid, platform_region, queue_type)
);
CREATE INDEX idx_league_leaderboard ON league_entries(platform_region, tier, league_points DESC);
