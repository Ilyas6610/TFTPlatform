-- GIN indexes for the stats explorer (internal/store/explore.go): its unit
-- conditions use `units @> '[{"character_id": ...}]'`, which these serve;
-- jsonb_path_ops keeps them small since only containment is needed.
CREATE INDEX idx_match_participants_units ON match_participants USING GIN (units jsonb_path_ops);
CREATE INDEX idx_match_participants_traits ON match_participants USING GIN (traits jsonb_path_ops);
