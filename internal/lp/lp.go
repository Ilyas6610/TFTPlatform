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

// clockSlack is how far a snapshot's fetch time may be from when Riot's
// record actually stood (our clock against Riot's game times, Riot's
// ladder caching, games that just ended). Games ending within it of a
// snapshot may or may not be counted in it; game counts decide.
const clockSlack = time.Hour

// seasonDrop is how many fewer games a later snapshot may count and still
// be a stale copy (the apex ladder lags the by-puuid lookup) rather than a
// new season's reset record.
const seasonDrop = 5

// Attribute maps match ids to LP changes. snaps may mix queues; games are a
// player's stored games (any queue). Games outside snapshot coverage, or in
// spans where the stored games don't add up to the record's game count
// (some not stored), get no entry.
//
// Snapshots are ordered by games played (wins + losses), not fetch time:
// a ladder snapshot can be older than a by-puuid one fetched seconds
// before it. Each snapshot is then placed in the player's stored ranked
// games by its game count, anchored to the games that ended before it was
// fetched (within clockSlack), so a span covers exactly as many stored
// games as Riot's record grew by.
func Attribute(snaps []store.RankSnapshot, games []store.TimedGame) map[string]Change {
	out := map[string]Change{}
	byQueue := map[string][]store.RankSnapshot{}
	for _, s := range snaps {
		byQueue[s.QueueType] = append(byQueue[s.QueueType], s)
	}
	for queue, qs := range byQueue {
		var ranked []store.TimedGame
		for _, g := range games {
			if QueueType(g.QueueID) == queue {
				ranked = append(ranked, g)
			}
		}
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].End().Before(ranked[j].End()) })
		for _, season := range seasons(qs) {
			attributeSeason(season, ranked, out)
		}
	}
	return out
}

// seasons splits one queue's snapshots where the game count resets (a new
// season), each ordered by games played, then fetch time.
func seasons(qs []store.RankSnapshot) [][]store.RankSnapshot {
	sort.SliceStable(qs, func(i, j int) bool { return qs[i].FetchedAt.Before(qs[j].FetchedAt) })
	var out [][]store.RankSnapshot
	var cur []store.RankSnapshot
	most := 0
	for _, s := range qs {
		if len(cur) > 0 && s.Games() < most-seasonDrop {
			out, cur, most = append(out, cur), nil, 0
		}
		cur = append(cur, s)
		most = max(most, s.Games())
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	for _, season := range out {
		sort.SliceStable(season, func(i, j int) bool {
			if season[i].Games() != season[j].Games() {
				return season[i].Games() < season[j].Games()
			}
			return season[i].FetchedAt.Before(season[j].FetchedAt)
		})
	}
	return out
}

// attributeSeason places a season's snapshots (ordered by games played) in
// ranked (stored games of the queue, by end time) and records each span's
// change on its newest game.
//
// A snapshot counting c games sits at position P = c + K: after the first
// P stored games, for one offset K shared along a run of snapshots. The
// time evidence bounds each position to the games that ended within
// clockSlack of the fetch; a run extends while some K satisfies every
// snapshot in it. A snapshot that fits no K with the run (a game in
// between isn't stored) starts a new run, and the span into it is unknown.
func attributeSeason(season []store.RankSnapshot, ranked []store.TimedGame, out map[string]Change) {
	n := len(season)
	if n < 2 {
		return
	}
	// Riot's record can't count a game before it happened: a snapshot
	// counting more games stood no earlier than one counting fewer, so
	// fetch times are made monotonic from the newest back.
	at := make([]time.Time, n)
	at[n-1] = season[n-1].FetchedAt
	for i := n - 2; i >= 0; i-- {
		at[i] = season[i].FetchedAt
		if at[i+1].Before(at[i]) {
			at[i] = at[i+1]
		}
	}
	endedBy := func(t time.Time) int {
		return sort.Search(len(ranked), func(i int) bool { return ranked[i].End().After(t) })
	}
	// Per snapshot: the exact position by time, and the K range it allows.
	exact := make([]int, n)
	lo, hi := make([]int, n), make([]int, n)
	for i, s := range season {
		exact[i] = endedBy(at[i])
		lo[i] = endedBy(at[i].Add(-clockSlack)) - s.Games()
		hi[i] = min(endedBy(at[i].Add(clockSlack)), len(ranked)) - s.Games()
	}

	for start := 0; start < n; {
		kLo, kHi := lo[start], hi[start]
		end := start + 1
		for ; end < n; end++ {
			nLo, nHi := max(kLo, lo[end]), min(kHi, hi[end])
			if nLo > nHi {
				break
			}
			kLo, kHi = nLo, nHi
		}
		if k, ok := bestOffset(season[start:end], exact[start:end], kLo, kHi); ok {
			for i := start + 1; i < end; i++ {
				from, to := season[i-1].Games()+k, season[i].Games()+k
				if to <= from || from < 0 || to > len(ranked) {
					continue
				}
				out[ranked[to-1].MatchID] = Change{Delta: Value(season[i]) - Value(season[i-1]), Games: to - from}
			}
		}
		start = end
	}
}

// bestOffset picks the K in [kLo, kHi] that puts most snapshots exactly
// where their fetch time says, then the closest overall. A tie is
// ambiguous: no K.
func bestOffset(run []store.RankSnapshot, exact []int, kLo, kHi int) (int, bool) {
	if len(run) < 2 {
		return 0, false
	}
	best, bestHits, bestDev, tie := 0, -1, 0, false
	for k := kLo; k <= kHi; k++ {
		hits, dev := 0, 0
		for i, s := range run {
			d := s.Games() + k - exact[i]
			if d == 0 {
				hits++
			}
			dev += max(d, -d)
		}
		switch {
		case hits > bestHits || (hits == bestHits && dev < bestDev):
			best, bestHits, bestDev, tie = k, hits, dev, false
		case hits == bestHits && dev == bestDev:
			tie = true
		}
	}
	return best, bestHits >= 0 && !tie
}

// typicalLP is a rough LP change per placement in Ranked (index 0 = 1st).
// Real gains depend on hidden MMR (+30..+60 for a win, more or less per
// player), so this only sketches the shape of a past climb.
var typicalLP = [8]int{40, 30, 20, 10, -10, -20, -30, -40}

// EstimatedPoint is an estimated standing right after a past game.
type EstimatedPoint struct {
	MatchID      string    `json:"matchId"`
	GameDatetime time.Time `json:"gameDatetime"`
	Placement    int       `json:"placement"`
	Value        int       `json:"value"` // on Value's linear scale
}

// EstimateBefore walks back from the first real snapshot through the
// stored Ranked games before it (same set, oldest first in games), undoing
// a typical LP change per placement, so the line ends exactly at first.
// It returns the estimated standing after each of those games, oldest
// first; nil when there's nothing before the first snapshot.
func EstimateBefore(first store.RankSnapshot, games []store.TimedGame, set int) []EstimatedPoint {
	var before []store.TimedGame
	for _, g := range games {
		if g.QueueID == 1100 && g.SetNumber == set && g.End().Before(first.FetchedAt) {
			before = append(before, g)
		}
	}
	out := make([]EstimatedPoint, len(before))
	v := Value(first)
	for i := len(before) - 1; i >= 0; i-- {
		g := before[i]
		out[i] = EstimatedPoint{MatchID: g.MatchID, GameDatetime: g.GameDatetime, Placement: g.Placement, Value: v}
		if g.Placement >= 1 && g.Placement <= 8 {
			v -= typicalLP[g.Placement-1]
		}
		if v < 0 {
			v = 0
		}
	}
	return out
}
