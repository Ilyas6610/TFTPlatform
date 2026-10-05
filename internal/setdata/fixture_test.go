package setdata

import (
	"encoding/json"
	"testing"
)

// fixture builds a minimal CommunityDragon TFT export containing set 18
// (standard and an alternate-mode entry) with the shapes the extractor
// handles: shop and non-shop units, a breakpoint trait with hashed
// variables, augments, and items of each kind.
type fixture struct {
	akaliMana     float64
	elderwoodHP   float64
	extractionNum float64
	extraAugment  bool
}

func defaultFixture() fixture {
	return fixture{akaliMana: 30, elderwoodHP: 200, extractionNum: 4}
}

func (f fixture) json(t *testing.T) []byte {
	t.Helper()
	unit := func(api, name string, cost int, traits []string, mana float64) map[string]any {
		return map[string]any{
			"apiName": api, "name": name, "cost": cost, "traits": traits,
			"tileIcon": "assets/characters/" + api + "_square.tex",
			"stats":    map[string]any{"hp": 650.0, "mana": mana, "attackSpeed": 0.75000001, "armor": nil},
			"ability": map[string]any{
				"name": "Kunai Strike",
				"desc": "Deal @Damage@ %i:scaleAD%%i:scaleAP% damage and @MagicDamageCalc1@ more.",
				"variables": []any{
					map[string]any{"name": "Damage", "value": []float64{0, 100, 150, 225, 0, 0, 0}},
				},
			},
		}
	}
	augments := []any{"DA_18_ItemExtraction", "DA_18_SilverThing"}
	if f.extraAugment {
		augments = append(augments, "DA_18_NewAugment")
	}
	export := map[string]any{
		"setData": []any{
			map[string]any{
				"number": 18, "mutator": "TFTSet18_PAIRS",
				"champions": []any{}, "traits": []any{}, "augments": []any{}, "items": []any{},
			},
			map[string]any{
				"number": 18, "mutator": "TFTSet18",
				"champions": []any{
					unit("DA_18_Akali", "Akali", 1, []string{"Adaptor"}, f.akaliMana),
					unit("DA_18_Ashe", "Ashe", 4, []string{"Elderwood"}, 50),
					unit("TFT_BlueGolem", "Blue Golem", 0, nil, 0),     // neutral monster
					unit("DA_18_Sapling", "Sapling", 8, []string{}, 0), // summon
				},
				"traits": []any{
					map[string]any{
						"apiName": "DA_18_Elderwood", "name": "Elderwood",
						"icon": "assets/ux/traiticons/trait_icon_18_elderwood.tex",
						"desc": "Gain plants.<br><row>(@MinUnits@) A tree</row><br><row>(@MinUnits@) Trees gain @StonebarkTreeBonusHealth@ Health</row>",
						"effects": []any{
							map[string]any{"minUnits": 3, "maxUnits": 4, "style": 1, "variables": map[string]any{}},
							map[string]any{"minUnits": 5, "maxUnits": 25000, "style": 5, "variables": map[string]any{
								varHash("StonebarkTreeBonusHealth"): f.elderwoodHP,
							}},
						},
					},
				},
				"augments": augments,
				"items":    []any{"TFT_Item_BFSword", "TFT_Item_Deathblade", "DA_Barrier18", "DA_Barrier18_Upgrade", "TFT_Assist_Gold_1", "DA_PhantomEmblem18"},
			},
			map[string]any{"number": 17, "mutator": "TFTSet17", "champions": []any{}, "traits": []any{}, "augments": []any{}, "items": []any{}},
		},
		"items": []any{
			map[string]any{
				"apiName": "DA_18_ItemExtraction", "name": "Item Extraction",
				"desc":    "Gain @NumComponents@ components.",
				"icon":    "assets/maps/tft/icons/augments/hexcore/item-extraction-ii.tex",
				"effects": map[string]any{"NumComponents": f.extractionNum},
			},
			map[string]any{
				"apiName": "DA_18_SilverThing", "name": "Silver Thing",
				"desc": "Gain @Chance*100@% chance.", "icon": "assets/x/missing-t1.tex",
				"effects":          map[string]any{"Chance": 0.15000000596046448, "Unused": nil},
				"associatedTraits": []any{"DA_18_Elderwood"},
			},
			map[string]any{"apiName": "DA_18_NewAugment", "name": "Brand New", "desc": "New!", "icon": "assets/x/new-iii.tex", "effects": map[string]any{}},
			map[string]any{"apiName": "TFT_Item_BFSword", "name": "B.F. Sword", "desc": "+10 AD", "effects": map[string]any{}},
			map[string]any{"apiName": "TFT_Item_Deathblade", "name": "Deathblade", "desc": "Big damage", "composition": []any{"TFT_Item_BFSword", "TFT_Item_BFSword"}, "effects": map[string]any{}},
			map[string]any{"apiName": "DA_Barrier18", "name": "Barrier", "desc": "Shield", "effects": map[string]any{}},
			map[string]any{"apiName": "DA_Barrier18_Upgrade", "name": "Barrier", "desc": "Bigger shield", "effects": map[string]any{}},
			map[string]any{"apiName": "TFT_Assist_Gold_1", "name": "1 gold", "desc": "Gain 1 gold", "effects": map[string]any{}},
			map[string]any{"apiName": "DA_PhantomEmblem18", "name": "Phantom Emblem", "desc": "Holder gains Phantom", "effects": map[string]any{}},
		},
	}
	b, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustExtract(t *testing.T, f fixture, version string) *SetData {
	t.Helper()
	d, err := Extract(f.json(t), 18, version)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return d
}
