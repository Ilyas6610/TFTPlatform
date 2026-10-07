package store

import (
	"context"
	"time"
)

// LobbyRankWindow is how far from a game a rank snapshot may be and still
// stand for the player's rank in it.
const LobbyRankWindow = 14 * 24 * time.Hour

// LobbyPlayer is one participant of a stored game with their Ranked
// standing around it, when known.
type LobbyPlayer struct {
	MatchID   string
	QueueID   int
	PUUID     string
	Placement int
	// Rank is the participant's Ranked standing: the snapshot nearest the
	// game within LobbyRankWindow, else their current apex ladder entry
	// (Current set). Nil when neither exists (most players below Master
	// whose profile nobody opened).
	Rank    *RankSnapshot
	Current bool
}

// LobbyPlayers returns every participant of matchIDs with their Ranked
// standing around each game. Ranked is used for every queue: it's the
// skill measure the site has for nearly everyone it ranks, while Double Up
// standings are rarely known.
func (s *Store) LobbyPlayers(ctx context.Context, matchIDs []string) ([]LobbyPlayer, error) {
	if len(matchIDs) == 0 {
		return []LobbyPlayer{}, nil
	}
	// Two index-ordered lookups per participant (the latest snapshot
	// before the game and the earliest after it) keep this cheap; the
	// closer one wins.
	rows, err := s.Pool.Query(ctx, `
		SELECT mp.match_id, m.queue_id, mp.puuid, mp.placement,
			coalesce(near.tier, le.tier),
			CASE WHEN near.tier IS NOT NULL THEN coalesce(near.rank, '') ELSE coalesce(le.rank, '') END,
			CASE WHEN near.tier IS NOT NULL THEN near.league_points ELSE le.league_points END,
			CASE WHEN near.tier IS NOT NULL THEN near.wins ELSE le.wins END,
			CASE WHEN near.tier IS NOT NULL THEN near.losses ELSE le.losses END,
			near.tier IS NULL AND le.tier IS NOT NULL
		FROM match_participants mp
		JOIN matches m USING (match_id)
		LEFT JOIN LATERAL (
			SELECT tier, rank, league_points, wins, losses FROM (
				(SELECT tier, rank, league_points, wins, losses, m.game_datetime - fetched_at AS gap
				 FROM rank_snapshots
				 WHERE puuid = mp.puuid AND queue_type = 'RANKED_TFT'
				   AND fetched_at <= m.game_datetime AND fetched_at > m.game_datetime - make_interval(secs => $2)
				 ORDER BY fetched_at DESC LIMIT 1)
				UNION ALL
				(SELECT tier, rank, league_points, wins, losses, fetched_at - m.game_datetime AS gap
				 FROM rank_snapshots
				 WHERE puuid = mp.puuid AND queue_type = 'RANKED_TFT'
				   AND fetched_at > m.game_datetime AND fetched_at < m.game_datetime + make_interval(secs => $2)
				 ORDER BY fetched_at LIMIT 1)
			) c ORDER BY gap LIMIT 1
		) near ON true
		LEFT JOIN LATERAL (
			SELECT tier, rank, league_points, wins, losses FROM league_entries
			WHERE puuid = mp.puuid AND queue_type = 'RANKED_TFT'
			ORDER BY fetched_at DESC LIMIT 1
		) le ON true
		WHERE mp.match_id = ANY($1)
		ORDER BY mp.match_id, mp.placement`, matchIDs, LobbyRankWindow.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LobbyPlayer{}
	for rows.Next() {
		var p LobbyPlayer
		var tier, rank *string
		var lp, wins, losses *int
		if err := rows.Scan(&p.MatchID, &p.QueueID, &p.PUUID, &p.Placement, &tier, &rank, &lp, &wins, &losses, &p.Current); err != nil {
			return nil, err
		}
		if tier != nil {
			p.Rank = &RankSnapshot{QueueType: "RANKED_TFT", Tier: *tier, Rank: deref(rank), LeaguePoints: *lp, Wins: *wins, Losses: *losses}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ApexCutoffs are the LP above Master at which a platform's Grandmaster and
// Challenger tiers start. They aren't fixed (each holds a set number of the
// region's top players), so they're read from the stored ladder: the 5th
// percentile of each tier's LP, which ignores the few players who lost LP
// since Riot last reassigned tiers. Zero when the tier isn't stored.
type ApexCutoffs struct {
	Grandmaster int
	Challenger  int
}

// LadderCutoffs returns platform's current ApexCutoffs.
func (s *Store) LadderCutoffs(ctx context.Context, platform string) (ApexCutoffs, error) {
	var c ApexCutoffs
	var gm, ch *int
	err := s.Pool.QueryRow(ctx, `
		SELECT percentile_disc(0.05) WITHIN GROUP (ORDER BY league_points) FILTER (WHERE tier = 'GRANDMASTER'),
			percentile_disc(0.05) WITHIN GROUP (ORDER BY league_points) FILTER (WHERE tier = 'CHALLENGER')
		FROM league_entries
		WHERE platform_region = $1 AND queue_type = 'RANKED_TFT' AND tier IN ('GRANDMASTER', 'CHALLENGER')`, platform).Scan(&gm, &ch)
	if gm != nil {
		c.Grandmaster = *gm
	}
	if ch != nil {
		c.Challenger = *ch
	}
	return c, err
}
