package store_test

import (
	"context"
	"slices"
	"testing"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func TestMetaBuilds(t *testing.T) {
	st := storetest.New(t)
	// Zyra: the same exact build (in any order) three times, another build
	// twice; one copy with only two items.
	seedExplore(t, st, "M1", 1100, []board{
		{1, 9, []map[string]any{unit("Zyra", 2, "AA", "AA", "Gunblade"), unit("Vi", 1)}, nil},
		{2, 9, []map[string]any{unit("Zyra", 2, "Gunblade", "AA", "AA")}, nil},
		{3, 9, []map[string]any{unit("Zyra", 1, "AA", "Gunblade", "AA")}, nil},
		{6, 8, []map[string]any{unit("Zyra", 1, "AA", "Gunblade", "Shojin")}, nil},
		{7, 8, []map[string]any{unit("Zyra", 1, "Shojin", "AA", "Gunblade")}, nil},
		{8, 7, []map[string]any{unit("Zyra", 1, "AA", "Shojin")}, nil},
	})
	seedExplore(t, st, "M2", 1090, []board{ // normal game: out of scope below
		{1, 9, []map[string]any{unit("Zyra", 2, "AA", "AA", "Gunblade")}, nil},
	})

	res, err := st.MetaBuilds(context.Background(), store.ExploreFilter{Set: 18, Queues: []int{1100}}, 2, 5, 6)
	if err != nil {
		t.Fatal(err)
	}
	if res.Boards != 6 || len(res.Units) != 2 || res.Units[0].ID != "Zyra" {
		t.Fatalf("units = %+v (boards %d)", res.Units, res.Boards)
	}
	zyra := res.Units[0]
	if zyra.Boards != 6 || zyra.PickRate != 1 {
		t.Errorf("Zyra stats = %+v", zyra.PlacementStats)
	}
	if len(zyra.Builds) != 2 {
		t.Fatalf("builds = %+v", zyra.Builds)
	}
	top := zyra.Builds[0]
	if !slices.Equal(top.Items, []string{"AA", "AA", "Gunblade"}) || top.Boards != 3 || top.AvgPlacement != 2 {
		t.Errorf("top build = %+v (items sorted, duplicates kept, any order counts)", top)
	}
	if !slices.Equal(zyra.Builds[1].Items, []string{"AA", "Gunblade", "Shojin"}) || zyra.Builds[1].Boards != 2 {
		t.Errorf("second build = %+v", zyra.Builds[1])
	}
	// AA is on all 6 boards, Gunblade on 5, Shojin on 3 (each board once).
	if zyra.Items[0].ID != "AA" || zyra.Items[0].Boards != 6 || zyra.Items[1].ID != "Gunblade" || zyra.Items[1].Boards != 5 {
		t.Errorf("items = %+v", zyra.Items)
	}

	// The minimum-games bar drops builds seen less often.
	strict, err := st.MetaBuilds(context.Background(), store.ExploreFilter{Set: 18, Queues: []int{1100}}, 3, 5, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(strict.Units[0].Builds) != 1 {
		t.Errorf("min 3 games should keep only the top build: %+v", strict.Units[0].Builds)
	}
}
