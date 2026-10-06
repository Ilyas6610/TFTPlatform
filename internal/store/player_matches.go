package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

type PlayerMatchSummary struct {
	MatchID      string
	GameDatetime time.Time
	TFTSetNumber int
	Placement    int
	Level        int
}

// PlayerMatch is one game in a player's history with their final board.
type PlayerMatch struct {
	MatchID      string        `json:"matchId"`
	GameDatetime time.Time     `json:"gameDatetime"`
	TFTSetNumber int           `json:"tftSetNumber"`
	QueueID      int           `json:"queueId"`
	GameVersion  string        `json:"-"` // for the patch, when Riot reports one
	Placement    int           `json:"placement"`
	Level        int           `json:"level"`
	Units        []BoardUnit   `json:"units"`
	Traits       []ActiveTrait `json:"traits"` // active only, most units first
	// Double Up only: the teammate and the team's placement (1-4).
	Partner *PlayerRef `json:"partner,omitempty"`
	Team    int        `json:"team,omitempty"`
}

type BoardUnit struct {
	ID    string   `json:"id"`
	Star  int      `json:"star"`
	Items []string `json:"items"`
}

type ActiveTrait struct {
	ID    string `json:"id"`
	Units int    `json:"units"`
	Tier  int    `json:"tier"`
	Style int    `json:"style"` // Riot's trait style: 1 bronze .. 4+ chromatic/unique
}

// PlayerMatches returns one page of a player's stored games, newest first:
// offset games are skipped, then up to limit returned.
func (s *Store) PlayerMatches(ctx context.Context, puuid string, offset, limit int) ([]PlayerMatch, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT mp.match_id, m.game_datetime, m.tft_set_number, m.queue_id, coalesce(m.game_version, ''), mp.placement, coalesce(mp.level, 0),
			`+arrayOr("mp.units")+`, `+arrayOr("mp.traits")+`,
			pt.puuid, `+riotIDOf("acc", "m", "pt.puuid")+`
		FROM match_participants mp
		JOIN matches m USING (match_id)
		LEFT JOIN match_participants pt ON m.queue_id = $4 AND `+teammateJoin+`
		LEFT JOIN accounts acc ON acc.puuid = pt.puuid
		WHERE mp.puuid = $1
		ORDER BY m.game_datetime DESC, mp.match_id DESC
		OFFSET $2 LIMIT $3
	`, puuid, offset, limit, DoubleUpQueue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PlayerMatch{}
	for rows.Next() {
		var m PlayerMatch
		var unitsJSON, traitsJSON []byte
		var partner, name, tag *string
		if err := rows.Scan(&m.MatchID, &m.GameDatetime, &m.TFTSetNumber, &m.QueueID, &m.GameVersion, &m.Placement, &m.Level, &unitsJSON, &traitsJSON,
			&partner, &name, &tag); err != nil {
			return nil, err
		}
		m.Units, m.Traits = parseBoard(unitsJSON, traitsJSON)
		if partner != nil {
			m.Partner = &PlayerRef{PUUID: *partner, GameName: deref(name), TagLine: deref(tag)}
			m.Team = (m.Placement + 1) / 2
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// parseBoard decodes stored units and traits. A malformed column yields an
// empty list rather than failing the page.
func parseBoard(unitsJSON, traitsJSON []byte) ([]BoardUnit, []ActiveTrait) {
	var units []struct {
		ID    string   `json:"character_id"`
		Tier  int      `json:"tier"`
		Items []string `json:"itemNames"`
	}
	var traits []struct {
		Name  string `json:"name"`
		Units int    `json:"num_units"`
		Tier  int    `json:"tier_current"`
		Style int    `json:"style"`
	}
	_ = json.Unmarshal(unitsJSON, &units)
	_ = json.Unmarshal(traitsJSON, &traits)

	outUnits := make([]BoardUnit, 0, len(units))
	for _, u := range units {
		items := u.Items
		if items == nil {
			items = []string{}
		}
		outUnits = append(outUnits, BoardUnit{ID: u.ID, Star: u.Tier, Items: items})
	}
	outTraits := []ActiveTrait{}
	for _, t := range traits {
		if t.Tier > 0 {
			outTraits = append(outTraits, ActiveTrait{ID: t.Name, Units: t.Units, Tier: t.Tier, Style: t.Style})
		}
	}
	sort.SliceStable(outTraits, func(i, j int) bool {
		if outTraits[i].Style != outTraits[j].Style {
			return outTraits[i].Style > outTraits[j].Style
		}
		return outTraits[i].Units > outTraits[j].Units
	})
	return outUnits, outTraits
}

// PlayerQueueStats is a player's results in one queue.
type PlayerQueueStats struct {
	QueueID int `json:"queueId"`
	PlacementStats
}

// PlayerQueues splits a player's games in set by queue, most played first.
func (s *Store) PlayerQueues(ctx context.Context, puuid string, set int) ([]PlayerQueueStats, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT m.queue_id, `+statsCols+`
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE mp.puuid = $1 AND m.tft_set_number = $2
		GROUP BY 1 ORDER BY 2 DESC, 1`, puuid, set)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlayerQueueStats{}
	for rows.Next() {
		var q PlayerQueueStats
		if err := rows.Scan(&q.QueueID, &q.Boards, &q.AvgPlacement, &q.Top4Rate, &q.WinRate); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// PlayerSets lists the sets a player has stored games in, newest first.
func (s *Store) PlayerSets(ctx context.Context, puuid string) ([]int, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT m.tft_set_number
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE mp.puuid = $1 ORDER BY 1 DESC`, puuid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var set int
		if err := rows.Scan(&set); err != nil {
			return nil, err
		}
		out = append(out, set)
	}
	return out, rows.Err()
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
