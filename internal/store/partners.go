package store

import (
	"context"
	"time"
)

// DoubleUpQueue is Riot's Double Up queue.
const DoubleUpQueue = 1160

// Double Up teammates are the placement pair (1-2, 3-4, 5-6, 7-8): Riot ranks
// a team's two players consecutively. Set 17 payloads carry
// partner_group_id and it always matched the pair; Set 18 dropped the field,
// but each pair still shares its elimination time. Team placement is
// (placement + 1) / 2.
const teammateJoin = `pt.match_id = mp.match_id AND pt.puuid <> mp.puuid AND (pt.placement + 1) / 2 = (mp.placement + 1) / 2`

// riotIDOf picks a player's Riot ID: the resolved one in accounts, else the
// one Riot reported in the given match's payload (accounts of players seen
// only in matches usually have no name yet). acc and m are table aliases.
func riotIDOf(acc, m, puuid string) string {
	return `CASE WHEN ` + acc + `.game_name <> '' THEN ` + acc + `.game_name ELSE (
			SELECT x->>'riotIdGameName' FROM jsonb_array_elements(` + m + `.raw_payload->'info'->'participants') x
			WHERE x->>'puuid' = ` + puuid + ` LIMIT 1) END,
		CASE WHEN ` + acc + `.game_name <> '' THEN ` + acc + `.tag_line ELSE (
			SELECT x->>'riotIdTagline' FROM jsonb_array_elements(` + m + `.raw_payload->'info'->'participants') x
			WHERE x->>'puuid' = ` + puuid + ` LIMIT 1) END`
}

// PlayerRef names a player.
type PlayerRef struct {
	PUUID    string `json:"puuid"`
	GameName string `json:"gameName,omitempty"`
	TagLine  string `json:"tagLine,omitempty"`
}

// Partner is a Double Up teammate and the team's results together.
type Partner struct {
	PlayerRef
	Games            int       `json:"games"`
	AvgTeamPlacement float64   `json:"avgTeamPlacement"` // 1-4
	Top2Rate         float64   `json:"top2Rate"`         // team top 2 (the LP-gaining half)
	WinRate          float64   `json:"winRate"`
	LastPlayed       time.Time `json:"lastPlayed"`
}

// PlayerPartners lists who a player teamed with in set's Double Up games,
// most games first (then most recent), at most limit.
func (s *Store) PlayerPartners(ctx context.Context, puuid string, set, limit int) ([]Partner, error) {
	rows, err := s.Pool.Query(ctx, `
		WITH games AS (
			SELECT m.match_id, m.game_datetime, (mp.placement + 1) / 2 AS team, pt.puuid AS partner
			FROM match_participants mp
			JOIN matches m USING (match_id)
			JOIN match_participants pt ON `+teammateJoin+`
			WHERE mp.puuid = $1 AND m.tft_set_number = $2 AND m.queue_id = $3
		), agg AS (
			SELECT partner, count(*) AS games, avg(team)::float8 AS avg_team,
				avg((team <= 2)::int)::float8 AS top2, avg((team = 1)::int)::float8 AS wins,
				max(game_datetime) AS last, (array_agg(match_id ORDER BY game_datetime DESC))[1] AS last_match
			FROM games GROUP BY partner
			ORDER BY games DESC, last DESC
			LIMIT $4
		)
		SELECT a.partner, `+riotIDOf("acc", "lm", "a.partner")+`, a.games, a.avg_team, a.top2, a.wins, a.last
		FROM agg a
		LEFT JOIN accounts acc ON acc.puuid = a.partner
		JOIN matches lm ON lm.match_id = a.last_match
		ORDER BY a.games DESC, a.last DESC`, puuid, set, DoubleUpQueue, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Partner{}
	for rows.Next() {
		var p Partner
		var name, tag *string
		if err := rows.Scan(&p.PUUID, &name, &tag, &p.Games, &p.AvgTeamPlacement, &p.Top2Rate, &p.WinRate, &p.LastPlayed); err != nil {
			return nil, err
		}
		p.GameName, p.TagLine = deref(name), deref(tag)
		out = append(out, p)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
