package lobby

import (
	"testing"

	"tft-platform/internal/store"
)

func ranked(tier, div string, lp int) *store.RankSnapshot {
	return &store.RankSnapshot{QueueType: "RANKED_TFT", Tier: tier, Rank: div, LeaguePoints: lp}
}

func TestFor(t *testing.T) {
	var ps []store.LobbyPlayer
	add := func(match string, queue int, puuid string, placement int, r *store.RankSnapshot, current bool) {
		ps = append(ps, store.LobbyPlayer{MatchID: match, QueueID: queue, PUUID: puuid, Placement: placement, Rank: r, Current: current})
	}
	// Ranked: 4 of 7 opponents known (Master 100/300 = 2900/3100, Diamond I 50 = 2750, Challenger 900 = 3700).
	add("R", 1100, "me", 3, nil, false) // my own rank never counts
	add("R", 1100, "a", 1, ranked("MASTER", "I", 100), false)
	add("R", 1100, "b", 2, ranked("MASTER", "I", 300), true)
	add("R", 1100, "c", 4, ranked("DIAMOND", "I", 50), false)
	add("R", 1100, "d", 5, ranked("CHALLENGER", "I", 900), false)
	for i, p := range []string{"e", "f", "g"} {
		add("R", 1100, p, 6+i, nil, false)
	}
	// Double Up: my teammate (placements 3-4) isn't an opponent; 3 of 6 known.
	add("D", 1160, "me", 3, nil, false)
	add("D", 1160, "mate", 4, ranked("CHALLENGER", "I", 2000), false)
	add("D", 1160, "x", 1, ranked("MASTER", "I", 0), false)
	add("D", 1160, "y", 2, ranked("MASTER", "I", 0), false)
	add("D", 1160, "z", 5, ranked("MASTER", "I", 300), false)
	for i, p := range []string{"u", "v", "w"} {
		add("D", 1160, p, 6+i, nil, false)
	}
	// Too few known: 3 of 7.
	add("T", 1100, "me", 1, nil, false)
	for i, p := range []string{"a", "b", "c"} {
		add("T", 1100, p, 2+i, ranked("MASTER", "I", 0), false)
	}
	for i, p := range []string{"d", "e", "f", "g"} {
		add("T", 1100, p, 5+i, nil, false)
	}
	// A match without me.
	add("X", 1100, "a", 1, ranked("MASTER", "I", 0), false)

	got := For(ps, "me")
	if len(got) != 2 {
		t.Fatalf("got %+v, want R and D", got)
	}
	if r := got["R"]; r != (Strength{Value: (2900 + 3100 + 2750 + 3700) / 4, Known: 4, Current: 1, Opponents: 7}) {
		t.Errorf("R = %+v", r)
	}
	if d := got["D"]; d != (Strength{Value: (2800 + 2800 + 3100) / 3, Known: 3, Opponents: 6}) {
		t.Errorf("D = %+v (the teammate must not count)", d)
	}
}
