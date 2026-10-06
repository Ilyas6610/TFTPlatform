// Package lp works out how much LP each ranked game gained or lost. Riot's
// match data has no LP, so it compares rank snapshots (tier, division, LP,
// wins + losses) taken before and after games: when exactly one stored
// ranked game falls between two snapshots, its change is exact; when
// several do, the change covers all of them.
package lp

import (
	"sort"
	"time"

	"tft-platform/internal/store"
)

// Ranked queues and their league queue types.
var queueTypes = map[int]string{
	1100: "RANKED_TFT",
	1160: "RANKED_TFT_DOUBLE_UP",
}

// QueueType returns the league queue a match queue counts toward, or "" for
// unranked queues.
func QueueType(queueID int) string { return queueTypes[queueID] }

var tierBase = map[string]int{
	"IRON": 0, "BRONZE": 400, "SILVER": 800, "GOLD": 1200,
	"PLATINUM": 1600, "EMERALD": 2000, "DIAMOND": 2400,
	// Apex tiers share one LP ladder: Grandmaster and Challenger are LP
	// thresholds above Master, not separate divisions.
	"MASTER": 2800, "GRANDMASTER": 2800, "CHALLENGER": 2800,
}

var divisionBase = map[string]int{"IV": 0, "III": 100, "II": 200, "I": 300}

// Value places a standing on one linear scale (100 LP per division), so a
// promotion or demotion still differences correctly.
func Value(r store.RankSnapshot) int {
	v := tierBase[r.Tier] + r.LeaguePoints
	if r.Tier != "MASTER" && r.Tier != "GRANDMASTER" && r.Tier != "CHALLENGER" {
		v += divisionBase[r.Rank]
	}
	return v
}

// Change is a game's LP change.
type Change struct {
	Delta int `json:"delta"`
	// Games is how many games Delta covers: 1 for an exact per-game change,
	// more when several games were played between two snapshots (the
	// change is then shown on the newest of them).
	Games int `json:"games"`
}

// Attribute maps match ids to LP changes. snaps may mix queues; games are a
// player's stored games (any queue). Games outside snapshot coverage, or in
// spans where the stored games don't add up to the record's game count
// (some not stored), get no entry.
func Attribute(snaps []store.RankSnapshot, games []store.TimedGame) map[string]Change {
	out := map[string]Change{}
	byQueue := map[string][]store.RankSnapshot{}
	for _, s := range snaps {
		byQueue[s.QueueType] = append(byQueue[s.QueueType], s)
	}
	for queue, qs := range byQueue {
		sort.SliceStable(qs, func(i, j int) bool { return qs[i].FetchedAt.Before(qs[j].FetchedAt) })
		var ranked []store.TimedGame
		for _, g := range games {
			if QueueType(g.QueueID) == queue {
				ranked = append(ranked, g)
			}
		}
		for i := 1; i < len(qs); i++ {
			before, after := qs[i-1], qs[i]
			played := after.Games() - before.Games()
			if played <= 0 {
				continue // no games between (or a reset): nothing to attribute
			}
			between := gamesIn(ranked, before.FetchedAt, after.FetchedAt)
			if len(between) != played {
				continue // a game isn't stored (or not yet counted): unknown
			}
			newest := between[len(between)-1]
			out[newest.MatchID] = Change{Delta: Value(after) - Value(before), Games: played}
		}
	}
	return out
}

// gamesIn returns games that ended in (from, to].
func gamesIn(games []store.TimedGame, from, to time.Time) []store.TimedGame {
	var out []store.TimedGame
	for _, g := range games {
		if g.GameDatetime.After(from) && !g.GameDatetime.After(to) {
			out = append(out, g)
		}
	}
	return out
}
