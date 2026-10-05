package suggest

import (
	"slices"
	"testing"

	"tft-platform/internal/comps"
	"tft-platform/internal/store"
)

var rec = Recipes{
	"IE":  {"Sword", "Glove"},
	"JG":  {"Rod", "Glove"},
	"GS":  {"Rod", "Rod"},
	"Bow": {"Sword", "Sword"},
}

func build(avg float64, boards int, items ...string) store.MetaBuild {
	slices.Sort(items)
	return store.MetaBuild{Items: items, PlacementStats: store.PlacementStats{Boards: boards, AvgPlacement: avg}}
}

func TestCraft_UsesHeldItemsAndComponents(t *testing.T) {
	// Holds IE whole, plus Rod, Glove and Rod: can make JG and GS? JG needs
	// Rod+Glove, GS needs Rod+Rod: only one of them (3 components, Glove once).
	inv := newInventory([]string{"IE", "Rod", "Glove", "Rod"})
	steps, missing, left := craft([]string{"GS", "IE", "JG"}, inv, rec)
	if len(steps) != 2 || len(missing) != 1 {
		t.Fatalf("steps=%+v missing=%v", steps, missing)
	}
	var held bool
	for _, s := range steps {
		if s.Item == "IE" && len(s.From) == 0 {
			held = true
		}
	}
	if !held {
		t.Errorf("IE should be taken as a held item: %+v", steps)
	}
	if inv["IE"] != 1 || left["IE"] != 0 {
		t.Errorf("input must not change; leftover should lose IE: inv=%v left=%v", inv, left)
	}
}

func TestCraft_NeverSpendsAPieceTwice(t *testing.T) {
	// One Sword and one Glove: IE or nothing else; Bow needs two Swords.
	_, missing, _ := craft([]string{"IE", "Bow"}, newInventory([]string{"Sword", "Glove"}), rec)
	if !slices.Equal(missing, []string{"Bow"}) {
		t.Fatalf("missing = %v, want only Bow", missing)
	}
	// Same component twice needs two copies.
	steps, missing, _ := craft([]string{"GS"}, newInventory([]string{"Rod"}), rec)
	if len(steps) != 0 || len(missing) != 1 {
		t.Fatalf("one Rod must not make Gunblade-style Rod+Rod: %+v %v", steps, missing)
	}
}

func TestCraft_PrefersWholeItemsOverComponents(t *testing.T) {
	// Holding IE and also Sword+Glove: the held IE satisfies the build and
	// the components stay free.
	_, _, left := craft([]string{"IE"}, newInventory([]string{"IE", "Sword", "Glove"}), rec)
	if left["Sword"] != 1 || left["Glove"] != 1 {
		t.Fatalf("components were spent although IE was held: %v", left)
	}
}

func TestAdvise_RanksByReadinessThenPlacement(t *testing.T) {
	in := Input{
		Units: []string{"Akali"}, Items: []string{"IE", "JG", "Sword"}, Recipes: rec,
		Builds: map[string][]store.MetaBuild{"Akali": {
			build(2.5, 50, "IE", "JG", "GS"),  // missing GS
			build(3.5, 10, "IE", "JG", "Bow"), // Bow from Sword+Sword: only 1 Sword -> missing
			build(4.5, 8, "IE", "JG"),         // fully ready, worse placement
		}},
	}
	r := Advise(in)
	opts := r.Units[0].Options
	if len(opts) != 3 || !opts[0].Ready || len(opts[0].Missing) != 0 {
		t.Fatalf("a ready build must come first: %+v", opts)
	}
	if opts[1].AvgPlacement != 2.5 { // then by placement among those missing one
		t.Errorf("second option = %+v", opts[1])
	}
	if len(r.Plan) != 1 || !r.Plan[0].Build.Ready || r.Plan[0].Unit != "Akali" {
		t.Errorf("plan = %+v", r.Plan)
	}
}

func TestAdvise_PlanDoesNotSpendItemsTwice(t *testing.T) {
	// Two units both want IE+JG, but there is one of each.
	in := Input{
		Units: []string{"A", "B"}, Items: []string{"IE", "JG"}, Recipes: rec,
		Builds: map[string][]store.MetaBuild{
			"A": {build(3.0, 20, "IE", "JG")},
			"B": {build(3.5, 20, "IE", "JG")},
		},
	}
	r := Advise(in)
	if len(r.Plan) != 1 || r.Plan[0].Unit != "A" {
		t.Fatalf("plan = %+v, want only the better-placing unit A", r.Plan)
	}
	// Alternatives are per unit against the whole inventory, so B still lists it.
	if len(r.Units[1].Options) != 1 {
		t.Errorf("B's alternatives = %+v", r.Units[1].Options)
	}
	if len(r.Leftover) != 0 {
		t.Errorf("leftover = %v", r.Leftover)
	}
}

func TestAdvise_PlanSplitsInventoryAcrossUnits(t *testing.T) {
	in := Input{
		Units: []string{"A", "B"}, Items: []string{"IE", "JG", "GS"}, Recipes: rec,
		Builds: map[string][]store.MetaBuild{
			"A": {build(3.0, 20, "IE", "JG")},
			"B": {build(3.2, 20, "GS")},
		},
	}
	r := Advise(in)
	if len(r.Plan) != 2 || len(r.Leftover) != 0 {
		t.Fatalf("plan = %+v leftover = %v", r.Plan, r.Leftover)
	}
}

func TestAdvise_UnitsWithNothingBuildableAreLeftOut(t *testing.T) {
	in := Input{
		Units: []string{"A"}, Items: []string{"Rod"}, Recipes: rec,
		Builds: map[string][]store.MetaBuild{"A": {build(3, 10, "IE", "Bow")}},
	}
	r := Advise(in)
	if len(r.Plan) != 0 || len(r.Units[0].Options) != 0 || !slices.Equal(r.Leftover, []string{"Rod"}) {
		t.Fatalf("%+v", r)
	}
}

func comp(avg float64, boards int, units ...comps.BoardUnit) comps.Comp {
	return comps.Comp{Stats: comps.Stats{Boards: boards, AvgPlacement: avg}, Board: units}
}

func TestAdvise_CompsRankedByOwnedUnits(t *testing.T) {
	in := Input{
		Units: []string{"A", "B", "C"}, Items: []string{"Sword", "Glove"}, Recipes: rec,
		Comps: []comps.Comp{
			comp(2.0, 100, comps.BoardUnit{ID: "A"}, comps.BoardUnit{ID: "X"}, comps.BoardUnit{ID: "Y"}),                       // 1 owned: dropped
			comp(4.0, 50, comps.BoardUnit{ID: "A", Items: []string{"IE"}}, comps.BoardUnit{ID: "B"}, comps.BoardUnit{ID: "Z"}), // 2 owned
			comp(4.5, 40, comps.BoardUnit{ID: "A"}, comps.BoardUnit{ID: "B"}, comps.BoardUnit{ID: "C"}),                        // 3 owned
		},
	}
	r := Advise(in)
	if len(r.Comps) != 2 || len(r.Comps[0].Have) != 3 {
		t.Fatalf("comps = %+v", r.Comps)
	}
	second := r.Comps[1]
	if !slices.Equal(second.Need, []string{"Z"}) || len(second.Fits) != 1 || second.Fits[0].Unit != "A" || len(second.Fits[0].Missing) != 0 {
		t.Errorf("second = %+v (A's IE can be made from Sword+Glove)", second)
	}
}

func TestAdvise_SingleOwnedUnitStillMatchesComps(t *testing.T) {
	r := Advise(Input{
		Units: []string{"A"},
		Comps: []comps.Comp{comp(3, 10, comps.BoardUnit{ID: "A"}, comps.BoardUnit{ID: "B"})},
	})
	if len(r.Comps) != 1 {
		t.Fatalf("comps = %+v", r.Comps)
	}
}

func TestAdvise_EmptyInputGivesEmptyLists(t *testing.T) {
	r := Advise(Input{})
	if r.Plan == nil || r.Units == nil || r.Comps == nil || r.Leftover == nil {
		t.Fatalf("lists must be non-nil for JSON: %+v", r)
	}
}
