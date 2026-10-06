package lp

import (
	"testing"
	"time"

	"tft-platform/internal/store"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func snap(queue, tier, div string, lp, wins, losses int, at time.Duration) store.RankSnapshot {
	return store.RankSnapshot{QueueType: queue, Tier: tier, Rank: div, LeaguePoints: lp, Wins: wins, Losses: losses, FetchedAt: t0.Add(at)}
}

func game(id string, queue int, at time.Duration) store.TimedGame {
	return store.TimedGame{MatchID: id, QueueID: queue, GameDatetime: t0.Add(at)}
}

func TestValue_PromotionAndApex(t *testing.T) {
	d1 := Value(store.RankSnapshot{Tier: "DIAMOND", Rank: "I", LeaguePoints: 90})
	m := Value(store.RankSnapshot{Tier: "MASTER", Rank: "I", LeaguePoints: 20})
	if m-d1 != 30 {
		t.Errorf("Diamond I 90 -> Master 20 = %+d, want +30", m-d1)
	}
	gm := Value(store.RankSnapshot{Tier: "GRANDMASTER", Rank: "I", LeaguePoints: 520})
	if gm-m != 500 {
		t.Errorf("Master 20 -> Grandmaster 520 = %+d, want +500 (one apex ladder)", gm-m)
	}
	if Value(store.RankSnapshot{Tier: "GOLD", Rank: "II", LeaguePoints: 10})-Value(store.RankSnapshot{Tier: "GOLD", Rank: "III", LeaguePoints: 95}) != 15 {
		t.Error("division promotion should difference across the boundary")
	}
}

func TestAttribute(t *testing.T) {
	snaps := []store.RankSnapshot{
		snap("RANKED_TFT", "MASTER", "I", 100, 10, 10, 0),
		snap("RANKED_TFT", "MASTER", "I", 135, 11, 10, time.Hour),   // one game: +35
		snap("RANKED_TFT", "MASTER", "I", 120, 12, 12, 3*time.Hour), // three games: -15 total
		snap("RANKED_TFT", "MASTER", "I", 90, 12, 13, 5*time.Hour),  // one game, not stored: unknown
		snap("RANKED_TFT_DOUBLE_UP", "GOLD", "I", 50, 3, 0, 0),
		snap("RANKED_TFT_DOUBLE_UP", "PLATINUM", "IV", 10, 4, 0, time.Hour), // +60 over a promotion
	}
	games := []store.TimedGame{
		game("A", 1100, 30*time.Minute),
		game("B", 1100, 90*time.Minute),
		game("N", 1090, 100*time.Minute), // normal game: never counted
		game("C", 1100, 2*time.Hour),
		game("D", 1100, 150*time.Minute),
		game("DU", 1160, 20*time.Minute),
	}
	got := Attribute(snaps, games)
	want := map[string]Change{
		"A":  {Delta: 35, Games: 1},
		"D":  {Delta: -15, Games: 3},
		"DU": {Delta: 60, Games: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %+v, want %+v", id, got[id], w)
		}
	}
}

func TestEstimateBefore(t *testing.T) {
	first := snap("RANKED_TFT", "MASTER", "I", 100, 10, 10, 3*time.Hour) // 2900
	games := []store.TimedGame{
		{MatchID: "old-set", QueueID: 1100, SetNumber: 17, Placement: 1, GameDatetime: t0},
		{MatchID: "A", QueueID: 1100, SetNumber: 18, Placement: 1, GameDatetime: t0.Add(time.Hour)},
		{MatchID: "N", QueueID: 1090, SetNumber: 18, Placement: 8, GameDatetime: t0.Add(90 * time.Minute)},
		{MatchID: "B", QueueID: 1100, SetNumber: 18, Placement: 8, GameDatetime: t0.Add(2 * time.Hour)},
		{MatchID: "after", QueueID: 1100, SetNumber: 18, Placement: 1, GameDatetime: t0.Add(4 * time.Hour)},
	}
	got := EstimateBefore(first, games, 18)
	// After B: 2900 (matches the snapshot); before B (-40) = after A: 2940.
	if len(got) != 2 || got[0].MatchID != "A" || got[0].Value != 2940 || got[1].MatchID != "B" || got[1].Value != 2900 {
		t.Fatalf("got %+v", got)
	}
}

// A real sequence: the apex ladder's snapshot (687, 373 games) was fetched
// seconds after a by-puuid one that already counted a game more (654, 374
// games), and the server's clock was behind, so snapshots "fetched" before
// a game started already counted it. Game counts still line up.
func TestAttribute_StaleLadderAndClockSkew(t *testing.T) {
	at := func(hm string) time.Time {
		v, _ := time.Parse("15:04", hm)
		return time.Date(2026, 10, 6, v.Hour(), v.Minute(), 0, 0, time.UTC)
	}
	s := func(lp, wins, losses int, fetched string) store.RankSnapshot {
		return store.RankSnapshot{QueueType: "RANKED_TFT", Tier: "CHALLENGER", Rank: "I", LeaguePoints: lp, Wins: wins, Losses: losses, FetchedAt: at(fetched)}
	}
	snaps := []store.RankSnapshot{
		s(667, 197, 174, "04:31"),
		s(654, 199, 175, "06:28"),
		s(687, 199, 174, "06:29"), // stale ladder copy
		s(640, 200, 176, "09:32"),
	}
	g := func(id, start string, mins int) store.TimedGame {
		return store.TimedGame{MatchID: id, QueueID: 1100, GameDatetime: at(start), GameLength: time.Duration(mins) * time.Minute}
	}
	games := []store.TimedGame{
		g("before", "03:01", 36),
		g("g1", "05:25", 40), g("g2", "05:59", 35), g("g3", "06:38", 39), g("g4", "07:18", 40), g("g5", "07:57", 36),
	}
	got := Attribute(snaps, games)
	want := map[string]Change{
		"g2": {Delta: 20, Games: 2},  // 667 -> 687 over g1, g2
		"g3": {Delta: -33, Games: 1}, // 687 -> 654
		"g5": {Delta: -14, Games: 2}, // 654 -> 640 over g4, g5
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %+v, want %+v", id, got[id], w)
		}
	}
}

func TestAttribute_NewSeason(t *testing.T) {
	snaps := []store.RankSnapshot{
		snap("RANKED_TFT", "MASTER", "I", 300, 100, 100, 0),
		snap("RANKED_TFT", "IRON", "IV", 0, 0, 0, 24*time.Hour), // reset
		snap("RANKED_TFT", "IRON", "IV", 40, 1, 0, 26*time.Hour),
	}
	games := []store.TimedGame{game("old", 1100, -time.Hour), game("new", 1100, 25*time.Hour)}
	got := Attribute(snaps, games)
	if len(got) != 1 || got["new"] != (Change{Delta: 40, Games: 1}) {
		t.Errorf("got %+v, want only the new season's game at +40", got)
	}
}
