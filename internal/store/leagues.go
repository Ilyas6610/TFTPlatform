package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
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

// SnapshotEntry is one leaderboard row plus the crawl-queue priority its
// tier earns (see internal/ingest/seeder.go).
type SnapshotEntry struct {
	LeagueEntry
	RoutingRegion string
	Priority      int16
}

// WriteLeagueSnapshot upserts a leaderboard snapshot for platform in one
// transaction: each player's account (PUUID-only if new), their league entry,
// and their crawl-queue slot. When complete is true the snapshot is the
// whole apex ladder, so any entry for platform not in it — a player who
// dropped out of master+ — is deleted; a partial snapshot (e.g. rate-limited
// partway) only upserts, so it never wipes tiers it didn't fetch.
func (s *Store) WriteLeagueSnapshot(ctx context.Context, platform string, entries []SnapshotEntry, complete bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, e := range entries {
		batch.Queue(`
			INSERT INTO accounts (puuid, routing_region)
			VALUES ($1, $2)
			ON CONFLICT (puuid) DO NOTHING
		`, e.PUUID, e.RoutingRegion)
		// fetched_at = now() is the transaction start time, which the prune
		// below relies on to tell this snapshot's rows from older ones.
		batch.Queue(`
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
		`, e.PUUID, platform, e.Tier, nullIfEmpty(e.Rank), e.LeaguePoints, e.Wins, e.Losses, e.HotStreak)
		batch.Queue(`
			INSERT INTO ingest_puuid_queue (puuid, platform_region, routing_region, priority)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (puuid) DO UPDATE SET
				priority = GREATEST(ingest_puuid_queue.priority, EXCLUDED.priority)
		`, e.PUUID, platform, e.RoutingRegion, e.Priority)
	}
	// The ladder doubles as rank history for apex players: one statement
	// records every entry written above (fetched_at = now()) whose standing
	// changed since the player's last snapshot (per-game LP).
	batch.Queue(`
		INSERT INTO rank_snapshots (puuid, queue_type, tier, rank, league_points, wins, losses)
		SELECT le.puuid, 'RANKED_TFT', le.tier, le.rank, le.league_points, le.wins, le.losses
		FROM league_entries le
		LEFT JOIN LATERAL (
			SELECT tier, rank, league_points, wins, losses FROM rank_snapshots rs
			WHERE rs.puuid = le.puuid AND rs.queue_type = 'RANKED_TFT'
			ORDER BY rs.fetched_at DESC LIMIT 1
		) last ON true
		WHERE le.platform_region = $1 AND le.queue_type = 'RANKED_TFT' AND le.fetched_at = now()
			AND (last.tier, last.rank, last.league_points, last.wins, last.losses)
				IS DISTINCT FROM (le.tier, le.rank, le.league_points, le.wins, le.losses)
	`, platform)
	if complete {
		batch.Queue(`DELETE FROM league_entries WHERE platform_region = $1 AND fetched_at < now()`, platform)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LeaderboardFetchedAt returns when platform's leaderboard snapshot was last
// written, or nil if it has never been seeded.
func (s *Store) LeaderboardFetchedAt(ctx context.Context, platform string) (*time.Time, error) {
	var t *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT max(fetched_at) FROM league_entries WHERE platform_region = $1`, platform).Scan(&t)
	return t, err
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
		ORDER BY `+leaderboardOrder+`
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

// RankedBoardStats summarises one player's stored ranked games of the newest
// set that has any. Riot's league entry only has top 4 finishes and the
// rest, so the 1st place rate and average placement can only come from the
// games we've stored; Games says how many that is.
type RankedBoardStats struct {
	Games        int
	Wins         int
	Top4         int
	AvgPlacement float64
}

// RankedBoardStats returns stats for each of puuids that has stored ranked
// games (queue 1100, the newest set with ranked games); others are absent.
func (s *Store) RankedBoardStats(ctx context.Context, puuids []string) (map[string]RankedBoardStats, error) {
	out := map[string]RankedBoardStats{}
	if len(puuids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT mp.puuid, count(*), count(*) FILTER (WHERE mp.placement = 1),
		       count(*) FILTER (WHERE mp.placement <= 4), avg(mp.placement)::float8
		FROM match_participants mp
		JOIN matches m ON m.match_id = mp.match_id
		WHERE mp.puuid = ANY($1) AND m.queue_id = 1100
		  AND m.tft_set_number = (SELECT max(tft_set_number) FROM matches WHERE queue_id = 1100)
		GROUP BY mp.puuid
	`, puuids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var st RankedBoardStats
		if err := rows.Scan(&p, &st.Games, &st.Wins, &st.Top4, &st.AvgPlacement); err != nil {
			return nil, err
		}
		out[p] = st
	}
	return out, rows.Err()
}

const leaderboardOrder = `
	CASE le.tier WHEN 'CHALLENGER' THEN 0 WHEN 'GRANDMASTER' THEN 1 WHEN 'MASTER' THEN 2 ELSE 3 END,
	le.league_points DESC`

// UnresolvedLeaderboardPUUIDs returns PUUIDs among platform's top limit
// leaderboard entries that have no Riot ID yet, in leaderboard order.
func (s *Store) UnresolvedLeaderboardPUUIDs(ctx context.Context, platform string, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT puuid FROM (
			SELECT le.puuid, a.game_name
			FROM league_entries le
			JOIN accounts a ON a.puuid = le.puuid
			WHERE le.platform_region = $1
			ORDER BY `+leaderboardOrder+`
			LIMIT $2
		) top
		WHERE game_name IS NULL
	`, platform, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
