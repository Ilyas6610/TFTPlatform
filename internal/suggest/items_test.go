package suggest

import (
	"math"
	"slices"
	"testing"

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
