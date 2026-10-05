package store

import (
	"context"
	"time"
)

type LeagueEntry struct {
	PUUID          string
	PlatformRegion string
	Tier           string
	Rank           string
	LeaguePoints   int
	Wins           int
	Losses         int
	HotStreak      bool
}

// UpsertLeagueEntry stores the current-snapshot leaderboard row for a
// player, keyed by (puuid, platform_region, queue_type) — re-seeding
// overwrites the previous snapshot rather than accumulating history.
func (s *Store) UpsertLeagueEntry(ctx context.Context, e LeagueEntry) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO league_entries (puuid, platform_region, queue_type, tier, rank, league_points, wins, losses, hot_streak, fetched_at)
		VALUES ($1, $2, 'RANKED_TFT', $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (puuid, platform_region, queue_type) DO UPDATE SET
			tier = EXCLUDED.tier,
			rank = EXCLUDED.rank,
			league_points = EXCLUDED.league_points,
			wins = EXCLUDED.wins,
			losses = EXCLUDED.losses,
			hot_streak = EXCLUDED.hot_streak,
			fetched_at = now()
	`, e.PUUID, e.PlatformRegion, e.Tier, nullIfEmpty(e.Rank), e.LeaguePoints, e.Wins, e.Losses, e.HotStreak)
	return err
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

type LeaderboardEntry struct {
	PUUID        string
	GameName     *string
	TagLine      *string
	Tier         string
	Rank         *string
	LeaguePoints int
	Wins         int
	Losses       int
	FetchedAt    time.Time
}

// GetLeaderboard returns the current snapshot for platform, ordered apex
// tiers first (challenger > grandmaster > master) then by LP. game_name/tag_line
// are nullable since organically-discovered accounts may not have a
// resolved Riot ID yet.
func (s *Store) GetLeaderboard(ctx context.Context, platform string, limit int) ([]LeaderboardEntry, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT le.puuid, a.game_name, a.tag_line, le.tier, le.rank, le.league_points, le.wins, le.losses, le.fetched_at
		FROM league_entries le
		JOIN accounts a ON a.puuid = le.puuid
		WHERE le.platform_region = $1
		ORDER BY
			CASE le.tier WHEN 'CHALLENGER' THEN 0 WHEN 'GRANDMASTER' THEN 1 WHEN 'MASTER' THEN 2 ELSE 3 END,
			le.league_points DESC
		LIMIT $2
	`, platform, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.PUUID, &e.GameName, &e.TagLine, &e.Tier, &e.Rank, &e.LeaguePoints, &e.Wins, &e.Losses, &e.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
