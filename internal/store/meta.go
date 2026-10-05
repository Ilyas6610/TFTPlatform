package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"tft-platform/internal/comps"
)

// MetaUnit is one unit's performance and how it's built, over the boards
// in scope (a set and queues).
type MetaUnit struct {
	ID string `json:"id"`
	PlacementStats
	PickRate float64 `json:"pickRate"` // share of boards fielding it
	// Builds are exact full builds: the unit's three items as a multiset
	// (order ignored, duplicates kept), most common first.
	Builds []MetaBuild `json:"builds"`
	// Items are the items it holds most, each board counted once.
	Items []ExploreBreakdownRow `json:"items"`
}

type MetaBuild struct {
	Items []string `json:"items"` // sorted
	PlacementStats
}

type MetaResult struct {
	Boards int        `json:"boards"`
	Units  []MetaUnit `json:"units"`
}

// MetaBuilds computes per-unit stats, exact 3-item builds seen at least
// minBuildGames times (up to buildsPerUnit per unit) and the most-held items
// (up to itemsPerUnit), over boards matching scope's set/queues/levels.
func (s *Store) MetaBuilds(ctx context.Context, scope ExploreFilter, minBuildGames, buildsPerUnit, itemsPerUnit int) (*MetaResult, error) {
	scope.Units, scope.Items, scope.Traits = nil, nil, nil
	var args []any
	where := scope.whereClause(&args)
	boards := `WITH f AS (
		SELECT mp.match_id, mp.puuid, mp.placement, ` + arrayOr("mp.units") + ` AS units
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE ` + where + `)`

	res := &MetaResult{Units: []MetaUnit{}}
	if err := s.Pool.QueryRow(ctx, boards+` SELECT count(*)::int FROM f`, args...).Scan(&res.Boards); err != nil {
		return nil, fmt.Errorf("meta boards: %w", err)
	}

	// Units, each board counted once.
	unitRows, err := s.queryBreakdown(ctx, boards+` SELECT x.id, 0, `+statsCols+`
		FROM f, LATERAL (SELECT DISTINCT e->>'character_id' AS id FROM jsonb_array_elements(f.units) e) x
		GROUP BY 1, 2 ORDER BY 3 DESC, 4 ASC`, args)
	if err != nil {
		return nil, fmt.Errorf("meta units: %w", err)
	}
	byID := map[string]*MetaUnit{}
	for _, r := range unitRows {
		res.Units = append(res.Units, MetaUnit{
			ID: r.ID, PlacementStats: r.PlacementStats,
			PickRate: float64(r.Boards) / float64(max(1, res.Boards)),
			Builds:   []MetaBuild{}, Items: []ExploreBreakdownRow{},
		})
	}
	for i := range res.Units {
		byID[res.Units[i].ID] = &res.Units[i]
	}

	// Exact full builds. Copies of one unit on one board with the same
	// build count once (DISTINCT board).
	minArg := len(args) + 1
	rows, err := s.Pool.Query(ctx, boards+fmt.Sprintf(` SELECT unit, build, `+statsCols+` FROM (
			SELECT DISTINCT f.match_id, f.puuid, f.placement, e->>'character_id' AS unit,
				(SELECT array_agg(i ORDER BY i) FROM jsonb_array_elements_text(e->'itemNames') i) AS build
			FROM f, jsonb_array_elements(f.units) e
			WHERE jsonb_typeof(e->'itemNames') = 'array' AND jsonb_array_length(e->'itemNames') = 3
		) c
		GROUP BY 1, 2 HAVING count(*) >= $%d
		ORDER BY 3 DESC, 4 ASC`, minArg), append(slices.Clone(args), minBuildGames)...)
	if err != nil {
		return nil, fmt.Errorf("meta builds: %w", err)
	}
	for rows.Next() {
		var unit string
		var b MetaBuild
		if err := rows.Scan(&unit, &b.Items, &b.Boards, &b.AvgPlacement, &b.Top4Rate, &b.WinRate); err != nil {
			rows.Close()
			return nil, err
		}
		if u := byID[unit]; u != nil && len(u.Builds) < buildsPerUnit {
			u.Builds = append(u.Builds, b)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Most-held items per unit, each board counted once.
	itemRows, err := s.Pool.Query(ctx, boards+` SELECT x.unit, x.item, `+statsCols+`
		FROM f, LATERAL (SELECT DISTINCT e->>'character_id' AS unit, i AS item
			FROM jsonb_array_elements(f.units) e, jsonb_array_elements_text(e->'itemNames') i) x
		GROUP BY 1, 2 ORDER BY 3 DESC, 4 ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("meta items: %w", err)
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var unit string
		var r ExploreBreakdownRow
		if err := itemRows.Scan(&unit, &r.ID, &r.Boards, &r.AvgPlacement, &r.Top4Rate, &r.WinRate); err != nil {
			return nil, err
		}
		if u := byID[unit]; u != nil && len(u.Items) < itemsPerUnit {
			u.Items = append(u.Items, r)
		}
	}
	if err := itemRows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(res.Units, func(i, j int) bool { return res.Units[i].Boards > res.Units[j].Boards })
	return res, nil
}

// FinalBoards loads the boards in scope (set/queues; board conditions are
// ignored) whose player reached at least minLevel, for comp grouping.
func (s *Store) FinalBoards(ctx context.Context, scope ExploreFilter, minLevel int) ([]comps.Board, error) {
	scope.Units, scope.Items, scope.Traits = nil, nil, nil
	if scope.LevelMin < minLevel {
		scope.LevelMin = minLevel
	}
	var args []any
	rows, err := s.Pool.Query(ctx, `SELECT mp.placement, `+arrayOr("mp.units")+`, `+arrayOr("mp.traits")+`
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE `+scope.whereClause(&args), args...)
	if err != nil {
		return nil, fmt.Errorf("final boards: %w", err)
	}
	defer rows.Close()

	var out []comps.Board
	for rows.Next() {
		var placement int
		var unitsJSON, traitsJSON []byte
		if err := rows.Scan(&placement, &unitsJSON, &traitsJSON); err != nil {
			return nil, err
		}
		var units []struct {
			ID    string   `json:"character_id"`
			Tier  int      `json:"tier"`
			Items []string `json:"itemNames"`
		}
		var traits []struct {
			Name  string `json:"name"`
			Units int    `json:"num_units"`
			Tier  int    `json:"tier_current"`
		}
		// A malformed row is skipped rather than failing the whole page.
		if json.Unmarshal(unitsJSON, &units) != nil || json.Unmarshal(traitsJSON, &traits) != nil {
			continue
		}
		b := comps.Board{Placement: placement}
		for _, u := range units {
			b.Units = append(b.Units, comps.Unit{ID: u.ID, Star: u.Tier, Items: u.Items})
		}
		for _, t := range traits {
			if t.Tier > 0 {
				b.Traits = append(b.Traits, comps.Trait{ID: t.Name, Units: t.Units, Tier: t.Tier})
			}
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
