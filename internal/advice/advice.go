// Package advice compares one player's results with everyone's in the same
// scope, to point at what to change: units they do worse with than their
// usual, and builds that everyone does better with than the one they use.
// Pure logic; the inputs are store.MetaBuilds results.
package advice

import (
	"math"
	"slices"
	"sort"

	"tft-platform/internal/store"
)

// Options are the evidence thresholds.
type Options struct {
	// MinUnitGames: a unit needs this many of the player's games.
	MinUnitGames int
	// MinBuildGames: the player's build needs this many of their games.
	MinBuildGames int
	// MinMetaGames: everyone's figures need this many boards.
	MinMetaGames int
	// UnitGap and BuildGap: the smallest difference in average placement
	// worth a note.
	UnitGap, BuildGap float64
	// Z: the difference must also be this many standard errors, so a few
	// lucky or unlucky games don't make a note.
	Z float64
	// Max notes per list.
	Max int
}

var Defaults = Options{MinUnitGames: 5, MinBuildGames: 3, MinMetaGames: 30, UnitGap: 0.5, BuildGap: 0.3, Z: 1.5, Max: 3}

// placementSD is the standard deviation of a placement spread evenly over
// 1-8 (sqrt(63/12)); real spreads are close to it.
const placementSD = 2.29

// se is the standard error of an average placement over n games.
func se(n int) float64 { return placementSD / math.Sqrt(float64(max(n, 1))) }

// UnitNote is a unit the player does notably worse or better with than
// expected. Expected is everyone's average with the unit shifted by the
// player's usual edge (their average minus everyone's), so a strong player
// isn't told every unit is fine and a weak one that every unit is bad.
type UnitNote struct {
	Unit     string  `json:"unit"`
	Games    int     `json:"games"`
	Avg      float64 `json:"avg"`
	MetaAvg  float64 `json:"metaAvg"`
	Expected float64 `json:"expected"`
}

// BuildNote is the player's usual build on a unit next to a build everyone
// does better with. Both sides are everyone's results, so the comparison
// doesn't depend on the player's own skill.
type BuildNote struct {
	Unit   string          `json:"unit"`
	Theirs store.MetaBuild `json:"theirs"` // the player's games with it
	// TheirsMeta is everyone's games with the player's build.
	TheirsMeta store.PlacementStats `json:"theirsMeta"`
	Better     store.MetaBuild      `json:"better"` // everyone's games
}

type Advice struct {
	// Edge is the player's average placement minus everyone's (negative =
	// better than average).
	Edge   float64     `json:"edge"`
	Weak   []UnitNote  `json:"weak"`
	Strong []UnitNote  `json:"strong"`
	Builds []BuildNote `json:"builds"`
}

// Build compares player (MetaBuilds over the player's boards) with meta
// (everyone's, same scope; builds should be wide enough to include the
// player's). playerAvg and metaAvg are the overall average placements.
func Build(player, meta *store.MetaResult, playerAvg, metaAvg float64, o Options) Advice {
	a := Advice{Edge: playerAvg - metaAvg, Weak: []UnitNote{}, Strong: []UnitNote{}, Builds: []BuildNote{}}
	everyone := map[string]*store.MetaUnit{}
	for i := range meta.Units {
		everyone[meta.Units[i].ID] = &meta.Units[i]
	}

	type scored struct {
		note  UnitNote
		score float64 // z: positive = worse than expected
	}
	var notes []scored
	for _, u := range player.Units {
		m := everyone[u.ID]
		if u.Boards < o.MinUnitGames || m == nil || m.Boards < o.MinMetaGames {
			continue
		}
		expected := m.AvgPlacement + a.Edge
		diff := u.AvgPlacement - expected
		// Everyone's average is far better sampled; the player's games are
		// the noise.
		z := diff / se(u.Boards)
		if math.Abs(diff) < o.UnitGap || math.Abs(z) < o.Z {
			continue
		}
		notes = append(notes, scored{UnitNote{Unit: u.ID, Games: u.Boards, Avg: u.AvgPlacement, MetaAvg: m.AvgPlacement, Expected: expected}, z})
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].score > notes[j].score })
	for _, s := range notes {
		if s.score > 0 && len(a.Weak) < o.Max {
			a.Weak = append(a.Weak, s.note)
		}
	}
	for i := len(notes) - 1; i >= 0; i-- {
		if notes[i].score < 0 && len(a.Strong) < o.Max {
			a.Strong = append(a.Strong, notes[i].note)
		}
	}

	// Builds: the player's most played build per unit (most played units
	// first) against everyone's best well-sampled build for that unit.
	for _, u := range player.Units {
		if len(a.Builds) >= o.Max {
			break
		}
		m := everyone[u.ID]
		if len(u.Builds) == 0 || m == nil {
			continue
		}
		theirs := u.Builds[0]
		if theirs.Boards < o.MinBuildGames {
			continue
		}
		var theirsMeta *store.MetaBuild
		for i := range m.Builds {
			if slices.Equal(m.Builds[i].Items, theirs.Items) {
				theirsMeta = &m.Builds[i]
			}
		}
		if theirsMeta == nil {
			continue // too rare for everyone's figures
		}
		// Another well-sampled build that's clearly better (by the gap and
		// by the standard error of the difference), best by its pessimistic
		// average so a thin sample's luck doesn't win.
		var better *store.MetaBuild
		pessimistic := func(b *store.MetaBuild) float64 { return b.AvgPlacement + o.Z*se(b.Boards) }
		for i := range m.Builds {
			b := &m.Builds[i]
			gap := theirsMeta.AvgPlacement - b.AvgPlacement
			noise := math.Hypot(se(b.Boards), se(theirsMeta.Boards))
			if b == theirsMeta || b.Boards < o.MinMetaGames || gap < o.BuildGap || gap < o.Z*noise {
				continue
			}
			if better == nil || pessimistic(b) < pessimistic(better) {
				better = b
			}
		}
		if better == nil {
			continue
		}
		a.Builds = append(a.Builds, BuildNote{Unit: u.ID, Theirs: theirs, TheirsMeta: theirsMeta.PlacementStats, Better: *better})
	}
	return a
}
