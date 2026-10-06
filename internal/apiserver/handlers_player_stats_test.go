package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func TestPlayerStats(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	ctx := context.Background()
	for _, p := range []string{"me", "other"} {
		if err := s.Store.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
			t.Fatal(err)
		}
	}
	board := func(units ...string) []byte {
		var us []map[string]any
		for _, u := range units {
			us = append(us, map[string]any{"character_id": u, "tier": 2, "itemNames": []string{}})
		}
		b, _ := json.Marshal(us)
		return b
	}
	comp := board("A", "B", "C", "D", "E", "F", "G", "H")
	// Three ranked games: "me" places 1, 2, 3 on the same board; "other"
	// places 8 each time on another.
	for i := 1; i <= 3; i++ {
		parts := map[string]store.MatchParticipant{
			"me":    {PUUID: "me", Placement: i, Level: 9, Units: comp, Traits: []byte(`[]`), RawParticipant: []byte(`{}`)},
			"other": {PUUID: "other", Placement: 8, Level: 9, Units: board("X"), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)},
		}
		if err := s.Store.InsertMatchWithParticipants(ctx, store.Match{
			MatchID: fmt.Sprintf("NA1_%d", i), RoutingRegion: "americas", GameDatetime: time.Unix(int64(i)*1000, 0),
			GameVersion: "x", TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
		}, parts); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/players/me/stats?set=18", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	var res PlayerStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Summary.Boards != 3 || res.Summary.AvgPlacement != 2 || res.Summary.Placements != [8]int{1, 1, 1} {
		t.Errorf("summary = %+v, want my 3 games at 2.00 avg", res.Summary)
	}
	if res.Baseline.Boards != 6 {
		t.Errorf("baseline = %+v, want everyone's 6 boards", res.Baseline)
	}
	if len(res.Units) != 8 || res.Units[0].Boards != 3 {
		t.Errorf("units = %+v, want my 8 units, 3 games each", res.Units)
	}
	if len(res.Comps) != 1 || res.Comps[0].Boards != 3 {
		t.Errorf("comps = %+v, want one comp of my 3 boards", res.Comps)
	}
	if len(res.Sets) != 1 || res.Sets[0] != 18 || len(res.Queues) != 1 || res.Queues[0].QueueID != 1100 {
		t.Errorf("sets/queues = %v / %+v", res.Sets, res.Queues)
	}

	// Paged history carries the board.
	_, page := getMatches(t, s, "/api/v1/players/me/matches?limit=2")
	if len(page.Matches) != 2 || page.Matches[0].MatchID != "NA1_3" || len(page.Matches[0].Units) != 8 || !page.HasMore {
		t.Errorf("history page = %+v", page)
	}
}

func TestPlayerStats_RejectsBadInput(t *testing.T) {
	s := &Server{}
	for _, path := range []string{
		"/api/v1/players/bad%20id/stats?set=18",
		"/api/v1/players/me/stats",
		"/api/v1/players/me/stats?set=18&unit=A",
	} {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path, rec.Code)
		}
	}
}

// History games carry their patch (from the calendar) and LP (from the rank
// snapshots around them); the first page carries the current rank, and the
// stats split by patch with the known LP.
func TestPlayerHistory_PatchAndLP(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	ctx := context.Background()
	if err := s.Store.UpsertAccountPUUIDOnly(ctx, "me", "americas"); err != nil {
		t.Fatal(err)
	}
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, time.UTC) }
	for _, p := range []store.PatchStart{
		{SetNumber: 18, TFTPatch: "18.2", GamePatch: "16.18", StartsAt: day(10, 18), SourceURL: "x"},
		{SetNumber: 18, TFTPatch: "18.3", GamePatch: "16.19", StartsAt: day(23, 18), SourceURL: "x"},
	} {
		if err := s.Store.PutPatchStart(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	addGame := func(id string, at time.Time, placement int) {
		t.Helper()
		if err := s.Store.InsertMatchWithParticipants(ctx, store.Match{
			MatchID: id, RoutingRegion: "americas", GameDatetime: at, GameVersion: "TFT Unreal Version ?.?.?.?",
			TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
		}, map[string]store.MatchParticipant{"me": {PUUID: "me", Placement: placement, Level: 8,
			Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}}); err != nil {
			t.Fatal(err)
		}
	}
	addGame("NA1_1", day(15, 12), 5) // 18.2
	addGame("NA1_2", day(25, 12), 1) // 18.3
	// Snapshots bracket only NA1_2: +45 LP.
	for _, r := range []store.RankSnapshot{
		{QueueType: "RANKED_TFT", Tier: "MASTER", Rank: "I", LeaguePoints: 100, Wins: 5, Losses: 5},
		{QueueType: "RANKED_TFT", Tier: "MASTER", Rank: "I", LeaguePoints: 145, Wins: 6, Losses: 5},
	} {
		if _, err := s.Store.AddRankSnapshot(ctx, "me", r); err != nil {
			t.Fatal(err)
		}
	}
	// Snapshot times are now(); move them around NA1_2.
	if _, err := s.Store.Pool.Exec(ctx, `UPDATE rank_snapshots SET fetched_at = CASE WHEN league_points = 100 THEN $1::timestamptz ELSE $2::timestamptz END`,
		day(25, 11), day(25, 13)); err != nil {
		t.Fatal(err)
	}

	_, page := getMatches(t, s, "/api/v1/players/me/matches")
	if len(page.Matches) != 2 {
		t.Fatalf("matches = %+v", page.Matches)
	}
	newest, older := page.Matches[0], page.Matches[1]
	if newest.Patch != "18.3" || older.Patch != "18.2" {
		t.Errorf("patches = %q, %q; want 18.3, 18.2", newest.Patch, older.Patch)
	}
	if newest.LP == nil || newest.LP.Delta != 45 || newest.LP.Games != 1 || older.LP != nil {
		t.Errorf("lp = %+v, %+v; want +45 on NA1_2 only", newest.LP, older.LP)
	}
	if len(page.Ranks) != 1 || page.Ranks[0].LeaguePoints != 145 {
		t.Errorf("ranks = %+v, want the latest Master 145 LP", page.Ranks)
	}

	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/players/me/stats?set=18", nil))
	var res PlayerStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Patches) != 2 || res.Patches[0].Patch != "18.3" || res.Patches[0].LP != 45 || res.Patches[1].LPGames != 0 {
		t.Errorf("patches = %+v, want 18.3 (+45 LP) then 18.2 (no LP)", res.Patches)
	}
}
