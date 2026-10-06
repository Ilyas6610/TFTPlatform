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
