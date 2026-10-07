CREATE INDEX IF NOT EXISTS idx_match_participants_units ON match_participants USING GIN (units jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_match_participants_traits ON match_participants USING GIN (traits jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_participants_match ON match_participants(match_id);
CREATE INDEX IF NOT EXISTS idx_participants_placement ON match_participants(placement);
CREATE INDEX IF NOT EXISTS idx_participants_augments_gin ON match_participants USING GIN (augments jsonb_path_ops);
