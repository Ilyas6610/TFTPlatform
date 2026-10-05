-- tier_context is reserved for future per-elo slicing (e.g. 'CHALLENGER')
-- but always 'ALL' for now — see internal/aggregate.
CREATE TABLE meta_unit_stats (
    tft_set_number   INTEGER NOT NULL,
    character_id     TEXT NOT NULL,
    tier_context     TEXT NOT NULL DEFAULT 'ALL',
    games_played     INTEGER NOT NULL,
    avg_placement    NUMERIC(4,2) NOT NULL,
    top4_rate        NUMERIC(5,4) NOT NULL,
    win_rate         NUMERIC(5,4) NOT NULL,
    pick_rate        NUMERIC(5,4) NOT NULL,
    computed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tft_set_number, character_id, tier_context)
);

CREATE TABLE meta_trait_stats (
    tft_set_number   INTEGER NOT NULL,
    trait_name       TEXT NOT NULL,
    trait_tier       SMALLINT NOT NULL,
    tier_context     TEXT NOT NULL DEFAULT 'ALL',
    games_played     INTEGER NOT NULL,
    avg_placement    NUMERIC(4,2) NOT NULL,
    top4_rate        NUMERIC(5,4) NOT NULL,
    win_rate         NUMERIC(5,4) NOT NULL,
    computed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tft_set_number, trait_name, trait_tier, tier_context)
);

CREATE TABLE meta_augment_stats (
    tft_set_number   INTEGER NOT NULL,
    augment_id       TEXT NOT NULL,
    tier_context     TEXT NOT NULL DEFAULT 'ALL',
    games_played     INTEGER NOT NULL,
    avg_placement    NUMERIC(4,2) NOT NULL,
    top4_rate        NUMERIC(5,4) NOT NULL,
    win_rate         NUMERIC(5,4) NOT NULL,
    computed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tft_set_number, augment_id, tier_context)
);

-- Lets the API expose "stats last updated at T" and lets an operator see
-- whether the aggregator is actually running.
CREATE TABLE meta_compute_runs (
    id              BIGSERIAL PRIMARY KEY,
    tft_set_number  INTEGER NOT NULL,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    matches_scanned INTEGER,
    status          TEXT NOT NULL DEFAULT 'running'
);
