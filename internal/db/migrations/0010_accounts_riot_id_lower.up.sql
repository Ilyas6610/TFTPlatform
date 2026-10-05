-- Riot IDs are case-insensitive, so profile lookups match on lower() and
-- "Name#TAG" and "name#tag" share one cached account.
CREATE INDEX idx_accounts_riot_id_lower ON accounts (lower(game_name), lower(tag_line))
    WHERE game_name IS NOT NULL AND tag_line IS NOT NULL;
