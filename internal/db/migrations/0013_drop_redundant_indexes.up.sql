-- Indexes that cost every match insert (GIN writes are the expensive part of
-- storing a participant) without helping a query:
--   * idx_match_participants_units/_traits (0009) are identical to
--     idx_participants_units_gin/_traits_gin (0004), which stay;
--   * idx_participants_match is the leading column of the unique
--     (match_id, puuid) constraint;
--   * idx_participants_placement has 8 distinct values, never selective;
--   * idx_participants_augments_gin: nothing filters on augments (Set 18's
--     match data has none, and the stats queries only unnest them).
DROP INDEX IF EXISTS idx_match_participants_units;
DROP INDEX IF EXISTS idx_match_participants_traits;
DROP INDEX IF EXISTS idx_participants_match;
DROP INDEX IF EXISTS idx_participants_placement;
DROP INDEX IF EXISTS idx_participants_augments_gin;
