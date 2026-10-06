package suggest

import (
	"slices"
	"sort"
	"strings"

	"tft-platform/internal/comps"
	"tft-platform/internal/store"
)

// Items-only advice: with no units given, the inventory itself is the
// question. Besides Candidates (units the items fit), Advise then returns
// Builds (real builds by their items, whoever carried them) and Crafts
// (what the held components make right now).

// Limits on items-only results.
const (
	maxItemBuilds  = 8
	maxCrafts      = 10
	maxUnitsPerUse = 3
)

// UnitUse is a unit that carried a build or item, and how it did.
type UnitUse struct {
	ID string `json:"id"`
	store.PlacementStats
}

// ItemBuild is a real 3-item build judged by its items alone: its stats
// combine every unit that carried it (per carrier, so a board where two
// units ran the same build counts twice; rare), and Units lists the main
// carriers.
type ItemBuild struct {
	BuildOption
	Units []UnitUse `json:"units"`
}

// Craft is a completed item the inventory holds or can make now. From is
// the two held components it takes (empty when held whole). Stats combine
// the real builds that include it, so an item seen in no build has none.
type Craft struct {
	Item string   `json:"item"`
	From []string `json:"from,omitempty"`
	store.PlacementStats
	Units []UnitUse `json:"units"`
}

// statsSum accumulates PlacementStats weighted by boards.
type statsSum struct {
	boards            int
	place, top4, wins float64
	units             map[string]*statsSum
}

func (s *statsSum) add(unit string, p store.PlacementStats) {
	n := float64(p.Boards)
	s.boards += p.Boards
	s.place += p.AvgPlacement * n
	s.top4 += p.Top4Rate * n
	s.wins += p.WinRate * n
	if unit == "" {
		return
	}
	if s.units == nil {
		s.units = map[string]*statsSum{}
	}
	u := s.units[unit]
	if u == nil {
		u = &statsSum{}
		s.units[unit] = u
	}
	u.add("", p)
}

func (s *statsSum) stats() store.PlacementStats {
	if s.boards == 0 {
		return store.PlacementStats{}
	}
	n := float64(s.boards)
	return store.PlacementStats{Boards: s.boards, AvgPlacement: s.place / n, Top4Rate: s.top4 / n, WinRate: s.wins / n}
}

// topUnits lists the carriers with the most boards.
func (s *statsSum) topUnits() []UnitUse {
	out := []UnitUse{}
	for id, u := range s.units {
		out = append(out, UnitUse{ID: id, PlacementStats: u.stats()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Boards != out[j].Boards {
			return out[i].Boards > out[j].Boards
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > maxUnitsPerUse {
		out = out[:maxUnitsPerUse]
	}
	return out
}

// itemBuilds groups every unit's builds by their items and ranks them like
// unit builds (closest to ready, then sample-aware placement). Builds the
// inventory contributes nothing to are left out.
func itemBuilds(in Input, inv inventory) []ItemBuild {
	sums := map[string]*statsSum{}
	items := map[string][]string{}
	for unit, builds := range in.Builds {
		for _, b := range builds {
			key := strings.Join(b.Items, ",")
			if sums[key] == nil {
				sums[key] = &statsSum{}
				items[key] = b.Items
			}
			sums[key].add(unit, b.PlacementStats)
		}
	}

	out := []ItemBuild{}
	for key, s := range sums {
		o, _ := option(store.MetaBuild{Items: items[key], PlacementStats: s.stats()}, inv, in.Recipes)
		if len(o.Steps) == 0 {
			continue
		}
		out = append(out, ItemBuild{BuildOption: o, Units: s.topUnits()})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].BuildOption, out[j].BuildOption
		if better(a, b) {
			return true
		}
		if better(b, a) {
			return false
		}
		return strings.Join(a.Items, ",") < strings.Join(b.Items, ",") // deterministic
	})
	if len(out) > maxItemBuilds {
		out = out[:maxItemBuilds]
	}
	return out
}

// crafts lists the completed items the inventory holds or can make from
// two held components, with how the real builds that include them did.
func crafts(in Input, inv inventory) []Craft {
	// Stats per item over every build that includes it (once per build,
	// even if the build holds two copies).
	perItem := map[string]*statsSum{}
	for unit, builds := range in.Builds {
		for _, b := range builds {
			seen := map[string]bool{}
			for _, it := range b.Items {
				if seen[it] {
					continue
				}
				seen[it] = true
				if perItem[it] == nil {
					perItem[it] = &statsSum{}
				}
				perItem[it].add(unit, b.PlacementStats)
			}
		}
	}

	made := map[string]Craft{}
	add := func(c Craft) {
		if _, dup := made[c.Item]; dup {
			return
		}
		if s := perItem[c.Item]; s != nil {
			c.PlacementStats, c.Units = s.stats(), s.topUnits()
		} else {
			c.Units = []UnitUse{}
		}
		made[c.Item] = c
	}
	// Held whole: completed items (anything with a recipe or seen in a build).
	for it := range inv {
		if _, isRecipe := in.Recipes[it]; isRecipe || perItem[it] != nil {
			add(Craft{Item: it})
		}
	}
	for it, r := range in.Recipes {
		a, b := r[0], r[1]
		if inv[a] > 0 && inv[b] > 0 && (a != b || inv[a] > 1) {
			add(Craft{Item: it, From: []string{a, b}})
		}
	}

	out := make([]Craft, 0, len(made))
	for _, c := range made {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Boards == 0) != (b.Boards == 0) {
			return b.Boards == 0 // items no build uses go last
		}
		if less, ok := lessSampled(a.AvgPlacement, a.Boards, b.AvgPlacement, b.Boards); ok {
			return less
		}
		return a.Item < b.Item
	})
	if len(out) > maxCrafts {
		out = out[:maxCrafts]
	}
	return out
}

// itemComps ranks comps (final boards) by how much of their carries' items
// the inventory makes. The anchor board's itemized units are served in
// board order (carries first) from one shared inventory, so no piece is
// counted for two carries. Comps the inventory contributes nothing to are
// left out; comps whose item advice is identical keep only the best one.
func itemComps(in Input, inv inventory) []CompMatch {
	type scored struct {
		m             CompMatch
		made, missing int
		enabled       int // units given a build because of a held emblem or artifact
		traitUnits    int // units in the traits the emblems add to
	}
	var all []scored
	for _, c := range in.Comps {
		m := CompMatch{Comp: c, Have: []string{}, Need: []string{}, Fits: []ItemFit{}, Emblems: []EmblemFit{}}
		left := inv.clone()
		fits := map[string]ItemFit{}

		// Emblems and artifacts first: they are the reason to build a
		// particular unit, so a board unit with a real build using one
		// takes that build (Master Yi with a Brawler Emblem).
		for _, bu := range c.Board {
			if f, after, ok := enablerFit(in, bu, left); ok {
				fits[bu.ID], left = f, after
			}
		}
		// Then each itemized unit's usual build from what's left.
		for _, bu := range c.Board {
			m.Need = append(m.Need, bu.ID)
			if _, done := fits[bu.ID]; done || len(bu.Items) == 0 {
				continue
			}
			steps, miss, after := craft(bu.Items, left, in.Recipes)
			fits[bu.ID], left = ItemFit{Unit: bu.ID, Items: bu.Items, Steps: nonNil(steps), Missing: nonNilStr(miss)}, after
		}
		// Finally emblems for traits the board plays.
		m.Emblems, left = emblemFits(in, c, left)

		var made, missing int
		for _, bu := range c.Board {
			if f, ok := fits[bu.ID]; ok {
				m.Fits = append(m.Fits, f)
				made += len(f.Steps)
				missing += len(f.Missing)
			}
		}
		made += len(m.Emblems)
		var enabled, traitUnits int
		for _, f := range m.Fits {
			if len(f.Enablers) > 0 {
				enabled++
			}
		}
		for _, e := range m.Emblems {
			for _, t := range c.Traits {
				if t.ID == e.Trait {
					traitUnits += t.Units
				}
			}
		}
		if made > 0 {
			all = append(all, scored{m, made, missing, enabled, traitUnits})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.made != b.made {
			return a.made > b.made
		}
		// An emblem or artifact that is the reason to build a unit on the
		// board beats one that only adds to a trait, and a trait the board
		// invests in beats one it merely touches.
		if a.enabled != b.enabled {
			return a.enabled > b.enabled
		}
		if a.traitUnits != b.traitUnits {
			return a.traitUnits > b.traitUnits
		}
		if a.missing != b.missing {
			return a.missing < b.missing
		}
		if less, ok := lessSampled(a.m.Comp.AvgPlacement, a.m.Comp.Boards, b.m.Comp.AvgPlacement, b.m.Comp.Boards); ok {
			return less
		}
		return a.m.Comp.Boards > b.m.Comp.Boards
	})

	out := []CompMatch{}
	seen := map[string]bool{}
	for _, s := range all {
		var key []string
		for _, f := range s.m.Fits {
			key = append(key, f.Unit+":"+strings.Join(f.Items, ","))
		}
		if len(key) == 0 {
			// No carry advice (only an emblem for a trait): the boards
			// themselves are what differ.
			key = s.m.Board()
		}
		k := strings.Join(key, ";")
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s.m)
		if len(out) == maxComps {
			break
		}
	}
	return out
}

// enablerFit picks, for board unit bu, its best real build that uses a held
// (or makeable) emblem or artifact, crafted from left. ok is false if no
// build of the unit uses one.
func enablerFit(in Input, bu comps.BoardUnit, left inventory) (fit ItemFit, after inventory, ok bool) {
	var best *BuildOption
	var used []string
	for _, b := range in.Builds[bu.ID] {
		o, rest := option(b, left, in.Recipes)
		var enablers []string
		for _, st := range o.Steps {
			if in.Enablers[st.Item] {
				enablers = append(enablers, st.Item)
			}
		}
		if len(enablers) == 0 {
			continue
		}
		if best == nil || better(o, *best) {
			o := o
			best, after, used = &o, rest, enablers
		}
	}
	if best == nil {
		return ItemFit{}, left, false
	}
	return ItemFit{
		Unit: bu.ID, Items: best.Items, Steps: best.Steps, Missing: best.Missing,
		Alt: !slices.Equal(best.Items, bu.Items), Enablers: used,
	}, after, true
}

// emblemFits uses the emblems left (held or makeable) whose trait board c
// plays, one copy per emblem.
func emblemFits(in Input, c comps.Comp, left inventory) ([]EmblemFit, inventory) {
	plays := map[string]bool{}
	for _, t := range c.Traits {
		plays[t.ID] = true
	}
	emblems := make([]string, 0, len(in.EmblemTraits))
	for e, trait := range in.EmblemTraits {
		if plays[trait] {
			emblems = append(emblems, e)
		}
	}
	sort.Strings(emblems)
	out := []EmblemFit{}
	for _, e := range emblems {
		steps, _, after := craft([]string{e}, left, in.Recipes)
		if len(steps) == 1 {
			out = append(out, EmblemFit{Item: e, Trait: in.EmblemTraits[e], From: steps[0].From})
			left = after
		}
	}
	return out, left
}

// Board lists the comp's anchor board unit ids (handy in tests and logs).
func (m CompMatch) Board() []string {
	ids := make([]string, len(m.Comp.Board))
	for i, bu := range m.Comp.Board {
		ids[i] = bu.ID
	}
	return ids
}
