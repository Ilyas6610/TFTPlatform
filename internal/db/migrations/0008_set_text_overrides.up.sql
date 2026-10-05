-- Fully-rendered description text for a set's units/augments/items from a
-- third-party source, used where Riot's exported data leaves values out
-- (see internal/setdata/overrides.go). One row per set and source, holding
-- the latest fetch and the set data version it was fetched against.
CREATE TABLE set_text_overrides (
    set_number  INTEGER NOT NULL,
    source      TEXT NOT NULL,
    version     TEXT NOT NULL,
    data        JSONB NOT NULL,          -- {"<apiName>": "<description template>"}
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (set_number, source)
);
