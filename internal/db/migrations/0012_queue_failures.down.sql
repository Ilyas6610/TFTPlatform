ALTER TABLE ingest_puuid_queue
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS failures;
