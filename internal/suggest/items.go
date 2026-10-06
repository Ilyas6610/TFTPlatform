package suggest

import (
	"sort"
	"strings"

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
// combine every unit that carried it, and Units lists the main carriers.
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
