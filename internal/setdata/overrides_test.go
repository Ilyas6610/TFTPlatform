package setdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tft-platform/internal/store/storetest"
)

const localeJS = `window.initLocalei18n = "en";
      window.s18Unitsi18n = {"DA_18_Akali":"Akali","DA_18_Akali_ability":"Kunai Strike","DA_18_Akali_desc":"Deal <colorphysical>145/220/380 %i:scaleAD% + 10/15/25 %i:scaleAP%</colorphysical> damage.<br>Again.","DA_18_Ashe_desc":"Fire @StillUnknown@ arrows."};
      window.s18Itemsi18n = {"DA_Barrier18_desc":"Shield."};
      window.notText = {"DA_X_desc":"ignored"};`

func TestParseLocaleFile(t *testing.T) {
	got, err := parseLocaleFile(localeJS)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["DA_Barrier18"] != "Shield." || got["DA_X"] != "" {
		t.Errorf("expected the 3 *_desc entries from i18n objects only, got %v", got)
	}
	if _, err := parseLocaleFile("window.foo = 1;"); err == nil {
		t.Error("expected an error when no descriptions are found")
	}
}

func overrideFixture() *SetData {
	return &SetData{
		Version: "16.19.1",
		Units: []Unit{
			{APIName: "DA_18_Akali", Ability: Ability{Desc: "Deal [[Physical Damage]] damage."}},
			{APIName: "DA_18_Ashe", Ability: Ability{Desc: "Fire [[Arrows]] arrows."}},
			{APIName: "DA_18_Kobuko", Ability: Ability{Desc: "Heal 350/400/475."}},
		},
	}
}

func TestAttachTextOverrides(t *testing.T) {
	texts, _ := parseLocaleFile(localeJS)
	texts["DA_18_Kobuko"] = "Something else."
	ov := []TextOverrides{{Source: "tactics.tools", PageURL: "https://tactics.tools/info/units", Version: "16.19.1", Texts: texts}}

	d := overrideFixture()
	AttachTextOverrides(d, ov)

	akali, ashe, kobuko := d.Units[0].Ability, d.Units[1].Ability, d.Units[2].Ability
	if akali.Desc != "Deal 145/220/380 (AD) + 10/15/25 (AP) damage.\nAgain." || akali.DescSource.Name != "tactics.tools" {
		t.Errorf("Akali should be filled from the override, got %q %+v", akali.Desc, akali.DescSource)
	}
	// An override that would itself leave unknowns isn't an improvement.
	if ashe.Desc != "Fire [[Arrows]] arrows." || ashe.DescSource.Name != "" {
		t.Errorf("Ashe should keep our text, got %q %+v", ashe.Desc, ashe.DescSource)
	}
	// Complete descriptions from Riot's data are never replaced.
	if kobuko.Desc != "Heal 350/400/475." || kobuko.DescSource.Name != "" {
		t.Errorf("Kobuko should keep Riot's text, got %q", kobuko.Desc)
	}
}

func TestAttachTextOverrides_OnlyForTheVersionFetchedAgainst(t *testing.T) {
	texts, _ := parseLocaleFile(localeJS)
	d := overrideFixture()
	d.Version = "16.18.1"

	AttachTextOverrides(d, []TextOverrides{{Source: "tactics.tools", Version: "16.19.1", Texts: texts}})

	if !strings.Contains(d.Units[0].Ability.Desc, "[[") {
		t.Errorf("an older patch must not get the live patch's numbers, got %q", d.Units[0].Ability.Desc)
	}
}

func TestSyncTextOverrides(t *testing.T) {
	st := storetest.New(t)
	cd, src := newFakeCDragon(t)
	ctx := context.Background()
	cd.set("latest", "16.19.200", defaultFixture().json(t))
	if _, err := Sync(ctx, src, st, 18, "latest"); err != nil {
		t.Fatal(err)
	}

	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() || r.URL.Path != "/static/s18/en.js" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(localeJS))
	}))
	t.Cleanup(srv.Close)
	ov := OverrideSource{Name: "tactics.tools", BaseURL: srv.URL, HTTP: srv.Client()}

	n, err := SyncTextOverrides(ctx, ov, st, 18)
	if err != nil || n != 3 {
		t.Fatalf("SyncTextOverrides = %d, %v", n, err)
	}
	loaded, err := LoadTextOverrides(ctx, st, 18)
	if err != nil || len(loaded) != 1 || loaded[0].Version != "16.19.200" || loaded[0].PageURL == "" {
		t.Fatalf("expected overrides stored against the newest snapshot, got %+v (err %v)", loaded, err)
	}

	// A failed fetch reports the error and keeps what was stored.
	fail.Store(true)
	if _, err := SyncTextOverrides(ctx, ov, st, 18); err == nil {
		t.Error("expected a fetch error")
	}
	if loaded, _ := LoadTextOverrides(ctx, st, 18); len(loaded) != 1 || len(loaded[0].Texts) != 3 {
		t.Errorf("expected previous overrides kept, got %+v", loaded)
	}
}

func TestAttachTextOverrides_FillsMissingDescriptions(t *testing.T) {
	d := &SetData{Version: "v1", Items: []Item{{APIName: "DA_Bloodthirster", Desc: ""}, {APIName: "DA_Other", Desc: ""}}}
	AttachTextOverrides(d, []TextOverrides{{Source: "tactics.tools", Version: "v1", Texts: map[string]string{"DA_Bloodthirster": "Gain 20% Omnivamp."}}})

	if d.Items[0].Desc != "Gain 20% Omnivamp." || d.Items[0].DescSource.Name != "tactics.tools" {
		t.Errorf("expected the missing description filled, got %+v", d.Items[0])
	}
	if d.Items[1].Desc != "" || d.Items[1].DescSource.Name != "" {
		t.Errorf("an item without an override stays empty, got %+v", d.Items[1])
	}
}
