package setdata

import (
	"bytes"
	"slices"
	"testing"
)

func TestRender(t *testing.T) {
	values := map[string]float64{"Damage": 120, "Ratio": 0.25}
	cases := map[string]string{
		"Deal @Damage@ damage":                    "Deal 120 damage",
		"Gain @Ratio*100@% speed":                 "Gain 25% speed",
		"Deal @damage@ (case-insensitive)":        "Deal 120 (case-insensitive)",
		"Deal @MagicDamageCalc1@ damage":          "Deal [[Magic Damage]] damage",
		"Gain @{0f90e7a4}@ stacks":                "Gain [[value]] stacks",
		"Start with @GenericCalc1@ Armor":         "Start with [[value]] Armor",
		"Leap @LeapHexRadius@ hexes":              "Leap [[Leap Hex Radius]] hexes",
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

func TestRender_DropsLiveTrackers(t *testing.T) {
	values := map[string]float64{"Gold": 2, "AttackSpeed": 0.1}
	cases := map[string]string{
		// A tracker on its own line goes, with any markup around it.
		"Gain @Gold@ gold.<br>Total Payouts: @TFTUnitProperty.item:TFT11_BloodBankPayoutTotal@ Gold<br>": "Gain 2 gold.",
		"Roll dice.<br><br>Reward: @TFTUnitProperty.item:TFT_Augment_MagicRoll@":                         "Roll dice.",
		"Win fights.<br><br><rules>Foes vanquished: @TFTUnitProperty.item:TFT15_X@</rules>":              "Win fights.",
		// A tracker in parentheses goes without taking the sentence with it.
		"Gain @AttackSpeed*100@% (Current: @TFTUnitProperty.item:TFT9_PumpingUpRounds@%) each round.": "Gain 10% each round.",
		// Unit-scoped trackers have no "item" segment.
		"Equip it.<br>@TFTUnitProperty.:TFT_Augment_TragicalBlade_TRAKey@": "Equip it.",
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
	if want := "Deal 100/150/225 (AD, AP) damage and [[Magic Damage]] more."; akali.Ability.Desc != want {
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
	}
	for _, w := range d.Wisps {
		if w.Kind != "wisp" {
			t.Errorf("%s in Wisps has kind %q", w.APIName, w.Kind)
		}
		variants[w.APIName] = w.Variant
	}
	want := map[string]string{
		"TFT_Item_BFSword":    "component",
		"TFT_Item_Deathblade": "completed",
		"DA_PhantomEmblem18":  "emblem",
		"TFT_Item_DebugBase":  "other", // the no-pools heuristic can't tell debug items apart
	}
	if len(kinds) != len(want) {
		t.Errorf("expected reward placeholders dropped, got kinds %v", kinds)
	}
	for api, kind := range want {
		if kinds[api] != kind {
			t.Errorf("%s kind = %q, want %q", api, kinds[api], kind)
		}
	}
	if len(variants) != 2 || variants["DA_Barrier18_Upgrade"] != "upgraded" || variants["DA_Barrier18"] != "" {
		t.Errorf("expected both Barrier variants as wisps, got %v", variants)
	}
}

func TestExtract_UnknownSet(t *testing.T) {
	if _, err := Extract(defaultFixture().json(t), 99, "v", nil); err == nil {
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

func TestDiff_WispsAreTheirOwnCategory(t *testing.T) {
	oldD := mustExtract(t, defaultFixture(), "1")
	newD := mustExtract(t, defaultFixture(), "2")
	newD.Wisps[0].Desc = "Reworded wisp"

	changes := Diff(oldD, newD)
	if len(changes) != 1 || changes[0].Category != "wisp" || changes[0].Kind != "text" || changes[0].APIName != newD.Wisps[0].APIName {
		t.Errorf("expected one wisp text change, got %+v", changes)
	}
}

func TestExtract_KeepsOnlyDAAugmentsWhenTheSetHasThem(t *testing.T) {
	d := mustExtract(t, defaultFixture(), "16.19.1")

	var got []string
	for _, a := range d.Augments {
		got = append(got, a.APIName)
	}
	// Legacy copies (TFT_Augment_ItemExtraction) and legacy-only augments
	// (TFT9_Augment_OldFavorite) go; same-named DA_ variants stay.
	want := []string{"DA_18_Primal_Nidalee", "DA_18_Primal_Sivir", "DA_18_ItemExtraction", "DA_18_SilverThing"}
	if !slices.Equal(got, want) {
		t.Errorf("augments = %v\nwant      %v", got, want)
	}
}

func TestDedupeAugments_SetsWithoutDAIDs(t *testing.T) {
	augs := []Augment{
		{APIName: "TFT17_Augment_Roll", Name: "Magic Roll"},
		{APIName: "TFT_Augment_MagicRoll", Name: "A Magic Roll"}, // legacy copy
		{APIName: "TFT_Augment_Other", Name: "Other"},            // legacy, no twin: kept
	}
	var got []string
	for _, a := range dedupeAugments(augs, 17) {
		got = append(got, a.APIName)
	}
	if want := []string{"TFT17_Augment_Roll", "TFT_Augment_Other"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAugmentKey(t *testing.T) {
	for _, pair := range [][2]string{{"A Magic Roll", "Magic Roll"}, {"Call To Chaos", "Call to Chaos"}, {"The Golden Egg", "golden egg"}} {
		if augmentKey(pair[0]) != augmentKey(pair[1]) {
			t.Errorf("%q and %q should be the same augment", pair[0], pair[1])
		}
	}
	if augmentKey("Aftershock") != "aftershock" {
		t.Error("a word starting with 'a' isn't an article")
	}
}

func TestDiff_AugmentIDSwapIsNotAnAddAndRemove(t *testing.T) {
	oldD := &SetData{Augments: []Augment{{APIName: "TFT_Augment_ExpectedUnexpectedness", Name: "Expected Unexpectedness", Desc: "Roll.",
		Values: map[string]float64{"Gold": 9, "NumReforgers": 3}}}}
	newD := &SetData{Augments: []Augment{{APIName: "DA_ExpectedUnexpectedness", Name: "Expected Unexpectedness", Desc: "Roll.",
		Values: map[string]float64{"Gold": 10, "NewOnlyField": 1}}}}

	changes := Diff(oldD, newD)
	// Fields only one of the two objects defines aren't balance changes.
	if len(changes) != 1 || changes[0].Kind != "changed" || changes[0].Field != "Gold" || changes[0].APIName != "DA_ExpectedUnexpectedness" {
		t.Errorf("expected a single Gold change on the new id, got %+v", changes)
	}
}

func TestDiff_RenameWithSameIDIsAChange(t *testing.T) {
	oldD := &SetData{Augments: []Augment{{APIName: "DA_BonusGift", Name: "Bonus Gift", Desc: "One gift."}}}
	newD := &SetData{Augments: []Augment{{APIName: "DA_BonusGift", Name: "Bonus Gifts", Desc: "Two gifts."}}}

	changes := Diff(oldD, newD)
	if len(changes) != 2 || changes[0].Kind != "renamed" || changes[0].Field != "Bonus Gift" || changes[1].Kind != "text" {
		t.Errorf("expected a rename plus a reword, got %+v", changes)
	}

	// A rename alone is still reported.
	newD.Augments[0].Desc = "One gift."
	if changes := Diff(oldD, newD); len(changes) != 1 || changes[0].Kind != "renamed" {
		t.Errorf("expected just the rename, got %+v", changes)
	}
}

func TestDiff_SameNamedAugmentVariantsStayDistinct(t *testing.T) {
	variants := func(sivirDesc string) *SetData {
		return &SetData{Augments: []Augment{
			{APIName: "DA_18_Primal_Nidalee", Name: "Beast Within", Desc: "Nidalee."},
			{APIName: "DA_18_Primal_Sivir", Name: "Beast Within", Desc: sivirDesc},
		}}
	}
	changes := Diff(variants("Sivir."), variants("Sivir, reworded."))
	if len(changes) != 1 || changes[0].APIName != "DA_18_Primal_Sivir" || changes[0].Kind != "text" {
		t.Errorf("expected only Sivir's variant to change, got %+v", changes)
	}
}

func TestTFTPatch(t *testing.T) {
	cases := []struct {
		set        int
		game, want string
	}{
		{18, "16.17", "18.1"},
		{18, "16.19", "18.3"},
		{18, "16.16", ""}, // before the set's first patch
		{18, "17.1", ""},  // next major: no anchor yet
		{99, "16.19", ""}, // unknown set
	}
	for _, c := range cases {
		if got := TFTPatch(c.set, c.game); got != c.want {
			t.Errorf("TFTPatch(%d, %q) = %q, want %q", c.set, c.game, got, c.want)
		}
	}
	if got := OfficialNotesURL("18.3"); got != "https://teamfighttactics.leagueoflegends.com/en-us/news/game-updates/teamfight-tactics-patch-18-3/" {
		t.Errorf("OfficialNotesURL = %q", got)
	}
}

func TestParsePools(t *testing.T) {
	p, err := parsePools(bytes.NewReader(fixtureMap22(t)), 18)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Items, []string{"DA_Component_BFSword", "DA_Deathblade", "DA_HealthPotion18"}) {
		t.Errorf("items = %v (the shared Common_Items list must be ignored)", p.Items)
	}
	if len(p.Augments) != 4 || len(p.Wisps) != 3 {
		t.Errorf("augments = %v, wisps = %v", p.Augments, p.Wisps)
	}
	if _, err := parsePools(bytes.NewReader(fixtureMap22(t)), 17); err == nil {
		t.Error("expected an error for a set the map data doesn't define")
	}
}

func TestExtract_WithPools(t *testing.T) {
	p, err := parsePools(bytes.NewReader(fixtureMap22(t)), 18)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Extract(defaultFixture().json(t), 18, "16.19.1", p)
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[string]string{}
	for _, it := range d.Items {
		kinds[it.APIName] = it.Kind
	}
	// Exactly the set's own items — no legacy copies, debug items or reward
	// placeholders — kept even without a description.
	want := map[string]string{"DA_Component_BFSword": "component", "DA_Deathblade": "completed", "DA_HealthPotion18": "potion"}
	if len(kinds) != len(want) {
		t.Errorf("items = %v, want %v", kinds, want)
	}
	for api, k := range want {
		if kinds[api] != k {
			t.Errorf("%s kind = %q, want %q", api, kinds[api], k)
		}
	}

	var wisps []string
	for _, w := range d.Wisps {
		wisps = append(wisps, w.APIName+":"+w.Variant)
	}
	// Phantom Emblem is a Wisp (it's in the Charms list), not an emblem.
	if want := []string{"DA_Barrier18:", "DA_Barrier18_Upgrade:upgraded", "DA_PhantomEmblem18:"}; !slices.Equal(wisps, want) {
		t.Errorf("wisps = %v, want %v", wisps, want)
	}
	if len(d.Augments) != 4 {
		t.Errorf("expected the 4 pooled augments, got %d", len(d.Augments))
	}
}

func TestClassifyItem(t *testing.T) {
	cases := map[string]string{
		"DA_18_EmblemCoven":               "emblem",
		"DA_18_EmblemFloraFatalisAugment": "emblem",
		"DA_Artifact_LichBane":            "artifact",
		"DA_BloodthirsterRadiant":         "radiant",
		"DA_BlastPotion18_Radiant":        "radiant",
		"DA_Consumable_ItemRemover":       "consumable",
		"DA_Reforger":                     "consumable",
		"DA_MasterworkUpgrade":            "consumable",
		"DA_LuckyItemChest":               "consumable",
		"DA_SpeedBooster18":               "booster",
		"DA_ManaPotion18":                 "potion",
		"DA_Something18":                  "other",
	}
	for api, want := range cases {
		if got, _ := classifyItem(api, false, false, false); got != want {
			t.Errorf("classifyItem(%s) = %q, want %q", api, got, want)
		}
	}
}

func TestClassifyItem_CraftableEmblemIsNotACompletedItem(t *testing.T) {
	if kind, _ := classifyItem("DA_18_EmblemBrawler", false, true, false); kind != "emblem" {
		t.Errorf("a Spatula-recipe emblem should be an emblem, got %q", kind)
	}
	if _, variant := classifyItem("DA_18_EmblemFloraFatalisAugment", false, false, false); variant != "augment" {
		t.Errorf("expected the augment-granted emblem variant, got %q", variant)
	}
}
