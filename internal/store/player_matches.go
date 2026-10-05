package store

import (
	"context"
	"time"
)

type PlayerMatchSummary struct {
	MatchID      string
	GameDatetime time.Time
	TFTSetNumber int
	Placement    int
	Level        int
}

// GetRecentMatchesForPUUID returns the most recent ingested matches for a
// player, newest first. Postgres-only — a first-ever lookup with nothing
// ingested yet returns an empty slice; fetching from Riot happens in the
// background (see internal/apiserver/handlers_player_matches.go), never on
// the request path.
func (s *Store) GetRecentMatchesForPUUID(ctx context.Context, puuid string, limit int) ([]PlayerMatchSummary, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT mp.match_id, m.game_datetime, m.tft_set_number, mp.placement, mp.level
		FROM match_participants mp
		JOIN matches m USING (match_id)
		WHERE mp.puuid = $1
		ORDER BY m.game_datetime DESC
		LIMIT $2
	`, puuid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerMatchSummary
	for rows.Next() {
		var m PlayerMatchSummary
		if err := rows.Scan(&m.MatchID, &m.GameDatetime, &m.TFTSetNumber, &m.Placement, &m.Level); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
