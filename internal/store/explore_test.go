package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

type board struct {
	placement, level int
	units            []map[string]any
	traits           []map[string]any
}

func unit(id string, star int, items ...string) map[string]any {
	if items == nil {
		items = []string{}
	}
	return map[string]any{"character_id": id, "tier": star, "itemNames": items}
}

func trait(id string, n, tier int) map[string]any {
	return map[string]any{"name": id, "num_units": n, "tier_current": tier}
}

// seedExplore stores one ranked set-18 match per call, one board per entry.
func seedExplore(t *testing.T, st *store.Store, matchID string, queue int, boards []board) {
	t.Helper()
	ctx := context.Background()
	parts := map[string]store.MatchParticipant{}
	for i, b := range boards {
		puuid := fmt.Sprintf("%s-p%d", matchID, i)
		if err := st.UpsertAccountPUUIDOnly(ctx, puuid, "americas"); err != nil {
			t.Fatal(err)
		}
		units, _ := json.Marshal(b.units)
		traits, _ := json.Marshal(b.traits)
		parts[puuid] = store.MatchParticipant{PUUID: puuid, Placement: b.placement, Level: b.level,
			Units: units, Traits: traits, RawParticipant: []byte(`{}`)}
	}
	if err := st.InsertMatchWithParticipants(ctx, store.Match{
		MatchID: matchID, RoutingRegion: "americas", GameDatetime: time.Now(), GameVersion: "x",
		TFTSetNumber: 18, QueueID: queue, TFTGameType: "standard", RawPayload: []byte(`{}`),
	}, parts); err != nil {
		t.Fatal(err)
	}
}

func exploreFixture(t *testing.T) *store.Store {
	st := storetest.New(t)
	seedExplore(t, st, "M1", 1100, []board{
		{1, 9, []map[string]any{unit("Akali", 2, "IE", "JG"), unit("Amumu", 1)}, []map[string]any{trait("Elderwood", 5, 2), trait("Brawler", 1, 0)}},
		{2, 8, []map[string]any{unit("Akali", 1, "IE"), unit("Akali", 1)}, []map[string]any{trait("Elderwood", 3, 1)}},
		{5, 7, []map[string]any{unit("Amumu", 2, "Warmog")}, []map[string]any{trait("Brawler", 2, 1)}},
		{8, 6, []map[string]any{unit("Vi", 1, "IE")}, nil},
	})
	seedExplore(t, st, "M2", 1090, []board{ // normal game
		{1, 9, []map[string]any{unit("Akali", 3, "IE")}, nil},
	})
	return st
}

func explore(t *testing.T, st *store.Store, f store.ExploreFilter) *store.ExploreResult {
	t.Helper()
	f.Set = 18
	res, err := st.Explore(context.Background(), f, 30)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestExplore_SummaryAndBaseline(t *testing.T) {
	st := exploreFixture(t)

	all := explore(t, st, store.ExploreFilter{Queues: []int{1100}})
	if s := all.Summary; s.Boards != 4 || s.AvgPlacement != 4 || s.WinRate != 0.25 || s.Top4Rate != 0.5 {
		t.Errorf("ranked summary = %+v", s)
	}
	if all.Summary.Placements != [8]int{1, 1, 0, 0, 1, 0, 0, 1} {
		t.Errorf("placements = %v", all.Summary.Placements)
	}

	akali := explore(t, st, store.ExploreFilter{Queues: []int{1100}, Units: []store.UnitCond{{ID: "Akali"}}})
	if akali.Summary.Boards != 2 || akali.Summary.AvgPlacement != 1.5 || akali.Baseline.Boards != 4 {
		t.Errorf("Akali in ranked: summary %+v baseline %+v", akali.Summary, akali.Baseline)
	}
	if anyQueue := explore(t, st, store.ExploreFilter{Units: []store.UnitCond{{ID: "Akali"}}}); anyQueue.Summary.Boards != 3 {
		t.Errorf("no queue filter should include the normal game, got %d", anyQueue.Summary.Boards)
	}
}

func TestExplore_UnitConditionsApplyToTheSameUnit(t *testing.T) {
	st := exploreFixture(t)
	q := []int{1100}
	cases := []struct {
		name string
		cond store.UnitCond
		want int
	}{
		{"min star", store.UnitCond{ID: "Akali", MinStar: 2}, 1},
		{"item on unit", store.UnitCond{ID: "Akali", Items: []string{"IE"}}, 2},
		{"two items on one unit", store.UnitCond{ID: "Akali", Items: []string{"IE", "JG"}}, 1},
		// Board 2 has IE on one Akali and a second, 1-star Akali: the star
		// and item conditions must hold on the same copy.
		{"star and item together", store.UnitCond{ID: "Akali", MinStar: 2, Items: []string{"IE"}}, 1},
		{"item on another unit doesn't count", store.UnitCond{ID: "Amumu", Items: []string{"IE"}}, 0},
	}
	for _, c := range cases {
		if got := explore(t, st, store.ExploreFilter{Queues: q, Units: []store.UnitCond{c.cond}}).Summary.Boards; got != c.want {
			t.Errorf("%s: got %d boards, want %d", c.name, got, c.want)
		}
	}
}

func TestExplore_ItemTraitAndLevelConditions(t *testing.T) {
	st := exploreFixture(t)
	q := []int{1100}
	if n := explore(t, st, store.ExploreFilter{Queues: q, Items: []string{"IE"}}).Summary.Boards; n != 3 {
		t.Errorf("IE anywhere: got %d, want 3", n)
	}
	if n := explore(t, st, store.ExploreFilter{Queues: q, Traits: []store.TraitCond{{ID: "Elderwood", MinUnits: 5}}}).Summary.Boards; n != 1 {
		t.Errorf("Elderwood 5+: got %d, want 1", n)
	}
	if n := explore(t, st, store.ExploreFilter{Queues: q, Traits: []store.TraitCond{{ID: "Elderwood"}}}).Summary.Boards; n != 2 {
		t.Errorf("Elderwood any: got %d, want 2", n)
	}
	// An exact tier is a unit range: Elderwood 3-4 excludes the 5-unit board.
	if n := explore(t, st, store.ExploreFilter{Queues: q, Traits: []store.TraitCond{{ID: "Elderwood", MinUnits: 3, MaxUnits: 4}}}).Summary.Boards; n != 1 {
		t.Errorf("Elderwood 3-4: got %d, want 1", n)
	}
	if n := explore(t, st, store.ExploreFilter{Queues: q, LevelMin: 8}).Summary.Boards; n != 2 {
		t.Errorf("level 8+: got %d, want 2", n)
	}
	if n := explore(t, st, store.ExploreFilter{Queues: q, LevelMin: 7, LevelMax: 7}).Summary.Boards; n != 1 {
		t.Errorf("level 7: got %d, want 1", n)
	}
}

func TestExplore_Breakdowns(t *testing.T) {
	st := exploreFixture(t)
	res := explore(t, st, store.ExploreFilter{Queues: []int{1100}, Units: []store.UnitCond{{ID: "Akali"}}})

	byID := func(rows []store.ExploreBreakdownRow) map[string]store.ExploreBreakdownRow {
		m := map[string]store.ExploreBreakdownRow{}
		for _, r := range rows {
			m[fmt.Sprintf("%s/%d", r.ID, r.Tier)] = r
		}
		return m
	}
	units := byID(res.Units)
	// Board 2 has two Akalis: it still counts as one board.
	if units["Akali/0"].Boards != 2 || units["Amumu/0"].Boards != 1 {
		t.Errorf("unit breakdown = %+v", res.Units)
	}
	if items := byID(res.Items); items["IE/0"].Boards != 2 || items["JG/0"].Boards != 1 {
		t.Errorf("item breakdown = %+v", res.Items)
	}
	traits := byID(res.Traits)
	if _, inactive := traits["Brawler/0"]; inactive || traits["Elderwood/2"].Boards != 1 || traits["Elderwood/1"].Boards != 1 {
		t.Errorf("trait breakdown should list active tiers only: %+v", res.Traits)
	}
	if len(res.UnitItems) != 1 || len(res.UnitStars) != 1 {
		t.Fatalf("expected one unit-items/stars list per unit condition")
	}
	if ui := byID(res.UnitItems[0]); ui["IE/0"].Boards != 2 || ui["JG/0"].Boards != 1 {
		t.Errorf("items on Akali = %+v", res.UnitItems[0])
	}
	// Stars are per board, by its best copy: board 1's 2-star, board 2's
	// pair of 1-stars counts once.
	if us := byID(res.UnitStars[0]); us["Akali/1"].Boards != 1 || us["Akali/2"].Boards != 1 {
		t.Errorf("Akali stars = %+v", res.UnitStars[0])
	}
}

func TestExplore_EmptyResultIsWellFormed(t *testing.T) {
	st := exploreFixture(t)
	res := explore(t, st, store.ExploreFilter{Units: []store.UnitCond{{ID: "Nobody"}}})
	if res.Summary.Boards != 0 || res.Summary.AvgPlacement != 0 || res.Units == nil || res.UnitItems == nil {
		t.Errorf("empty result = %+v", res)
	}
}

func TestExploreOptions(t *testing.T) {
	st := exploreFixture(t)
	o, err := st.ExploreOptions(context.Background(), 18)
	if err != nil {
		t.Fatal(err)
	}
	if o.Boards != 5 || o.Levels != [2]int{6, 9} || len(o.Queues) != 2 || o.Queues[0].ID != 1100 {
		t.Errorf("options = %+v", o)
	}
	if o.Units[0].ID != "Akali" || o.Units[0].Boards != 3 {
		t.Errorf("units should be sorted by boards: %+v", o.Units)
	}
	var elderwood store.ExploreTraitCount
	for _, tr := range o.Traits {
		if tr.ID == "Elderwood" {
			elderwood = tr
		}
		if tr.ID == "Brawler" && tr.Boards != 1 {
			t.Errorf("only active Brawler counts: %+v", tr)
		}
	}
	if elderwood.MaxUnits != 5 || elderwood.Boards != 2 {
		t.Errorf("Elderwood = %+v", elderwood)
	}
	// Per-tier counts: one board at tier 1 (3 units), one at tier 2 (5).
	if len(elderwood.Tiers) != 2 || elderwood.Tiers[0] != (store.ExploreTierCount{Tier: 1, Boards: 1}) ||
		elderwood.Tiers[1] != (store.ExploreTierCount{Tier: 2, Boards: 1}) {
		t.Errorf("Elderwood tiers = %+v", elderwood.Tiers)
	}
	if o.Traits[0].Boards < o.Traits[len(o.Traits)-1].Boards {
		t.Errorf("traits should be sorted by boards: %+v", o.Traits)
	}
}

func TestExplore_UnitItemsCountBoardsNotCopies(t *testing.T) {
	st := storetest.New(t)
	// One board, two Akalis both holding IE: still one board with IE.
	seedExplore(t, st, "M1", 1100, []board{
		{1, 9, []map[string]any{unit("Akali", 2, "IE"), unit("Akali", 2, "IE", "IE")}, nil},
	})
	res := explore(t, st, store.ExploreFilter{Units: []store.UnitCond{{ID: "Akali"}}})
	if len(res.UnitItems[0]) != 1 || res.UnitItems[0][0].ID != "IE" || res.UnitItems[0][0].Boards != 1 {
		t.Errorf("items on Akali = %+v", res.UnitItems[0])
	}
}

func TestExplore_TraitBreakdownIncludesEveryTier(t *testing.T) {
	st := storetest.New(t)
	// More (trait, tier) pairs than the breakdown limit used below.
	var boards []board
	for i := 0; i < 8; i++ {
		var traits []map[string]any
		for k := 0; k < 4; k++ {
			traits = append(traits, trait(fmt.Sprintf("T%d", k), i%3+1, i%3+1))
		}
		boards = append(boards, board{i + 1, 8, nil, traits})
	}
	seedExplore(t, st, "M1", 1100, boards)

	res, err := st.Explore(context.Background(), store.ExploreFilter{Set: 18}, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 4 traits x 3 tiers, even though breakdowns are limited to 3 rows.
	if len(res.Traits) != 12 {
		t.Errorf("expected all 12 (trait, tier) rows, got %d", len(res.Traits))
	}
	if len(res.Units) > 3 || len(res.Items) > 3 {
		t.Errorf("other breakdowns keep the limit")
	}
}

func TestInsertMatchIfNew_ConcurrentWritersStoreOnce(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	for _, p := range []string{"p1", "p2"} {
		if err := st.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
			t.Fatal(err)
		}
	}
	m := store.Match{MatchID: "NA1_1", RoutingRegion: "americas", GameDatetime: time.Now(), GameVersion: "x",
		TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`)}
	parts := map[string]store.MatchParticipant{}
	for _, p := range []string{"p1", "p2"} {
		parts[p] = store.MatchParticipant{PUUID: p, Placement: 1, Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
	}

	var wg sync.WaitGroup
	var inserted atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := st.InsertMatchIfNew(ctx, m, parts)
			if err != nil {
				t.Error(err)
			}
			if ok {
				inserted.Add(1)
			}
		}()
	}
	wg.Wait()
	if inserted.Load() != 1 {
		t.Fatalf("%d writers reported inserting the same match, want exactly 1", inserted.Load())
	}
	var matches, participants int
	st.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM matches), (SELECT count(*) FROM match_participants)`).Scan(&matches, &participants)
	if matches != 1 || participants != 2 {
		t.Fatalf("matches=%d participants=%d, want 1 and 2", matches, participants)
	}
}
