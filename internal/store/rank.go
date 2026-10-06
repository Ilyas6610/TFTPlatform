package store

import (
	"context"
	"time"
)

// RankSnapshot is a player's standing in one ranked queue at a moment.
type RankSnapshot struct {
	QueueType    string    `json:"queueType"`
	Tier         string    `json:"tier"`
	Rank         string    `json:"rank"`
	LeaguePoints int       `json:"leaguePoints"`
	Wins         int       `json:"wins"`
	Losses       int       `json:"losses"`
	FetchedAt    time.Time `json:"fetchedAt"`
}

// Games is how many ranked games the snapshot's record counts.
func (r RankSnapshot) Games() int { return r.Wins + r.Losses }

// AddRankSnapshot records r for puuid unless the player's latest snapshot in
// that queue already shows the same standing, so the history holds one row
// per change. It reports whether a row was added.
func (s *Store) AddRankSnapshot(ctx context.Context, puuid string, r RankSnapshot) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO rank_snapshots (puuid, queue_type, tier, rank, league_points, wins, losses)
		SELECT $1, $2, $3, $4, $5, $6, $7
		WHERE NOT EXISTS (
			SELECT 1 FROM (
				SELECT tier, rank, league_points, wins, losses FROM rank_snapshots
				WHERE puuid = $1 AND queue_type = $2
				ORDER BY fetched_at DESC LIMIT 1
			) last
			WHERE last.tier = $3 AND last.rank IS NOT DISTINCT FROM $4 AND last.league_points = $5
				AND last.wins = $6 AND last.losses = $7
		)`, puuid, r.QueueType, r.Tier, nullIfEmpty(r.Rank), r.LeaguePoints, r.Wins, r.Losses)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RankHistory returns a player's snapshots, oldest first.
func (s *Store) RankHistory(ctx context.Context, puuid string) ([]RankSnapshot, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT queue_type, tier, coalesce(rank, ''), league_points, wins, losses, fetched_at
		FROM rank_snapshots WHERE puuid = $1 ORDER BY fetched_at, id`, puuid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RankSnapshot{}
	for rows.Next() {
		var r RankSnapshot
		if err := rows.Scan(&r.QueueType, &r.Tier, &r.Rank, &r.LeaguePoints, &r.Wins, &r.Losses, &r.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TimedGame is a stored game reduced to what LP attribution and patch
// grouping need.
type TimedGame struct {
	MatchID      string
	GameDatetime time.Time
	QueueID      int
	SetNumber    int
	GameVersion  string
	Placement    int
	GameLength   time.Duration
}

// End is when the game ended (Riot's game_datetime is its start); a game
// counts toward the player's rank from then.
func (g TimedGame) End() time.Time { return g.GameDatetime.Add(g.GameLength) }

// PlayerGamesSince returns a player's stored games played after since,
// oldest first.
func (s *Store) PlayerGamesSince(ctx context.Context, puuid string, since time.Time) ([]TimedGame, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT mp.match_id, m.game_datetime, m.queue_id, m.tft_set_number, coalesce(m.game_version, ''), mp.placement,
			coalesce(m.game_length, 0)
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE mp.puuid = $1 AND m.game_datetime > $2
		ORDER BY m.game_datetime, mp.match_id`, puuid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimedGame{}
	for rows.Next() {
		var g TimedGame
		var length float64
		if err := rows.Scan(&g.MatchID, &g.GameDatetime, &g.QueueID, &g.SetNumber, &g.GameVersion, &g.Placement, &length); err != nil {
			return nil, err
		}
		g.GameLength = time.Duration(length * float64(time.Second))
		out = append(out, g)
	}
	return out, rows.Err()
}

// PatchStart is when a TFT patch went live.
type PatchStart struct {
	SetNumber int       `json:"setNumber"`
	TFTPatch  string    `json:"tftPatch"`  // "18.3"
	GamePatch string    `json:"gamePatch"` // "16.19"
	StartsAt  time.Time `json:"startsAt"`
	SourceURL string    `json:"sourceUrl"`
}

// PutPatchStart records (or corrects) when a patch went live.
func (s *Store) PutPatchStart(ctx context.Context, p PatchStart) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO patch_calendar (set_number, tft_patch, game_patch, starts_at, source_url)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (set_number, tft_patch) DO UPDATE SET
			game_patch = EXCLUDED.game_patch, starts_at = EXCLUDED.starts_at, source_url = EXCLUDED.source_url`,
		p.SetNumber, p.TFTPatch, p.GamePatch, p.StartsAt, p.SourceURL)
	return err
}

// PatchCalendar returns every known patch start, oldest first.
func (s *Store) PatchCalendar(ctx context.Context) ([]PatchStart, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT set_number, tft_patch, game_patch, starts_at, source_url
		FROM patch_calendar ORDER BY starts_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PatchStart{}
	for rows.Next() {
		var p PatchStart
		if err := rows.Scan(&p.SetNumber, &p.TFTPatch, &p.GamePatch, &p.StartsAt, &p.SourceURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
