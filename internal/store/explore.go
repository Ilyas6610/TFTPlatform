package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ExploreFilter selects player boards (one row per player per match) for the
// stats explorer. All conditions must hold (AND).
type ExploreFilter struct {
	Set      int
	Queues   []int // empty = any queue
	LevelMin int   // 0 = no bound
	LevelMax int
	Units    []UnitCond
	Items    []string // item anywhere on the board
	Traits   []TraitCond
	// PUUID limits boards to one player's (player profile stats). It is a
	// board condition like the others: the baseline stays everyone's.
	PUUID string
}

// UnitCond matches a board fielding the unit, optionally at a minimum star
// level and holding all of Items (on that same unit).
type UnitCond struct {
	ID      string
	MinStar int
	Items   []string
}

// TraitCond matches a board with MinUnits..MaxUnits of the trait counted
// (MaxUnits 0 = no upper bound). An exact tier is a range up to the next
// breakpoint: Juggernaut tier 2 is 4-5 units.
type TraitCond struct {
	ID       string
	MinUnits int
	MaxUnits int
}

// arrayOr normalizes a JSONB column to an array: board data from Riot is
// always an array, but a malformed row (null, an object) must not make
// jsonb_array_elements fail the whole query.
func arrayOr(col string) string {
	return "(CASE jsonb_typeof(" + col + ") WHEN 'array' THEN " + col + " ELSE '[]'::jsonb END)"
}

// whereClause builds the filter as SQL over `mp` (match_participants) and
// `m` (matches), appending bind arguments to args. Every value is a bound
// parameter; nothing user-supplied is interpolated.
func (f ExploreFilter) whereClause(args *[]any) string {
	arg := func(v any) string {
		*args = append(*args, v)
		return fmt.Sprintf("$%d", len(*args))
	}
	conds := []string{"m.tft_set_number = " + arg(f.Set)}
	if len(f.Queues) > 0 {
		conds = append(conds, "m.queue_id = ANY("+arg(f.Queues)+")")
	}
	if f.LevelMin > 0 {
		conds = append(conds, "mp.level >= "+arg(f.LevelMin))
	}
	if f.LevelMax > 0 {
		conds = append(conds, "mp.level <= "+arg(f.LevelMax))
	}
	if f.PUUID != "" {
		conds = append(conds, "mp.puuid = "+arg(f.PUUID))
	}
	for _, u := range f.Units {
		// The containment test can use the GIN index on units; the EXISTS
		// then checks star level and items on that same unit.
		contains, _ := json.Marshal([]map[string]string{{"character_id": u.ID}})
		elem := []string{"e->>'character_id' = " + arg(u.ID)}
		if u.MinStar > 1 {
			elem = append(elem, "(e->>'tier')::int >= "+arg(u.MinStar))
		}
		if len(u.Items) > 0 {
			items, _ := json.Marshal(u.Items)
			elem = append(elem, "e->'itemNames' @> "+arg(string(items))+"::jsonb")
			// Containment ignores duplicates, so an item listed twice (two
			// Archangel's) also needs a count.
			counts := map[string]int{}
			for _, it := range u.Items {
				counts[it]++
			}
			for _, it := range u.Items {
				if n := counts[it]; n > 1 {
					elem = append(elem, fmt.Sprintf(
						"(SELECT count(*) FROM jsonb_array_elements_text(e->'itemNames') i WHERE i = %s) >= %s", arg(it), arg(n)))
					counts[it] = 0 // once per distinct item
				}
			}
		}
		conds = append(conds, fmt.Sprintf(
			"mp.units @> %s::jsonb AND EXISTS (SELECT 1 FROM jsonb_array_elements("+arrayOr("mp.units")+") e WHERE %s)",
			arg(string(contains)), strings.Join(elem, " AND ")))
	}
	for _, item := range f.Items {
		conds = append(conds, "EXISTS (SELECT 1 FROM jsonb_array_elements("+arrayOr("mp.units")+") e WHERE e->'itemNames' ? "+arg(item)+")")
	}
	for _, t := range f.Traits {
		min := t.MinUnits
		if min < 1 {
			min = 1
		}
		upper := ""
		if t.MaxUnits > 0 {
			upper = " AND (e->>'num_units')::int <= " + arg(t.MaxUnits)
		}
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM jsonb_array_elements("+arrayOr("mp.traits")+") e WHERE e->>'name' = %s AND (e->>'num_units')::int >= %s%s)",
			arg(t.ID), arg(min), upper))
	}
	return strings.Join(conds, " AND ")
}

// Placement statistics over a set of boards.
type PlacementStats struct {
	Boards       int     `json:"boards"`
	AvgPlacement float64 `json:"avgPlacement"`
	Top4Rate     float64 `json:"top4Rate"`
	WinRate      float64 `json:"winRate"`
}

const statsCols = `count(*)::int, coalesce(avg(placement), 0)::float8,
	coalesce(avg((placement <= 4)::int), 0)::float8, coalesce(avg((placement = 1)::int), 0)::float8`

// ExploreSummary is the filter's overall result.
type ExploreSummary struct {
	PlacementStats
	Placements [8]int `json:"placements"` // boards finishing 1st..8th
}

// ExploreBreakdownRow is one unit, item or trait tier seen on the filtered
// boards, with stats over the boards that have it.
type ExploreBreakdownRow struct {
	ID   string `json:"id"`
	Tier int    `json:"tier,omitempty"` // traits: tier_current; unit items: unused
	PlacementStats
}

// ExploreResult bundles everything the explorer shows for one filter.
type ExploreResult struct {
	Summary  ExploreSummary        `json:"summary"`
	Baseline PlacementStats        `json:"baseline"` // same set/queues/levels, no unit/item/trait conditions
	Units    []ExploreBreakdownRow `json:"units"`
	Items    []ExploreBreakdownRow `json:"items"`
	Traits   []ExploreBreakdownRow `json:"traits"`
	// UnitItems[i] is the items held by the unit of filter.Units[i] on the
	// matching boards: for each item, the boards where a copy satisfying the
	// condition holds it (each board counted once).
	UnitItems [][]ExploreBreakdownRow `json:"unitItems"`
	// UnitStars[i] is the boards by the highest star level of that unit.
	UnitStars [][]ExploreBreakdownRow `json:"unitStars"`
}

// traitTierLimit bounds the trait breakdown, which must include every
// (trait, tier) pair; far above any real set's count.
const traitTierLimit = 1000

// Explore runs filter and computes its summary, baseline and breakdowns
// (each limited to the `limit` most common entries).
func (s *Store) Explore(ctx context.Context, f ExploreFilter, limit int) (*ExploreResult, error) {
	var args []any
	where := f.whereClause(&args)
	filtered := `WITH f AS (
		SELECT mp.match_id, mp.puuid, mp.placement, ` + arrayOr("mp.units") + ` AS units, ` + arrayOr("mp.traits") + ` AS traits
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE ` + where + `)`

	res := &ExploreResult{UnitItems: [][]ExploreBreakdownRow{}, UnitStars: [][]ExploreBreakdownRow{}}

	// Summary and placement histogram.
	row := s.Pool.QueryRow(ctx, filtered+` SELECT `+statsCols+`,
		count(*) FILTER (WHERE placement = 1)::int, count(*) FILTER (WHERE placement = 2)::int,
		count(*) FILTER (WHERE placement = 3)::int, count(*) FILTER (WHERE placement = 4)::int,
		count(*) FILTER (WHERE placement = 5)::int, count(*) FILTER (WHERE placement = 6)::int,
		count(*) FILTER (WHERE placement = 7)::int, count(*) FILTER (WHERE placement = 8)::int
		FROM f`, args...)
	sm := &res.Summary
	p := &sm.Placements
	if err := row.Scan(&sm.Boards, &sm.AvgPlacement, &sm.Top4Rate, &sm.WinRate,
		&p[0], &p[1], &p[2], &p[3], &p[4], &p[5], &p[6], &p[7]); err != nil {
		return nil, fmt.Errorf("explore summary: %w", err)
	}

	// Baseline: the same scope without board conditions.
	var baseArgs []any
	base := ExploreFilter{Set: f.Set, Queues: f.Queues, LevelMin: f.LevelMin, LevelMax: f.LevelMax}
	bl := &res.Baseline
	if err := s.Pool.QueryRow(ctx, `SELECT `+statsCols+`
		FROM match_participants mp JOIN matches m USING (match_id)
		WHERE `+base.whereClause(&baseArgs), baseArgs...).Scan(&bl.Boards, &bl.AvgPlacement, &bl.Top4Rate, &bl.WinRate); err != nil {
		return nil, fmt.Errorf("explore baseline: %w", err)
	}

	limitArg := len(args) + 1
	breakdown := func(name, from, groupCols string, limit int) ([]ExploreBreakdownRow, error) {
		rows, err := s.queryBreakdown(ctx, filtered+` SELECT `+groupCols+`, `+statsCols+` FROM `+from+
			` GROUP BY 1, 2 ORDER BY 3 DESC, 4 ASC LIMIT $`+fmt.Sprint(limitArg), append(args, limit))
		if err != nil {
			return nil, fmt.Errorf("explore %s: %w", name, err)
		}
		return rows, nil
	}

	var err error
	// Units and items count each board once even if it has two copies.
	if res.Units, err = breakdown("units",
		`f, LATERAL (SELECT DISTINCT e->>'character_id' AS id FROM jsonb_array_elements(f.units) e) x`,
		`x.id, 0`, limit); err != nil {
		return nil, err
	}
	if res.Items, err = breakdown("items",
		`f, LATERAL (SELECT DISTINCT i AS id FROM jsonb_array_elements(f.units) e, jsonb_array_elements_text(e->'itemNames') i) x`,
		`x.id, 0`, limit); err != nil {
		return nil, err
	}
	// Active traits only, by the tier reached.
	if res.Traits, err = breakdown("traits",
		`f, jsonb_array_elements(f.traits) e WHERE (e->>'tier_current')::int > 0`,
		`e->>'name', (e->>'tier_current')::int`,
		// Every tier of every trait: the UI groups tiers per trait and sums
		// them, so a cut-off tier would undercount the trait. A set has only
		// ~100 (trait, tier) pairs.
		traitTierLimit); err != nil {
		return nil, err
	}

	// Per unit condition: items held and star levels, counted per unit copy
	// that satisfies the condition.
	for _, u := range f.Units {
		uArgs := append([]any{}, args...)
		match := "e->>'character_id' = $" + fmt.Sprint(len(uArgs)+1)
		uArgs = append(uArgs, u.ID)
		if u.MinStar > 1 {
			uArgs = append(uArgs, u.MinStar)
			match += " AND (e->>'tier')::int >= $" + fmt.Sprint(len(uArgs))
		}
		uArgs = append(uArgs, limit)
		lim := "$" + fmt.Sprint(len(uArgs))

		// Per board: the distinct items on matching copies, and the best
		// star level among them, so counts line up with Boards elsewhere.
		items, err := s.queryBreakdown(ctx, filtered+` SELECT x.i, 0, `+statsCols+`
			FROM f, LATERAL (SELECT DISTINCT i FROM jsonb_array_elements(f.units) e, jsonb_array_elements_text(e->'itemNames') i
				WHERE `+match+`) x
			GROUP BY 1, 2 ORDER BY 3 DESC, 4 ASC LIMIT `+lim, uArgs)
		if err != nil {
			return nil, fmt.Errorf("explore unit items: %w", err)
		}
		stars, err := s.queryBreakdown(ctx, filtered+` SELECT `+fmt.Sprintf("$%d", len(args)+1)+`::text, x.star, `+statsCols+`
			FROM f, LATERAL (SELECT max((e->>'tier')::int) AS star FROM jsonb_array_elements(f.units) e WHERE `+match+`) x
			WHERE x.star IS NOT NULL
			GROUP BY 1, 2 ORDER BY 2 ASC LIMIT `+lim, uArgs)
		if err != nil {
			return nil, fmt.Errorf("explore unit stars: %w", err)
		}
		res.UnitItems = append(res.UnitItems, items)
		res.UnitStars = append(res.UnitStars, stars)
	}
	return res, nil
}

func (s *Store) queryBreakdown(ctx context.Context, q string, args []any) ([]ExploreBreakdownRow, error) {
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExploreBreakdownRow{}
	for rows.Next() {
		var r ExploreBreakdownRow
		if err := rows.Scan(&r.ID, &r.Tier, &r.Boards, &r.AvgPlacement, &r.Top4Rate, &r.WinRate); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExploreOptions lists what occurs in a set's match data, so the explorer
// only offers conditions that can match.
type ExploreOptions struct {
	Boards int                 `json:"boards"`
	Queues []ExploreQueueCount `json:"queues"`
	Units  []ExploreIDCount    `json:"units"`
	Items  []ExploreIDCount    `json:"items"`
	Traits []ExploreTraitCount `json:"traits"`
	Levels [2]int              `json:"levels"` // min, max
}

type ExploreQueueCount struct {
	ID       int    `json:"id"`
	GameType string `json:"gameType"`
	Boards   int    `json:"boards"`
}

type ExploreIDCount struct {
	ID     string `json:"id"`
	Boards int    `json:"boards"`
}

type ExploreTraitCount struct {
	ID       string `json:"id"`
	Boards   int    `json:"boards"` // boards with the trait active at any tier
	MaxUnits int    `json:"maxUnits"`
	// Tiers are the active tiers seen (tier_current, ascending), so the UI
	// can offer each tier with its own count.
	Tiers []ExploreTierCount `json:"tiers"`
}

type ExploreTierCount struct {
	Tier   int `json:"tier"`
	Boards int `json:"boards"`
}

// ExploreOptions scans set's boards for the queues, units, items, traits and
// levels present.
func (s *Store) ExploreOptions(ctx context.Context, set int) (*ExploreOptions, error) {
	const scope = `FROM match_participants mp JOIN matches m USING (match_id) WHERE m.tft_set_number = $1`
	o := &ExploreOptions{Queues: []ExploreQueueCount{}, Units: []ExploreIDCount{}, Items: []ExploreIDCount{}, Traits: []ExploreTraitCount{}}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*)::int, coalesce(min(mp.level), 0), coalesce(max(mp.level), 0) `+scope, set).
		Scan(&o.Boards, &o.Levels[0], &o.Levels[1]); err != nil {
		return nil, err
	}

	rows, err := s.Pool.Query(ctx, `SELECT coalesce(m.queue_id, 0), coalesce(m.tft_game_type, ''), count(*)::int `+scope+` GROUP BY 1, 2 ORDER BY 3 DESC`, set)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var q ExploreQueueCount
		if err := rows.Scan(&q.ID, &q.GameType, &q.Boards); err != nil {
			rows.Close()
			return nil, err
		}
		o.Queues = append(o.Queues, q)
	}
	rows.Close()

	idCounts := func(q string) ([]ExploreIDCount, error) {
		rows, err := s.Pool.Query(ctx, q, set)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []ExploreIDCount{}
		for rows.Next() {
			var c ExploreIDCount
			if err := rows.Scan(&c.ID, &c.Boards); err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, rows.Err()
	}
	if o.Units, err = idCounts(`SELECT x.id, count(*)::int
		FROM match_participants mp JOIN matches m USING (match_id),
			LATERAL (SELECT DISTINCT e->>'character_id' AS id FROM jsonb_array_elements(` + arrayOr("mp.units") + `) e) x
		WHERE m.tft_set_number = $1 GROUP BY 1 ORDER BY 2 DESC`); err != nil {
		return nil, err
	}
	if o.Items, err = idCounts(`SELECT x.id, count(*)::int
		FROM match_participants mp JOIN matches m USING (match_id),
			LATERAL (SELECT DISTINCT i AS id FROM jsonb_array_elements(` + arrayOr("mp.units") + `) e, jsonb_array_elements_text(e->'itemNames') i) x
		WHERE m.tft_set_number = $1 GROUP BY 1 ORDER BY 2 DESC`); err != nil {
		return nil, err
	}

	// One row per (trait, tier); a board has one tier per trait, so tier
	// counts add up to the trait's total.
	rows, err = s.Pool.Query(ctx, `SELECT e->>'name', (e->>'tier_current')::int, count(*)::int, max((e->>'num_units')::int)
		FROM match_participants mp JOIN matches m USING (match_id), jsonb_array_elements(`+arrayOr("mp.traits")+`) e
		WHERE m.tft_set_number = $1 AND (e->>'tier_current')::int > 0
		GROUP BY 1, 2 ORDER BY 1, 2`, set)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*ExploreTraitCount{}
	var order []string
	for rows.Next() {
		var id string
		var tier, boards, maxUnits int
		if err := rows.Scan(&id, &tier, &boards, &maxUnits); err != nil {
			return nil, err
		}
		t, ok := byID[id]
		if !ok {
			t = &ExploreTraitCount{ID: id, Tiers: []ExploreTierCount{}}
			byID[id] = t
			order = append(order, id)
		}
		t.Boards += boards
		t.MaxUnits = max(t.MaxUnits, maxUnits)
		t.Tiers = append(t.Tiers, ExploreTierCount{Tier: tier, Boards: boards})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range order {
		o.Traits = append(o.Traits, *byID[id])
	}
	// Most common traits first, as with units and items.
	sort.SliceStable(o.Traits, func(i, j int) bool { return o.Traits[i].Boards > o.Traits[j].Boards })
	return o, rows.Err()
}

// PlacementSummary is the stats over the boards matching f.
func (s *Store) PlacementSummary(ctx context.Context, f ExploreFilter) (PlacementStats, error) {
	var args []any
	where := f.whereClause(&args)
	var p PlacementStats
	err := s.Pool.QueryRow(ctx, `SELECT `+statsCols+`
		FROM match_participants mp JOIN matches m USING (match_id) WHERE `+where, args...).
		Scan(&p.Boards, &p.AvgPlacement, &p.Top4Rate, &p.WinRate)
	return p, err
}
