package sessions

import (
	"testing"
	"time"

	"tft-platform/internal/store"
)

func TestAnalyze(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var games []store.TimedGame
	// Back-to-back 35-minute games (5 minutes between); a 2-hour break
	// starts a new session.
	at := t0
	play := func(placements ...int) {
		for _, p := range placements {
			games = append(games, store.TimedGame{GameDatetime: at, GameLength: 35 * time.Minute, Placement: p})
			at = at.Add(40 * time.Minute)
		}
	}
	play(1, 6, 7, 8, 2, 3)
	at = at.Add(2 * time.Hour)
	play(5, 4)

	r := Analyze(games)
	if r.Sessions != 2 || r.Longest != 6 || r.AvgGames != 4 {
		t.Errorf("sessions = %d, longest %d, avg %v; want 2, 6, 4", r.Sessions, r.Longest, r.AvgGames)
	}
	// Game 1: 1 and 5; game 2: 6 and 4; 3: 7; 4: 8; 5+: 2, 3.
	wantPos := map[int][2]float64{1: {2, 3}, 2: {2, 5}, 3: {1, 7}, 4: {1, 8}, 5: {2, 2.5}}
	if len(r.ByPosition) != 5 {
		t.Fatalf("byPosition = %+v", r.ByPosition)
	}
	for _, p := range r.ByPosition {
		if w := wantPos[p.Game]; float64(p.Boards) != w[0] || p.AvgPlacement != w[1] {
			t.Errorf("game %d = %d games at %v, want %v", p.Game, p.Boards, p.AvgPlacement, w)
		}
	}
	// After a top 4 (1, 2, ...): 6, 3. After a bottom 4 (6, 7, 8 | 5): 7, 8, 2, 4.
	// After two bottom 4s (6+7, 7+8): 8, 2. A new session's first game follows nothing.
	if r.AfterTop4.Boards != 2 || r.AfterTop4.AvgPlacement != 4.5 {
		t.Errorf("after top 4 = %+v", r.AfterTop4)
	}
	if r.AfterBottom4.Boards != 4 || r.AfterBottom4.AvgPlacement != 5.25 {
		t.Errorf("after bottom 4 = %+v", r.AfterBottom4)
	}
	if r.AfterTwoBottom4.Boards != 2 || r.AfterTwoBottom4.AvgPlacement != 5 {
		t.Errorf("after two bottom 4s = %+v", r.AfterTwoBottom4)
	}
}

func TestAnalyze_Empty(t *testing.T) {
	r := Analyze(nil)
	if r.Sessions != 0 || r.ByPosition == nil || len(r.ByPosition) != 0 {
		t.Errorf("got %+v", r)
	}
}
