// Package sessions groups a player's games into play sessions and looks
// for patterns in them: how results change over a session and after bad
// games (tilt). Pure logic over stored games.
package sessions

import (
	"time"

	"tft-platform/internal/store"
)

// Gap ends a session: the next game starting more than this after the
// previous one ended (queueing takes a few minutes).
const Gap = 30 * time.Minute

// maxPosition: games from the 5th on share one bucket.
const maxPosition = 5

// Position is the stats of the games played Nth in a session.
type Position struct {
	Game int `json:"game"` // 1..5; 5 = 5th and later
	store.PlacementStats
}

// Result is a player's session patterns.
type Result struct {
	Sessions int     `json:"sessions"`
	AvgGames float64 `json:"avgGames"` // games per session
	Longest  int     `json:"longest"`  // games in the longest session
	// ByPosition is results by game number in the session.
	ByPosition []Position `json:"byPosition"`
	// The next game in the same session after a top 4, after a bottom 4,
	// and after two bottom 4s in a row.
	AfterTop4       store.PlacementStats `json:"afterTop4"`
	AfterBottom4    store.PlacementStats `json:"afterBottom4"`
	AfterTwoBottom4 store.PlacementStats `json:"afterTwoBottom4"`
}

// acc builds PlacementStats.
type acc struct {
	n, top4, wins int
	sum           float64
}

func (a *acc) add(placement int) {
	a.n++
	a.sum += float64(placement)
	if placement <= 4 {
		a.top4++
	}
	if placement == 1 {
		a.wins++
	}
}

func (a acc) stats() store.PlacementStats {
	if a.n == 0 {
		return store.PlacementStats{}
	}
	n := float64(a.n)
	return store.PlacementStats{Boards: a.n, AvgPlacement: a.sum / n, Top4Rate: float64(a.top4) / n, WinRate: float64(a.wins) / n}
}

// Analyze finds sessions in games (oldest first).
func Analyze(games []store.TimedGame) Result {
	var pos [maxPosition]acc
	var afterTop, afterBottom, afterTwo acc
	var res Result
	sessionGames, total := 0, 0
	for i, g := range games {
		newSession := i == 0 || g.GameDatetime.Sub(games[i-1].End()) > Gap
		if newSession {
			if sessionGames > 0 {
				res.Sessions++
				res.Longest = max(res.Longest, sessionGames)
			}
			sessionGames = 0
		}
		sessionGames++
		total++
		pos[min(sessionGames, maxPosition)-1].add(g.Placement)
		if newSession {
			continue
		}
		prev := games[i-1].Placement
		if prev <= 4 {
			afterTop.add(g.Placement)
		} else {
			afterBottom.add(g.Placement)
			if sessionGames >= 3 && games[i-2].Placement > 4 {
				afterTwo.add(g.Placement)
			}
		}
	}
	if sessionGames > 0 {
		res.Sessions++
		res.Longest = max(res.Longest, sessionGames)
	}
	if res.Sessions > 0 {
		res.AvgGames = float64(total) / float64(res.Sessions)
	}
	res.ByPosition = []Position{}
	for i, a := range pos {
		if a.n > 0 {
			res.ByPosition = append(res.ByPosition, Position{Game: i + 1, PlacementStats: a.stats()})
		}
	}
	res.AfterTop4, res.AfterBottom4, res.AfterTwoBottom4 = afterTop.stats(), afterBottom.stats(), afterTwo.stats()
	return res
}
