package suggest

import (
	"math"
	"slices"
	"testing"

	"tft-platform/internal/comps"
	"tft-platform/internal/store"
)

// Items-only: the same build carried by two units is one entry with
// combined stats, listing both carriers; builds the inventory can't help
// are left out.
func TestAdvise_ItemsOnlyBuildsCombineCarriers(t *testing.T) {
	in := Input{
		Items:   []string{"Sword", "Glove", "Rod"},
		Recipes: rec,
		Builds: map[string][]store.MetaBuild{
			"A": {build(3.0, 20, "IE", "JG", "GS")},
			"B": {build(4.0, 20, "IE", "JG", "GS"), build(2.0, 50, "Bow", "Bow", "Bow")},
		},
	}
	r := Advise(in)
	if len(r.Builds) != 1 {
		t.Fatalf("builds = %+v, want only the IE/JG/GS build (no Bow can be made)", r.Builds)
	}
	b := r.Builds[0]
	if b.Boards != 40 || math.Abs(b.AvgPlacement-3.5) > 1e-9 {
		t.Errorf("combined stats = %d boards at %.2f, want 40 at 3.50", b.Boards, b.AvgPlacement)
	}
	if len(b.Units) != 2 || len(b.Steps) != 1 || len(b.Missing) != 2 {
		t.Errorf("build = %+v, want both carriers and one makeable item (IE or JG from Sword/Glove/Rod)", b)
	}
	if len(r.Plan) != 0 || len(r.Comps) != 0 {
		t.Errorf("unit sections should stay empty without units")
	}
}

func TestAdvise_ItemsOnlyBuildsRankReadyFirst(t *testing.T) {
	in := Input{
		Items:   []string{"IE", "JG"},
		Recipes: rec,
		Builds: map[string][]store.MetaBuild{
			"A": {build(2.0, 100, "IE", "GS", "GS")}, // better placement, 2 missing
			"B": {build(4.0, 100, "IE", "JG", "GS")}, // 1 missing
		},
	}
	r := Advise(in)
	if len(r.Builds) != 2 || !slices.Equal(r.Builds[0].Items, []string{"GS", "IE", "JG"}) {
		t.Fatalf("builds = %+v, want the closer build first", r.Builds)
	}
}

// Crafts: held completed items and everything two held components make,
// each piece counted (Rod+Rod needs two Rods); stats come from the builds
// that include the item.
func TestAdvise_ItemsOnlyCrafts(t *testing.T) {
	in := Input{
		Items:   []string{"Sword", "Glove", "Rod", "IE"},
		Recipes: rec,
		Builds: map[string][]store.MetaBuild{
			"A": {build(3.0, 30, "IE", "IE", "JG")},
			"B": {build(5.0, 10, "JG", "Bow", "Bow")},
		},
	}
	r := Advise(in)
	got := map[string]Craft{}
	for _, c := range r.Crafts {
		got[c.Item] = c
	}
	if _, ok := got["GS"]; ok {
		t.Error("GS needs two Rods; only one is held")
	}
	if _, ok := got["Bow"]; ok {
		t.Error("Bow needs two Swords; only one is held")
	}
	ie, jg := got["IE"], got["JG"]
	if ie.From != nil || ie.Boards != 30 {
		t.Errorf("IE = %+v, want held whole, with the one build including it (IE twice counts once)", ie)
	}
	if !slices.Equal(jg.From, []string{"Rod", "Glove"}) || jg.Boards != 40 || len(jg.Units) != 2 {
		t.Errorf("JG = %+v, want Rod+Glove with stats from both builds", jg)
	}
	if len(r.Crafts) != 2 || r.Crafts[0].Item != "IE" {
		t.Errorf("crafts = %+v, want IE (3.0 avg) before JG (3.5)", r.Crafts)
	}
}

func TestAdvise_UnitsGivenSkipsItemsOnlySections(t *testing.T) {
	r := Advise(Input{
		Units: []string{"A"}, Items: []string{"Sword", "Glove"}, Recipes: rec,
		Builds: map[string][]store.MetaBuild{"A": {build(3.0, 20, "IE", "JG", "GS")}},
	})
	if len(r.Builds) != 0 || len(r.Crafts) != 0 || len(r.Candidates) != 0 {
		t.Errorf("items-only sections set with units: %+v", r)
	}
}

// Items only: final boards are ranked by how many of their carries' items
// the inventory makes, serving carries from one shared inventory.
func TestAdvise_ItemsOnlyFinalBoards(t *testing.T) {
	carry := func(id string, items ...string) comps.BoardUnit {
		return comps.BoardUnit{ID: id, Items: items}
	}
	in := Input{
		Items:   []string{"Sword", "Glove", "IE"},
		Recipes: rec,
		Comps: []comps.Comp{
			// Two carries both want IE: one held IE plus Sword+Glove make two.
			comp(4.0, 50, carry("A", "IE", "GS", "GS"), carry("B", "IE", "Bow", "Bow"), comps.BoardUnit{ID: "T"}),
			// Better placement but only one IE makeable for its single carry.
			comp(3.0, 50, carry("C", "IE", "JG", "JG"), comps.BoardUnit{ID: "T"}),
			// Nothing makeable: dropped.
			comp(2.0, 50, carry("D", "GS", "GS", "GS")),
		},
	}
	r := Advise(in)
	if len(r.Comps) != 2 {
		t.Fatalf("comps = %+v, want the two the items help", r.Comps)
	}
	first := r.Comps[0]
	if len(first.Fits) != 2 || len(first.Fits[0].Steps) != 1 || len(first.Fits[1].Steps) != 1 {
		t.Fatalf("first = %+v, want IE for A and IE for B", first)
	}
	if first.Fits[0].Steps[0].From != nil || first.Fits[1].Steps[0].From == nil {
		t.Errorf("A should take the held IE and B the Sword+Glove one, not both the same piece: %+v", first.Fits)
	}
	if len(first.Have) != 0 || len(first.Need) != 3 {
		t.Errorf("have/need = %v/%v, want nothing owned (no units given)", first.Have, first.Need)
	}
	if r.Comps[1].Comp.AvgPlacement != 3.0 {
		t.Errorf("second = %+v, want the one-IE comp", r.Comps[1])
	}
}
