CREATE TABLE matches (
    match_id         TEXT PRIMARY KEY,
    routing_region   TEXT NOT NULL,
    game_datetime    TIMESTAMPTZ NOT NULL,
    game_length      DOUBLE PRECISION,
    game_version     TEXT NOT NULL,
    tft_set_number   INTEGER,
    queue_id         INTEGER,
    tft_game_type    TEXT,
    raw_payload      JSONB NOT NULL,
    ingested_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_matches_datetime ON matches(game_datetime);
CREATE INDEX idx_matches_set ON matches(tft_set_number);
