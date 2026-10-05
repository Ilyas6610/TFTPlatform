CREATE TABLE match_participants (
    id                BIGSERIAL PRIMARY KEY,
    match_id          TEXT NOT NULL REFERENCES matches(match_id) ON DELETE CASCADE,
    puuid             TEXT NOT NULL REFERENCES accounts(puuid) ON DELETE CASCADE,
    placement         SMALLINT NOT NULL,
    level             SMALLINT,
    last_round        SMALLINT,
    players_eliminated INTEGER,
    total_damage_to_players INTEGER,
    gold_left         SMALLINT,
    time_eliminated   DOUBLE PRECISION,
    tier_context      TEXT,
    units             JSONB NOT NULL,
    traits            JSONB NOT NULL,
    augments          JSONB,
    companion         JSONB,
    raw_participant   JSONB NOT NULL,
    UNIQUE (match_id, puuid)
);
CREATE INDEX idx_participants_puuid ON match_participants(puuid);
CREATE INDEX idx_participants_match ON match_participants(match_id);
CREATE INDEX idx_participants_placement ON match_participants(placement);
CREATE INDEX idx_participants_units_gin ON match_participants USING GIN (units jsonb_path_ops);
CREATE INDEX idx_participants_traits_gin ON match_participants USING GIN (traits jsonb_path_ops);
CREATE INDEX idx_participants_augments_gin ON match_participants USING GIN (augments jsonb_path_ops);
