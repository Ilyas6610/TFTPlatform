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
