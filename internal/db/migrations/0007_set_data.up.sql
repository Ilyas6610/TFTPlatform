-- Normalized static game data for a TFT set (units, traits, augments,
-- items) as of one CommunityDragon content version — see internal/setdata.
-- One row per version, so diffing consecutive rows gives each patch's
-- number changes.
CREATE TABLE set_data_snapshots (
    set_number  INTEGER NOT NULL,
    version     TEXT NOT NULL,         -- e.g. "16.19.8230722"
    patch       TEXT NOT NULL,         -- e.g. "16.19"
    data        JSONB NOT NULL,
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (set_number, version)
);
