package setdata

import (
	"slices"
	"testing"
)

func TestRender(t *testing.T) {
	values := map[string]float64{"Damage": 120, "Ratio": 0.25}
	cases := map[string]string{
		"Deal @Damage@ damage":                    "Deal 120 damage",
		"Gain @Ratio*100@% speed":                 "Gain 25% speed",
		"Deal @damage@ (case-insensitive)":        "Deal 120 (case-insensitive)",
		"Deal @Unknown@ damage":                   "Deal ? damage",
		"Deal 5 %i:scaleHealth%%i:scaleAP% magic": "Deal 5 (HP, AP) magic",
		"Gold %i:someUnknownIcon% here":           "Gold here",
		"<Bright>Bold</Bright> text":              "Bold text",
		`Line one<br>Line two\nLine three`:        "Line one\nLine two\nLine three",
		"A<br><br><br><br>B":                      "A\n\nB",
	}
	for tmpl, want := range cases {
		if got := render(tmpl, lookupIn(values)); got != want {
			t.Errorf("render(%q) = %q, want %q", tmpl, got, want)
		}
	}
}

func TestVarHash_MatchesCommunityDragonKeys(t *testing.T) {
	// Observed in the real export for Elderwood's 5-unit breakpoint.
	if got := varHash("StonebarkTreeBonusHealth"); got != "{b027c2f9}" {
		t.Errorf("got %s", got)
	}
}

func TestNamedValues_ResolvesHashesRoundsAndDropsNulls(t *testing.T) {
	v1, v2 := 0.15000000596046448, 7.0
	got := namedValues(map[string]*float64{
		varHash("BonusHealth"): &v1,
		"{deadbeef}":           &v2,
		"Missing":              nil,
	}, "Gain @BonusHealth@")
	if got["BonusHealth"] != 0.15 || got["{deadbeef}"] != 7 || len(got) != 2 {
		t.Errorf("unexpected values: %v", got)
	}
}

func TestExtract_Units(t *testing.T) {
	d := mustExtract(t, defaultFixture(), "16.19.1")

	if len(d.Units) != 2 || d.Units[0].Name != "Akali" || d.Units[1].Name != "Ashe" {
		t.Fatalf("expected only shop units sorted by cost, got %+v", d.Units)
	}
	akali := d.Units[0]
	if akali.Stats["attackSpeed"] != 0.75 || akali.Stats["mana"] != 30 {
		t.Errorf("unexpected stats: %v", akali.Stats)
	}
	if _, ok := akali.Stats["armor"]; ok {
		t.Error("null stats should be dropped")
	}
	if want := "Deal 100/150/225 (AD, AP) damage and ? more."; akali.Ability.Desc != want {
		t.Errorf("ability desc = %q, want %q", akali.Ability.Desc, want)
	}
	if want := "https://raw.communitydragon.org/16.19/game/assets/characters/da_18_akali_square.png"; akali.Icon != want {
		t.Errorf("icon = %q, want %q", akali.Icon, want)
	}
}

func TestExtract_TraitBreakpoints(t *testing.T) {
	d := mustExtract(t, defaultFixture(), "16.19.1")

	if len(d.Traits) != 1 {
		t.Fatalf("expected 1 trait from the standard mode, got %d", len(d.Traits))
	}
	tr := d.Traits[0]
	if tr.Desc != "Gain plants." || len(tr.Breakpoints) != 2 {
		t.Fatalf("unexpected trait: %+v", tr)
	}
	if tr.Breakpoints[0].Text != "(3) A tree" {
		t.Errorf("breakpoint 1 text = %q", tr.Breakpoints[0].Text)
	}
	bp := tr.Breakpoints[1]
	if bp.Text != "(5) Trees gain 200 Health" || bp.Values["StonebarkTreeBonusHealth"] != 200 || bp.Style != 5 {
		t.Errorf("breakpoint 2 = %+v", bp)
	}
}

func TestExtract_AugmentsAndItems(t *testing.T) {
	d := mustExtract(t, defaultFixture(), "16.19.1")

	augs := map[string]Augment{}
	for _, a := range d.Augments {
		augs[a.APIName] = a
	}
	if a := augs["DA_18_ItemExtraction"]; a.Tier != 2 || a.Desc != "Gain 4 components." {
		t.Errorf("item extraction = %+v", a)
	}
	if a := augs["DA_18_SilverThing"]; a.Tier != 1 || a.Desc != "Gain 15% chance." || !slices.Equal(a.Traits, []string{"Elderwood"}) {
		t.Errorf("silver thing = %+v", a)
	}

	kinds := map[string]string{}
	variants := map[string]string{}
	for _, it := range d.Items {
		kinds[it.APIName] = it.Kind
		variants[it.APIName] = it.Variant
	}
	want := map[string]string{
		"TFT_Item_BFSword":     "component",
		"TFT_Item_Deathblade":  "completed",
		"DA_Barrier18":         "special",
		"DA_Barrier18_Upgrade": "special",
		"DA_PhantomEmblem18":   "emblem",
	}
	if len(kinds) != len(want) {
		t.Errorf("expected reward placeholders dropped, got kinds %v", kinds)
	}
	for api, kind := range want {
		if kinds[api] != kind {
			t.Errorf("%s kind = %q, want %q", api, kinds[api], kind)
		}
	}
	if variants["DA_Barrier18_Upgrade"] != "upgraded" || variants["DA_Barrier18"] != "" {
		t.Errorf("unexpected variants: %v", variants)
	}
}

func TestExtract_UnknownSet(t *testing.T) {
	if _, err := Extract(defaultFixture().json(t), 99, "v"); err == nil {
		t.Error("expected error for a set not in the export")
	}
}

func TestLatestSet_IgnoresAlternateModes(t *testing.T) {
	got, err := LatestSet(defaultFixture().json(t))
	if err != nil || got != 18 {
		t.Errorf("LatestSet = %d, %v; want 18", got, err)
	}
}

func TestDiff(t *testing.T) {
	oldF := defaultFixture()
	newF := defaultFixture()
	newF.akaliMana = 25
	newF.elderwoodHP = 250
	newF.extractionNum = 3
	newF.extraAugment = true

	changes := Diff(mustExtract(t, oldF, "16.18.1"), mustExtract(t, newF, "16.19.1"))

	type key struct{ category, name, kind, field string }
	got := map[key][2]float64{}
	for _, c := range changes {
		var v [2]float64
		if c.Old != nil {
			v[0] = *c.Old
		}
		if c.New != nil {
			v[1] = *c.New
		}
		got[key{c.Category, c.Name, c.Kind, c.Field}] = v
	}
	want := map[key][2]float64{
		{"unit", "Akali", "changed", "mana"}:                              {30, 25},
		{"trait", "Elderwood", "changed", "(5) StonebarkTreeBonusHealth"}: {200, 250},
		{"augment", "Item Extraction", "changed", "NumComponents"}:        {4, 3},
		{"augment", "Brand New", "added", ""}:                             {},
	}
	if len(got) != len(want) {
		t.Errorf("expected %d changes, got %d: %+v", len(want), len(got), changes)
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("missing or wrong change %+v: got %v (present=%v), want %v", k, g, ok, v)
		}
	}
}

func TestDiff_ReportsRemovalsAndRewordsOnly(t *testing.T) {
	oldD := mustExtract(t, defaultFixture(), "1")
	newD := mustExtract(t, defaultFixture(), "2")
	newD.Units = newD.Units[:1] // Ashe removed
	newD.Items[0].Desc = "Reworded with no number change"

	changes := Diff(oldD, newD)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %+v", changes)
	}
	kinds := map[string]string{}
	for _, c := range changes {
		kinds[c.Name] = c.Kind
	}
	if kinds["Ashe"] != "removed" || kinds[newD.Items[0].Name] != "text" {
		t.Errorf("unexpected changes: %+v", changes)
	}
}

func TestDiff_IdenticalIsEmpty(t *testing.T) {
	if c := Diff(mustExtract(t, defaultFixture(), "1"), mustExtract(t, defaultFixture(), "2")); len(c) != 0 {
		t.Errorf("expected no changes, got %+v", c)
	}
}

func TestCompareVersionsAndPatchOf(t *testing.T) {
	if CompareVersions("16.9.1", "16.10.0") >= 0 || CompareVersions("16.19.2", "16.19.10") >= 0 || CompareVersions("16.19", "16.19.0") != 0 {
		t.Error("versions must compare numerically by segment")
	}
	if got := PatchOf("16.19.8230722"); got != "16.19" {
		t.Errorf("PatchOf = %q", got)
	}
}
