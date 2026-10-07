-- A player whose crawl fails (an id list Riot rejects, a match that keeps
-- erroring) is retried later with exponential backoff instead of blocking
-- the head of the crawl queue on every run.
ALTER TABLE ingest_puuid_queue
    ADD COLUMN failures        SMALLINT    NOT NULL DEFAULT 0,
    ADD COLUMN last_error      TEXT,
    ADD COLUMN next_attempt_at TIMESTAMPTZ;
