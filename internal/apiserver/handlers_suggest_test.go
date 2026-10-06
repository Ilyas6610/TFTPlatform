package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
	"tft-platform/internal/suggest"
)

func TestExploreSuggest_Rejects(t *testing.T) {
	s := &Server{} // validation runs before the store is touched
	for _, q := range []string{
		"set=0&have_unit=A",
		"set=18&have_unit=bad%20id",
		"set=18&have_unit=A&have_item=x%3Bdrop",
		"set=18&queue=x&have_unit=A",
		"set=18&have_unit=A&have_unit=B&have_unit=C&have_unit=D&have_unit=E&have_unit=F&have_unit=G&have_unit=H&have_unit=I&have_unit=J&have_unit=K&have_unit=L&have_unit=M&have_unit=N&have_unit=O&have_unit=P",
	} {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/explore/suggest?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, rec.Code)
		}
	}
}

func TestExploreSuggest_NoUnitsIsEmptyNotAnError(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(&Server{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/explore/suggest?set=18", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	var res suggest.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Plan == nil || res.Comps == nil {
		t.Fatalf("want empty lists, got %s (%v)", rec.Body, err)
	}
}

// seedItemBoards stores a ranked set-18 match whose players each field Zyra
// with the given items, placing 1..n.
func seedItemBoards(t *testing.T, st *store.Store, matchID string, n int, items []string) {
	t.Helper()
	ctx := context.Background()
	parts := map[string]store.MatchParticipant{}
	for i := 0; i < n; i++ {
		puuid := fmt.Sprintf("%s-p%d", matchID, i)
		if err := st.UpsertAccountPUUIDOnly(ctx, puuid, "americas"); err != nil {
			t.Fatal(err)
		}
		units, _ := json.Marshal([]map[string]any{{"character_id": "Zyra", "tier": 2, "itemNames": items}})
		parts[puuid] = store.MatchParticipant{PUUID: puuid, Placement: i + 1, Level: 9,
			Units: units, Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
	}
	if err := st.InsertMatchWithParticipants(ctx, store.Match{
		MatchID: matchID, RoutingRegion: "americas", GameDatetime: time.Now(), GameVersion: "x",
		TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
	}, parts); err != nil {
		t.Fatal(err)
	}
}

func TestExploreSuggest_BuildsFromComponents(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	s := &Server{Store: st}
	seedItemBoards(t, st, "NA1_1", 4, []string{"IE", "JG", "GS"})

	// Set data supplies the recipe: IE = Sword + Glove.
	data, _ := json.Marshal(setdata.SetData{SetNumber: 18, Version: "16.1.1", Items: []setdata.Item{
		{APIName: "IE", Kind: "completed", Composition: []string{"Sword", "Glove"}},
		{APIName: "JG", Kind: "completed", Composition: []string{"Rod", "Glove"}},
	}})
	if _, err := st.InsertSetDataSnapshot(ctx, store.SetDataSnapshot{SetNumber: 18, Version: "16.1.1", Patch: "16.1", Data: data}); err != nil {
		t.Fatal(err)
	}

	get := func(q string) suggest.Result {
		t.Helper()
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/explore/suggest?"+q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d: %s", q, rec.Code, rec.Body)
		}
		var res suggest.Result
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	// JG and GS held whole, IE only as Sword + Glove: the full build is ready.
	res := get("set=18&queue=1100&have_unit=Zyra&have_item=JG&have_item=GS&have_item=Sword&have_item=Glove")
	if len(res.Plan) != 1 || res.Plan[0].Unit != "Zyra" {
		t.Fatalf("plan = %+v", res.Plan)
	}
	b := res.Plan[0].Build
	if !b.Ready || len(b.Steps) != 3 || !slices.Equal(b.Items, []string{"GS", "IE", "JG"}) {
		t.Errorf("build = %+v", b)
	}
	var combined bool
	for _, st := range b.Steps {
		if st.Item == "IE" && slices.Equal(st.From, []string{"Sword", "Glove"}) {
			combined = true
		}
	}
	if !combined {
		t.Errorf("IE should be combined from Sword + Glove: %+v", b.Steps)
	}

	// Only one item held: still proposed, with the rest missing.
	part := get("set=18&queue=1100&have_unit=Zyra&have_item=GS")
	if len(part.Plan) != 1 || part.Plan[0].Build.Ready || len(part.Plan[0].Build.Missing) != 2 {
		t.Errorf("partial plan = %+v", part.Plan)
	}

	// An item the unit never builds: its best build is shown to aim for.
	none := get("set=18&queue=1100&have_unit=Zyra&have_item=Warmogs")
	if len(none.Plan) != 1 || len(none.Plan[0].Build.Steps) != 0 || !slices.Equal(none.Leftover, []string{"Warmogs"}) {
		t.Errorf("unrelated item: %+v", none)
	}

	// Units only: no items needed to see what the unit builds.
	only := get("set=18&queue=1100&have_unit=Zyra")
	if len(only.Plan) != 1 || len(only.Units[0].Options) != 1 {
		t.Errorf("units only: %+v", only)
	}

	// Items only: the units those items fit.
	byItems := get("set=18&queue=1100&have_item=JG&have_item=GS")
	if len(byItems.Candidates) != 1 || byItems.Candidates[0].Unit != "Zyra" {
		t.Errorf("items only: %+v", byItems.Candidates)
	}
}

// Emblems map to traits by name through the stored set data: with only a
// Brawler Emblem, boards playing Brawler come back as final boards.
func TestExploreSuggest_EmblemMatchesTraitBoards(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	s := &Server{Store: st}

	data, _ := json.Marshal(setdata.SetData{
		SetNumber: 18, Version: "16.1.1",
		Traits: []setdata.Trait{{APIName: "DA_18_Brawler", Name: "Brawler"}},
		Items:  []setdata.Item{{APIName: "DA_18_EmblemBrawler", Name: "Brawler Emblem", Kind: "emblem"}},
	})
	if _, err := st.InsertSetDataSnapshot(ctx, store.SetDataSnapshot{SetNumber: 18, Version: "16.1.1", Patch: "16.1", Data: data}); err != nil {
		t.Fatal(err)
	}
	// Six level 9 boards of one Brawler comp: enough for a comp.
	units, _ := json.Marshal([]map[string]any{
		{"character_id": "A", "tier": 2, "itemNames": []string{}}, {"character_id": "B", "tier": 2, "itemNames": []string{}},
	})
	traits, _ := json.Marshal([]map[string]any{{"name": "DA_18_Brawler", "num_units": 2, "tier_current": 1}})
	parts := map[string]store.MatchParticipant{}
	for i := 0; i < 6; i++ {
		p := fmt.Sprintf("p%d", i)
		if err := st.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
			t.Fatal(err)
		}
		parts[p] = store.MatchParticipant{PUUID: p, Placement: i + 1, Level: 9, Units: units, Traits: traits, RawParticipant: []byte(`{}`)}
	}
	if err := st.InsertMatchWithParticipants(ctx, store.Match{
		MatchID: "NA1_1", RoutingRegion: "americas", GameDatetime: time.Now(), GameVersion: "x",
		TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
	}, parts); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/explore/suggest?set=18&queue=1100&have_item=DA_18_EmblemBrawler", nil))
	var res suggest.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Comps) != 1 || len(res.Comps[0].Emblems) != 1 || res.Comps[0].Emblems[0].Trait != "DA_18_Brawler" {
		t.Fatalf("comps = %+v, want the Brawler board with the emblem for Brawler", res.Comps)
	}
}
