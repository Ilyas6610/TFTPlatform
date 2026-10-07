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

func TestPlayerRankHistory(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	ctx := context.Background()
	if err := s.Store.UpsertAccountPUUIDOnly(ctx, "me", "americas"); err != nil {
		t.Fatal(err)
	}
	for _, r := range []store.RankSnapshot{
		{QueueType: "RANKED_TFT", Tier: "DIAMOND", Rank: "I", LeaguePoints: 80, Wins: 1, Losses: 1},
		{QueueType: "RANKED_TFT", Tier: "DIAMOND", Rank: "I", LeaguePoints: 80, Wins: 1, Losses: 1}, // unchanged: not recorded
		{QueueType: "RANKED_TFT", Tier: "MASTER", Rank: "I", LeaguePoints: 10, Wins: 2, Losses: 1},
		{QueueType: "RANKED_TFT_DOUBLE_UP", Tier: "GOLD", Rank: "II", LeaguePoints: 5, Wins: 1, Losses: 0},
	} {
		if _, err := s.Store.AddRankSnapshot(ctx, "me", r); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/players/me/ranks", nil))
	var res RankHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	ranked := res.History["RANKED_TFT"]
	if len(ranked) != 2 || ranked[0].Value != 2780 || ranked[1].Value != 2810 || len(res.History["RANKED_TFT_DOUBLE_UP"]) != 1 {
		t.Errorf("history = %+v, want Diamond I 80 (2780) -> Master 10 (2810) and one Double Up point", res.History)
	}
	if res.Estimated == nil || len(res.Estimated) != 0 {
		t.Errorf("estimated = %+v, want an empty list (no games stored)", res.Estimated)
	}
}

func TestPlayerStats_DoubleUpPartners(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	ctx := context.Background()
	for _, p := range []string{"me", "pal", "foe1", "foe2"} {
		if err := s.Store.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Store.UpsertAccount(ctx, store.Account{PUUID: "named", GameName: "Named", TagLine: "NA1", RoutingRegion: "americas"}); err != nil {
		t.Fatal(err)
	}
	// "pal" has no resolved Riot ID; the payload carries one.
	payload := []byte(`{"info":{"participants":[{"puuid":"pal","riotIdGameName":"Pal","riotIdTagline":"EUW"}]}}`)
	game := func(id string, queue int, at int64, places map[string]int) {
		t.Helper()
		parts := map[string]store.MatchParticipant{}
		for p, pl := range places {
			parts[p] = store.MatchParticipant{PUUID: p, Placement: pl, Level: 8, Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
		}
		if err := s.Store.InsertMatchWithParticipants(ctx, store.Match{
			MatchID: id, RoutingRegion: "americas", GameDatetime: time.Unix(at, 0),
			GameVersion: "x", TFTSetNumber: 18, QueueID: queue, TFTGameType: "pairs", RawPayload: payload,
		}, parts); err != nil {
			t.Fatal(err)
		}
	}
	// Teammates are placement pairs: 1-2, 3-4, 5-6, 7-8.
	game("NA1_1", 1160, 1000, map[string]int{"me": 1, "pal": 2, "foe1": 3, "foe2": 4})
	game("NA1_2", 1160, 2000, map[string]int{"me": 4, "pal": 3, "foe1": 1, "foe2": 2})
	game("NA1_3", 1160, 3000, map[string]int{"me": 6, "named": 5, "pal": 7})
	// Ranked placements 1 and 2 aren't teammates.
	game("NA1_4", 1100, 4000, map[string]int{"me": 1, "pal": 2})

	get := func(path string) PlayerStatsResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body)
		}
		var res PlayerStatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := get("/api/v1/players/me/stats?set=18")
	if len(res.Partners) != 2 {
		t.Fatalf("partners = %+v, want pal and named", res.Partners)
	}
	pal, named := res.Partners[0], res.Partners[1]
	if pal.PUUID != "pal" || pal.Games != 2 || pal.AvgTeamPlacement != 1.5 || pal.Top2Rate != 1 || pal.WinRate != 0.5 ||
		pal.GameName != "Pal" || pal.TagLine != "EUW" || !pal.LastPlayed.Equal(time.Unix(2000, 0)) {
		t.Errorf("pal = %+v, want 2 games, team 1 and 2, name from the payload", pal)
	}
	if named.PUUID != "named" || named.Games != 1 || named.AvgTeamPlacement != 3 || named.Top2Rate != 0 || named.GameName != "Named" {
		t.Errorf("named = %+v, want 1 game as team 3, name from accounts", named)
	}
	if res := get("/api/v1/players/me/stats?set=18&queue=1100"); len(res.Partners) != 0 {
		t.Errorf("ranked-only partners = %+v, want none", res.Partners)
	}

	_, page := getMatches(t, s, "/api/v1/players/me/matches")
	var got []string
	for _, m := range page.Matches {
		p := "-"
		if m.Partner != nil {
			p = fmt.Sprintf("%s:%s#%s team %d", m.Partner.PUUID, m.Partner.GameName, m.Partner.TagLine, m.Team)
		}
		got = append(got, p)
	}
	want := []string{"-", "named:Named#NA1 team 3", "pal:Pal#EUW team 2", "pal:Pal#EUW team 1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("history partners = %v, want %v", got, want)
	}
}

func TestPlayerAdvice(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	// Not registered while the panel is disabled; mount it here.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/players/{puuid}/advice", s.handlePlayerAdvice)
	if rec := httptest.NewRecorder(); func() int {
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/players/me/advice?set=18", nil))
		return rec.Code
	}() != http.StatusNotFound {
		t.Errorf("advice is registered on the public router")
	}
	for path, want := range map[string]int{
		"/api/v1/players/bad%20id/advice?set=18":     http.StatusBadRequest,
		"/api/v1/players/me/advice?set=18&unit=X":    http.StatusBadRequest,
		"/api/v1/players/me/advice?set=18&queue=abc": http.StatusBadRequest,
		"/api/v1/players/me/advice?set=18":           http.StatusOK, // no games: empty advice
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d: %s", path, rec.Code, want, rec.Body)
		}
		if want == http.StatusOK && rec.Body.String() != `{"edge":0,"weak":[],"strong":[],"builds":[]}`+"\n" {
			t.Errorf("%s: body %s", path, rec.Body)
		}
	}
}

// The everyone's baseline in /stats comes from the stats cache: a player's
// own numbers are live, but the scope baseline (a scan of every board in it)
// is reused, and shared by every player in the scope.
func TestPlayerStats_BaselineIsCachedPerScope(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	h := NewRouter(s)
	seedBoards(t, s.Store, "NA1_1", [][]string{{"A"}, {"A"}, {"A"}}) // players NA1_1-p0..p2
	stats := func(puuid string) PlayerStatsResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/players/"+puuid+"/stats?set=18&queue=1100", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body)
		}
		var res PlayerStatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	if got := stats("NA1_1-p0"); got.Baseline.Boards != 3 || got.Summary.Boards != 1 {
		t.Fatalf("baseline %d boards, summary %d; want 3 and 1", got.Baseline.Boards, got.Summary.Boards)
	}
	seedBoards(t, s.Store, "NA1_2", [][]string{{"A"}, {"A"}})
	// Another player in the same scope reuses the cached baseline...
	if got := stats("NA1_1-p1"); got.Baseline.Boards != 3 {
		t.Errorf("baseline %d boards, want the cached 3", got.Baseline.Boards)
	}
	// ...while a player's own games are always live.
	if got := stats("NA1_2-p0"); got.Summary.Boards != 1 || got.Baseline.Boards != 3 {
		t.Errorf("summary %d, baseline %d; want 1 live game and the cached baseline 3", got.Summary.Boards, got.Baseline.Boards)
	}
}

func TestLobbyStrength(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	ctx := context.Background()
	// Nine ranked games; in game i every opponent is Master with i*100 LP
	// (a snapshot just after it).
	mine := []int{8, 7, 6, 5, 4, 3, 2, 1, 1}
	for i := 1; i <= 9; i++ {
		parts := map[string]store.MatchParticipant{}
		others := []int{}
		for pl := 1; pl <= 8; pl++ {
			if pl != mine[i-1] {
				others = append(others, pl)
			}
		}
		for j := 0; j < 8; j++ {
			p, placement := "me", mine[i-1]
			if j > 0 {
				p, placement = fmt.Sprintf("p%d", j), others[j-1]
			}
			if err := s.Store.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
				t.Fatal(err)
			}
			parts[p] = store.MatchParticipant{PUUID: p, Placement: placement, Level: 8, Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
		}
		if err := s.Store.InsertMatchWithParticipants(ctx, store.Match{
			MatchID: fmt.Sprintf("NA1_%d", i), RoutingRegion: "americas", GameDatetime: time.Unix(int64(i)*1000, 0),
			GameVersion: "x", TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
		}, parts); err != nil {
			t.Fatal(err)
		}
		// Each game's opponents have their own snapshot near it.
		for j := 1; j < 8; j++ {
			if _, err := s.Store.Pool.Exec(ctx, `INSERT INTO rank_snapshots (puuid, queue_type, tier, rank, league_points, wins, losses, fetched_at)
				VALUES ($1, 'RANKED_TFT', 'MASTER', 'I', $2, 1, 1, $3)`, fmt.Sprintf("p%d", j), i*100, time.Unix(int64(i)*1000+60, 0)); err != nil {
				t.Fatal(err)
			}
		}
	}

	// na1's ladder (other players): Grandmaster from 200 LP, Challenger from 600.
	for i := 0; i < 20; i++ {
		for _, e := range []struct {
			tier string
			lp   int
		}{{"GRANDMASTER", 200 + i}, {"CHALLENGER", 600 + i}} {
			p := fmt.Sprintf("%s%d", e.tier, i)
			if err := s.Store.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.Pool.Exec(ctx, `INSERT INTO league_entries (puuid, platform_region, tier, rank, league_points, wins, losses)
				VALUES ($1, 'na1', $2, 'I', $3, 1, 1)`, p, e.tier, e.lp); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, page := getMatches(t, s, "/api/v1/players/me/matches?limit=3")
	if l := page.Matches[0].Lobby; l == nil || l.Value != 2800+900 || l.Tier != "CHALLENGER" || l.Known != 7 || l.Opponents != 7 || l.Current != 0 {
		t.Errorf("newest game's lobby = %+v, want Challenger 900 LP from 7 of 7", l)
	}
	if l := page.Matches[2].Lobby; l == nil || l.Value != 2800+700 || l.Tier != "CHALLENGER" {
		t.Errorf("third game's lobby = %+v", l)
	}

	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/matches/NA1_5/lobby", nil))
	var lob MatchLobbyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &lob); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
	}
	if lob.Total != 8 || lob.Known != 7 || len(lob.Players) != 7 || lob.Average != 3300 || lob.AverageTier != "GRANDMASTER" {
		t.Errorf("match lobby = %+v, want 7 of 8 known at 500 LP, Grandmaster by na1's cutoffs", lob)
	}
	for _, p := range lob.Players {
		if p.PUUID == "me" || p.Tier != "MASTER" || p.LeaguePoints != 500 || p.Value != 3300 || p.Current {
			t.Errorf("player %+v, want an opponent at Master 500 from a snapshot near the game", p)
		}
	}
	rec = httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/matches/bad/lobby", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/matches/NA1_99999/lobby", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unstored match: got %d, want 404", rec.Code)
	}
}
