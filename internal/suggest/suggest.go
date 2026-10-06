// Package suggest proposes item builds and team comps from what a player
// holds right now: the units on their board and bench and the items (whole
// or still components) in their inventory. It works on plain data so it can
// be tested without a database; internal/apiserver feeds it the exact
// builds and comps computed from ingested matches (internal/store,
// internal/comps) and the set's item recipes.
package suggest

import (
	"sort"
	"strings"

	"tft-platform/internal/comps"
	"tft-platform/internal/store"
)

// Recipes maps a completed item to the two components it is built from.
type Recipes map[string][2]string

// Input is everything a suggestion is computed from.
type Input struct {
	Units   []string // owned units
	Items   []string // inventory; repeat an id for each copy
	Recipes Recipes
	// Builds are the exact 3-item builds seen on each unit in real boards.
	Builds map[string][]store.MetaBuild
	Comps  []comps.Comp
}

// Limits on what's returned.
const (
	maxOptionsPerUnit = 4
	maxComps          = 5
)

// Step is one item the player can make right now: held as is (From empty)
// or combined from two held components.
type Step struct {
	Item string   `json:"item"`
	From []string `json:"from,omitempty"`
}

// BuildOption is one real build of a unit and how much of it the inventory
// covers.
type BuildOption struct {
	Items []string `json:"items"` // the full build, sorted
	store.PlacementStats
	Steps   []Step   `json:"steps"`   // items makeable now
	Missing []string `json:"missing"` // items the inventory can't make
	Ready   bool     `json:"ready"`   // nothing missing
}

// UnitAdvice lists a unit's builds, closest to ready first.
type UnitAdvice struct {
	ID      string        `json:"id"`
	Options []BuildOption `json:"options"`
}

// PlanEntry is one unit's slot in the combined plan.
type PlanEntry struct {
	Unit  string      `json:"unit"`
	Build BuildOption `json:"build"`
	// Aim marks a target rather than a step: nothing in the inventory
	// builds toward it yet, so Build has no steps and every item missing.
	Aim bool `json:"aim"`
}

// ItemFit is what the inventory can make of the items a comp's board puts on
// one of the player's units.
type ItemFit struct {
	Unit    string   `json:"unit"`
	Items   []string `json:"items"`
	Steps   []Step   `json:"steps"`
	Missing []string `json:"missing"`
}

// CompMatch is a comp that fits the player's units.
type CompMatch struct {
	Comp comps.Comp `json:"comp"`
	Have []string   `json:"have"` // owned units on the comp's board
	Need []string   `json:"need"` // board units not owned yet
	Fits []ItemFit  `json:"fits"`
}

type Result struct {
	// Plan gives each unit one build and never spends an inventory item
	// twice, best-fitting builds first. Units the inventory can't help
	// come last, with their best-placing build marked Aim.
	Plan []PlanEntry `json:"plan"`
	// Leftover is what the plan doesn't use.
	Leftover []string `json:"leftover"`
	// Units has every owned unit's alternatives, each judged against the
	// whole inventory (so the same item may appear in several).
	Units []UnitAdvice `json:"units"`
	Comps []CompMatch  `json:"comps"`
	// Candidates is set when no units were given: the units whose real
	// builds this inventory fits best, one build each, closest to ready
	// first.
	Candidates []PlanEntry `json:"candidates"`
}

type inventory map[string]int

func newInventory(items []string) inventory {
	inv := inventory{}
	for _, it := range items {
		inv[it]++
	}
	return inv
}

func (inv inventory) clone() inventory {
	c := make(inventory, len(inv))
	for k, v := range inv {
		c[k] = v
	}
	return c
}

// craft makes as many of items as inv allows: each from a held copy or from
// two held components, never reusing a piece. Among equal outcomes it
// prefers spending whole items over components. It returns the inventory
// left over; inv itself is not modified.
func craft(items []string, inv inventory, rec Recipes) (steps []Step, missing []string, left inventory) {
	type outcome struct {
		steps   []Step
		missing []string
		left    inventory
		spent   int // components used, to break ties
	}
	var best *outcome
	var walk func(i int, cur inventory, steps []Step, missing []string, spent int)
	walk = func(i int, cur inventory, steps []Step, missing []string, spent int) {
		if i == len(items) {
			if best == nil || len(steps) > len(best.steps) || (len(steps) == len(best.steps) && spent < best.spent) {
				best = &outcome{append([]Step(nil), steps...), append([]string(nil), missing...), cur.clone(), spent}
			}
			return
		}
		it := items[i]
		if cur[it] > 0 {
			cur[it]--
			walk(i+1, cur, append(steps, Step{Item: it}), missing, spent)
			cur[it]++
		}
		if r, ok := rec[it]; ok {
			a, b := r[0], r[1]
			if cur[a] > 0 && cur[b] > 0 && (a != b || cur[a] > 1) {
				cur[a]--
				cur[b]--
				walk(i+1, cur, append(steps, Step{Item: it, From: []string{a, b}}), missing, spent+2)
				cur[a]++
				cur[b]++
			}
		}
		walk(i+1, cur, steps, append(missing, it), spent)
	}
	walk(0, inv.clone(), nil, nil, 0)
	return best.steps, best.missing, best.left
}

func option(b store.MetaBuild, inv inventory, rec Recipes) (BuildOption, inventory) {
	steps, missing, left := craft(b.Items, inv, rec)
	return BuildOption{
		Items: b.Items, PlacementStats: b.PlacementStats,
		Steps: nonNil(steps), Missing: nonNilStr(missing), Ready: len(missing) == 0,
	}, left
}

// Placement averages from a handful of boards are mostly luck, so ranking
// does two things. Samples under reliableBoards sort behind better-sampled
// ones, and averages are pulled toward the middle of the field: shrunk adds
// shrinkBoards imaginary boards that placed shrinkMean (100 boards at 3.0
// barely move; 4 boards at 1.5 land near 3.9). The real numbers are still
// what's shown.
const (
	reliableBoards = 10
	shrinkBoards   = 10
	shrinkMean     = 4.5
)

func shrunk(avg float64, boards int) float64 {
	n := float64(boards)
	return (avg*n + shrinkBoards*shrinkMean) / (n + shrinkBoards)
}

// lessSampled orders by trustworthiness then shrunk placement, for two
// results with equal standing otherwise (same number of items missing, same
// owned units).
func lessSampled(avgA float64, boardsA int, avgB float64, boardsB int) (less, decided bool) {
	if thinA, thinB := boardsA < reliableBoards, boardsB < reliableBoards; thinA != thinB {
		return !thinA, true
	}
	if sa, sb := shrunk(avgA, boardsA), shrunk(avgB, boardsB); sa != sb {
		return sa < sb, true
	}
	return false, false
}

// better orders options: closest to ready, then the best (shrunk) placement,
// then the most-played, so results are deterministic.
func better(a, b BuildOption) bool {
	if len(a.Missing) != len(b.Missing) {
		return len(a.Missing) < len(b.Missing)
	}
	if less, ok := lessSampled(a.AvgPlacement, a.Boards, b.AvgPlacement, b.Boards); ok {
		return less
	}
	return a.Boards > b.Boards
}

// Advise computes the plan, per-unit alternatives and matching comps.
func Advise(in Input) Result {
	inv := newInventory(in.Items)
	res := Result{Plan: []PlanEntry{}, Leftover: []string{}, Units: []UnitAdvice{}, Comps: []CompMatch{}, Candidates: []PlanEntry{}}

	// Per-unit alternatives against the whole inventory.
	owned := dedupe(in.Units)
	for _, u := range owned {
		adv := UnitAdvice{ID: u, Options: []BuildOption{}}
		// Builds the inventory can't help with are still listed, after the
		// ones it can: a unit with nothing to build yet shows what to aim
		// for rather than nothing.
		for _, b := range in.Builds[u] {
			o, _ := option(b, inv, in.Recipes)
			adv.Options = append(adv.Options, o)
		}
		sort.SliceStable(adv.Options, func(i, j int) bool { return better(adv.Options[i], adv.Options[j]) })
		if len(adv.Options) > maxOptionsPerUnit {
			adv.Options = adv.Options[:maxOptionsPerUnit]
		}
		res.Units = append(res.Units, adv)
	}

	// Combined plan: repeatedly take the best (unit, build) the remaining
	// inventory still supports.
	left := inv.clone()
	remaining := append([]string(nil), owned...)
	for len(remaining) > 0 {
		var (
			bestOpt  BuildOption
			bestLeft inventory
			bestIdx  = -1
		)
		for i, u := range remaining {
			for _, b := range in.Builds[u] {
				o, after := option(b, left, in.Recipes)
				if len(o.Steps) == 0 {
					continue
				}
				if bestIdx < 0 || better(o, bestOpt) {
					bestOpt, bestLeft, bestIdx = o, after, i
				}
			}
		}
		if bestIdx < 0 {
			break
		}
		res.Plan = append(res.Plan, PlanEntry{Unit: remaining[bestIdx], Build: bestOpt})
		left = bestLeft
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}
	// Units nothing fits yet get the build that places best, to aim for.
	for _, u := range remaining {
		var best *BuildOption
		for _, b := range in.Builds[u] {
			o, _ := option(b, left, in.Recipes)
			if best == nil || better(o, *best) {
				o := o
				best = &o
			}
		}
		if best != nil {
			res.Plan = append(res.Plan, PlanEntry{Unit: u, Build: *best, Aim: true})
		}
	}
	for it, n := range left {
		for ; n > 0; n-- {
			res.Leftover = append(res.Leftover, it)
		}
	}
	sort.Strings(res.Leftover)

	if len(owned) == 0 && len(in.Items) > 0 {
		res.Candidates = candidates(in, inv)
	}

	res.Comps = matchComps(in, owned, inv)
	return res
}

func matchComps(in Input, owned []string, inv inventory) []CompMatch {
	if len(owned) == 0 {
		return []CompMatch{}
	}
	own := map[string]bool{}
	for _, u := range owned {
		own[u] = true
	}
	// Enough overlap to be about the player's board rather than one stray
	// unit; with a single owned unit, that one is enough.
	minHave := min(2, len(owned))

	var out []CompMatch
	for _, c := range in.Comps {
		m := CompMatch{Comp: c, Have: []string{}, Need: []string{}, Fits: []ItemFit{}}
		for _, bu := range c.Board {
			if !own[bu.ID] {
				m.Need = append(m.Need, bu.ID)
				continue
			}
			m.Have = append(m.Have, bu.ID)
			if len(bu.Items) > 0 {
				steps, missing, _ := craft(bu.Items, inv, in.Recipes)
				m.Fits = append(m.Fits, ItemFit{Unit: bu.ID, Items: bu.Items, Steps: nonNil(steps), Missing: nonNilStr(missing)})
			}
		}
		if len(m.Have) >= minHave {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if len(a.Have) != len(b.Have) {
			return len(a.Have) > len(b.Have)
		}
		if less, ok := lessSampled(a.Comp.AvgPlacement, a.Comp.Boards, b.Comp.AvgPlacement, b.Comp.Boards); ok {
			return less
		}
		return a.Comp.Boards > b.Comp.Boards
	})
	// Variants of one comp are anchored separately and can overlap the
	// player's units identically; keep the best of each so the slots show
	// different directions.
	seen := map[string]bool{}
	kept := []CompMatch{}
	for _, m := range out {
		have := append([]string(nil), m.Have...)
		sort.Strings(have)
		key := strings.Join(have, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, m)
		if len(kept) == maxComps {
			break
		}
	}
	return kept
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func nonNil(s []Step) []Step {
	if s == nil {
		return []Step{}
	}
	return s
}

func nonNilStr(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// maxCandidates bounds the units suggested for an inventory.
const maxCandidates = 8

// candidates ranks every unit by how well its best-fitting real build uses
// the inventory; units with nothing makeable are left out.
func candidates(in Input, inv inventory) []PlanEntry {
	out := []PlanEntry{}
	for unit, builds := range in.Builds {
		var best *BuildOption
		for _, b := range builds {
			o, _ := option(b, inv, in.Recipes)
			if len(o.Steps) == 0 {
				continue
			}
			if best == nil || better(o, *best) {
				o := o
				best = &o
			}
		}
		if best != nil {
			out = append(out, PlanEntry{Unit: unit, Build: *best})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Build, out[j].Build
		if better(a, b) {
			return true
		}
		if better(b, a) {
			return false
		}
		return out[i].Unit < out[j].Unit // deterministic
	})
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out
}
