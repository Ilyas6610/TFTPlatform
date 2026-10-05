// Package comps groups final boards into team compositions ("comps") and
// describes each by the exact board players actually run plus its flexible
// alternatives. It works on plain data so it can be tested without a
// database; internal/store loads the boards.
//
// Grouping is anchored on real boards: distinct exact unit sets are taken
// from most to least played, and each joins the comp whose anchor board it
// overlaps most (Jaccard similarity of unit sets >= SimilarityThreshold),
// or anchors a new comp. Every comp is therefore named after a board that
// was actually played, typically its most common one.
package comps

import (
	"sort"
	"strings"
)

// Board is one player's final board.
type Board struct {
	Placement int
	Units     []Unit
	Traits    []Trait // active traits only
}

type Unit struct {
	ID    string
	Star  int
	Items []string
}

type Trait struct {
	ID    string `json:"id"`
	Units int    `json:"units"` // num_units
	Tier  int    `json:"tier"`  // tier_current
}

// Stats over a set of boards.
type Stats struct {
	Boards       int     `json:"boards"`
	AvgPlacement float64 `json:"avgPlacement"`
	Top4Rate     float64 `json:"top4Rate"`
	WinRate      float64 `json:"winRate"`
}

func statsOf(boards []*Board) Stats {
	s := Stats{Boards: len(boards)}
	if len(boards) == 0 {
		return s
	}
	for _, b := range boards {
		s.AvgPlacement += float64(b.Placement)
		if b.Placement <= 4 {
			s.Top4Rate++
		}
		if b.Placement == 1 {
			s.WinRate++
		}
	}
	n := float64(len(boards))
	s.AvgPlacement /= n
	s.Top4Rate /= n
	s.WinRate /= n
	return s
}

// Comp is one team composition.
type Comp struct {
	Stats
	PlayRate float64 `json:"playRate"` // share of all boards considered
	// Board is the anchor: the exact board this comp is built around, with
	// stats over the boards that ran exactly it.
	Board      []BoardUnit `json:"board"`
	BoardStats Stats       `json:"boardStats"`
	Traits     []Trait     `json:"traits"` // active on the anchor board, by unit count
	// Variants are other exact boards in the comp, as swaps from Board.
	Variants []Variant `json:"variants"`
	// Flex are units off the anchor board that the comp often runs.
	Flex []UnitUsage `json:"flex"`
}

// BoardUnit is one unit of the anchor board as usually played.
type BoardUnit struct {
	ID    string   `json:"id"`
	Star  int      `json:"star"`  // most common star level in the comp
	Items []string `json:"items"` // most common exact item set (may be empty)
	// Frequency is the share of the comp's boards that field this unit.
	Frequency float64 `json:"frequency"`
}

// Variant is an exact board differing from the anchor.
type Variant struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
	Stats
}

// UnitUsage is a unit's share of the comp's boards and how those boards do.
type UnitUsage struct {
	ID        string  `json:"id"`
	Frequency float64 `json:"frequency"`
	Stats
}

// Options tune grouping and what's reported.
type Options struct {
	SimilarityThreshold float64 // Jaccard needed to join a comp (default 0.6)
	MinBoards           int     // comps smaller than this are dropped
	MinVariantBoards    int     // variants seen less often are dropped
	MaxVariants         int
	MinFlexFrequency    float64 // flex units below this share are dropped
	MaxFlex             int
	MinBuildShare       float64 // a unit's build is shown if this share of its copies carry it
}

func (o *Options) defaults() {
	if o.SimilarityThreshold == 0 {
		o.SimilarityThreshold = 0.6
	}
	if o.MinBoards == 0 {
		o.MinBoards = 5
	}
	if o.MinVariantBoards == 0 {
		o.MinVariantBoards = 2
	}
	if o.MaxVariants == 0 {
		o.MaxVariants = 4
	}
	if o.MinFlexFrequency == 0 {
		o.MinFlexFrequency = 0.15
	}
	if o.MaxFlex == 0 {
		o.MaxFlex = 6
	}
	if o.MinBuildShare == 0 {
		o.MinBuildShare = 0.05
	}
}

type unitSet map[string]bool

func setOf(b *Board) unitSet {
	s := unitSet{}
	for _, u := range b.Units {
		s[u.ID] = true
	}
	return s
}

func (s unitSet) key() string {
	ids := make([]string, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func jaccard(a, b unitSet) float64 {
	inter := 0
	for id := range a {
		if b[id] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

type group struct {
	set    unitSet
	boards []*Board
}

type cluster struct {
	anchor *group
	groups []*group
}

func (c *cluster) boards() []*Board {
	var out []*Board
	for _, g := range c.groups {
		out = append(out, g.boards...)
	}
	return out
}

// Build groups boards into comps, largest first.
func Build(boards []Board, opts Options) []Comp {
	opts.defaults()

	// Distinct exact unit sets, most played first (ties by key, so results
	// are deterministic).
	byKey := map[string]*group{}
	for i := range boards {
		b := &boards[i]
		if len(b.Units) == 0 {
			continue
		}
		s := setOf(b)
		k := s.key()
		g, ok := byKey[k]
		if !ok {
			g = &group{set: s}
			byKey[k] = g
		}
		g.boards = append(g.boards, b)
	}
	groups := make([]*group, 0, len(byKey))
	for _, g := range byKey {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].boards) != len(groups[j].boards) {
			return len(groups[i].boards) > len(groups[j].boards)
		}
		return groups[i].set.key() < groups[j].set.key()
	})

	var clusters []*cluster
	for _, g := range groups {
		var best *cluster
		bestSim := 0.0
		for _, c := range clusters {
			if sim := jaccard(g.set, c.anchor.set); sim >= opts.SimilarityThreshold && sim > bestSim {
				best, bestSim = c, sim
			}
		}
		if best == nil {
			clusters = append(clusters, &cluster{anchor: g, groups: []*group{g}})
		} else {
			best.groups = append(best.groups, g)
		}
	}

	total := 0
	for _, g := range groups {
		total += len(g.boards)
	}
	var out []Comp
	for _, c := range clusters {
		bs := c.boards()
		if len(bs) < opts.MinBoards {
			continue
		}
		out = append(out, describe(c, bs, total, opts))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Boards > out[j].Boards })
	return out
}

func describe(c *cluster, bs []*Board, total int, opts Options) Comp {
	comp := Comp{
		Stats:      statsOf(bs),
		PlayRate:   float64(len(bs)) / float64(max(1, total)),
		BoardStats: statsOf(c.anchor.boards),
		Variants:   []Variant{},
		Flex:       []UnitUsage{},
	}

	// Per unit: which boards field it, star levels and exact item sets.
	type usage struct {
		boards []*Board
		stars  map[int]int
		builds map[string]int
	}
	uses := map[string]*usage{}
	for _, b := range bs {
		seen := map[string]bool{}
		for _, u := range b.Units {
			us := uses[u.ID]
			if us == nil {
				us = &usage{stars: map[int]int{}, builds: map[string]int{}}
				uses[u.ID] = us
			}
			if !seen[u.ID] {
				us.boards = append(us.boards, b)
				seen[u.ID] = true
			}
			us.stars[u.Star]++
			items := append([]string(nil), u.Items...)
			sort.Strings(items)
			us.builds[strings.Join(items, ",")]++
		}
	}

	for id := range c.anchor.set {
		us := uses[id]
		bu := BoardUnit{ID: id, Items: []string{}, Frequency: float64(len(us.boards)) / float64(len(bs))}
		bu.Star = modeInt(us.stars)
		if build := commonBuild(us.builds, opts.MinBuildShare); build != "" {
			bu.Items = strings.Split(build, ",")
		}
		comp.Board = append(comp.Board, bu)
	}
	// Itemized units (carries) first, then by how core they are.
	sort.Slice(comp.Board, func(i, j int) bool {
		a, b := comp.Board[i], comp.Board[j]
		if len(a.Items) != len(b.Items) {
			return len(a.Items) > len(b.Items)
		}
		if a.Frequency != b.Frequency {
			return a.Frequency > b.Frequency
		}
		return a.ID < b.ID
	})

	// Traits as active on the anchor board (its first board is
	// representative: same units, so the same traits barring emblems).
	comp.Traits = append([]Trait{}, c.anchor.boards[0].Traits...)
	sort.Slice(comp.Traits, func(i, j int) bool {
		if comp.Traits[i].Units != comp.Traits[j].Units {
			return comp.Traits[i].Units > comp.Traits[j].Units
		}
		return comp.Traits[i].ID < comp.Traits[j].ID
	})

	for _, g := range c.groups {
		if g == c.anchor || len(g.boards) < opts.MinVariantBoards || len(comp.Variants) >= opts.MaxVariants {
			continue
		}
		v := Variant{Add: []string{}, Remove: []string{}, Stats: statsOf(g.boards)}
		for id := range g.set {
			if !c.anchor.set[id] {
				v.Add = append(v.Add, id)
			}
		}
		for id := range c.anchor.set {
			if !g.set[id] {
				v.Remove = append(v.Remove, id)
			}
		}
		sort.Strings(v.Add)
		sort.Strings(v.Remove)
		comp.Variants = append(comp.Variants, v)
	}

	for id, us := range uses {
		freq := float64(len(us.boards)) / float64(len(bs))
		if c.anchor.set[id] || freq < opts.MinFlexFrequency {
			continue
		}
		comp.Flex = append(comp.Flex, UnitUsage{ID: id, Frequency: freq, Stats: statsOf(us.boards)})
	}
	sort.Slice(comp.Flex, func(i, j int) bool {
		if comp.Flex[i].Frequency != comp.Flex[j].Frequency {
			return comp.Flex[i].Frequency > comp.Flex[j].Frequency
		}
		return comp.Flex[i].ID < comp.Flex[j].ID
	})
	if len(comp.Flex) > opts.MaxFlex {
		comp.Flex = comp.Flex[:opts.MaxFlex]
	}
	return comp
}

func modeInt(m map[int]int) int {
	best, bestN := 0, -1
	for k, n := range m {
		if n > bestN || (n == bestN && k > best) {
			best, bestN = k, n
		}
	}
	return best
}

// minBuildCopies: a build is shown only if at least this many copies of the
// unit carried it.
const minBuildCopies = 3

// commonBuild returns the most common non-empty exact item set if it was
// carried by at least minBuildCopies copies and minShare of the unit's
// copies, else "". Builds of flexible units (tanks) spread out, and "no
// items" alone would otherwise win.
func commonBuild(builds map[string]int, minShare float64) string {
	total := 0
	for _, n := range builds {
		total += n
	}
	best, bestN := "", 0
	for k, n := range builds {
		if k == "" {
			continue
		}
		// Prefer more common, then fuller builds, then a stable order.
		if n > bestN || (n == bestN && (strings.Count(k, ",") > strings.Count(best, ",") || (strings.Count(k, ",") == strings.Count(best, ",") && k < best))) {
			best, bestN = k, n
		}
	}
	// A build worth showing is repeated and not a rounding error of the
	// unit's copies.
	if total == 0 || bestN < minBuildCopies || float64(bestN)/float64(total) < minShare {
		return ""
	}
	return best
}
