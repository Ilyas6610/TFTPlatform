CREATE TABLE ingest_puuid_queue (
    puuid              TEXT PRIMARY KEY REFERENCES accounts(puuid) ON DELETE CASCADE,
    platform_region    TEXT NOT NULL,
    routing_region     TEXT NOT NULL,
    priority           SMALLINT NOT NULL DEFAULT 0,
    last_crawled_at    TIMESTAMPTZ,
    last_match_id_seen TEXT,
    added_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_queue_priority ON ingest_puuid_queue(priority DESC, last_crawled_at NULLS FIRST);

CREATE TABLE ingest_runs (
    id                BIGSERIAL PRIMARY KEY,
    run_type          TEXT NOT NULL,
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at       TIMESTAMPTZ,
    status            TEXT NOT NULL DEFAULT 'running',
    requests_made     INTEGER NOT NULL DEFAULT 0,
    matches_ingested  INTEGER NOT NULL DEFAULT 0,
    error_detail      TEXT
);
