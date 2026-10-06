-- A player's rank over time, one row per observed change (profile syncs and
-- leaderboard refreshes). Riot's match data has no LP, so per-game LP comes
-- from the difference between the snapshots around a game.
CREATE TABLE rank_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    puuid           TEXT NOT NULL REFERENCES accounts(puuid) ON DELETE CASCADE,
    queue_type      TEXT NOT NULL,
    tier            TEXT NOT NULL,
    rank            TEXT,
    league_points   INTEGER NOT NULL,
    wins            INTEGER NOT NULL,
    losses          INTEGER NOT NULL,
    fetched_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_rank_snapshots_player ON rank_snapshots(puuid, queue_type, fetched_at);

-- When each TFT patch went live. Set 18's matches report no game version,
-- so a game's patch comes from its date. starts_at is approximate: Riot's
-- patch notes publish date plus a day (patches deploy the day after notes).
CREATE TABLE patch_calendar (
    set_number      INTEGER NOT NULL,
    tft_patch       TEXT NOT NULL,   -- "18.3"
    game_patch      TEXT NOT NULL,   -- "16.19"
    starts_at       TIMESTAMPTZ NOT NULL,
    source_url      TEXT NOT NULL,
    PRIMARY KEY (set_number, tft_patch)
);
