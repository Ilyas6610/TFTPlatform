package apiserver

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"tft-platform/internal/store"
)

const (
	exploreMaxConds = 6  // per kind (units, items, traits)
	exploreLimit    = 30 // rows per breakdown
)

// Riot ids: letters, digits and underscores ("DA_18_Akali_AD").
var exploreID = regexp.MustCompile(`^[A-Za-z0-9_]{1,80}$`)

// handleExplore serves GET /api/v1/explore: placement stats for player
// boards matching every condition, plus what else those boards ran.
//
//	set=18                     required
//	queue=1100                 repeatable; omitted = any queue
//	level=7-10                 player level range (either end optional: "8-", "-7")
//	unit=ID[*minStar][:I1,I2]  repeatable; e.g. DA_18_Akali_AD*2:DA_InfinityEdge
//	item=ID                    repeatable; item anywhere on the board
//	trait=ID[*min[-max]]       repeatable; units counted, e.g. DA_18_Elderwood*5
//	                           (5+) or DA_Juggernaut18*4-5 (exactly tier 2)
func (s *Server) handleExplore(w http.ResponseWriter, r *http.Request) {
	f, err := parseExploreFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	res, err := s.Store.Explore(r.Context(), f, exploreLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleExploreOptions serves GET /api/v1/explore/options?set=18: the
// queues, units, items, traits and levels present in that set's matches.
func (s *Server) handleExploreOptions(w http.ResponseWriter, r *http.Request) {
	set, err := strconv.Atoi(r.URL.Query().Get("set"))
	if err != nil || set <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_set", "set must be a positive number")
		return
	}
	opts, err := s.Store.ExploreOptions(r.Context(), set)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func parseExploreFilter(q url.Values) (store.ExploreFilter, error) {
	var f store.ExploreFilter
	set, err := strconv.Atoi(q.Get("set"))
	if err != nil || set <= 0 {
		return f, fmt.Errorf("set must be a positive number")
	}
	f.Set = set

	for _, v := range q["queue"] {
		id, err := strconv.Atoi(v)
		if err != nil || id < 0 {
			return f, fmt.Errorf("invalid queue %q", v)
		}
		f.Queues = append(f.Queues, id)
	}

	if v := q.Get("level"); v != "" {
		lo, hi, ok := strings.Cut(v, "-")
		if !ok {
			lo, hi = v, v
		}
		var errLo, errHi error
		if lo != "" {
			f.LevelMin, errLo = parseBounded(lo, 1, 10)
		}
		if hi != "" {
			f.LevelMax, errHi = parseBounded(hi, 1, 10)
		}
		if errLo != nil || errHi != nil || (f.LevelMin > 0 && f.LevelMax > 0 && f.LevelMin > f.LevelMax) {
			return f, fmt.Errorf("invalid level range %q", v)
		}
	}

	if len(q["unit"]) > exploreMaxConds || len(q["item"]) > exploreMaxConds || len(q["trait"]) > exploreMaxConds {
		return f, fmt.Errorf("at most %d unit, item and trait conditions each", exploreMaxConds)
	}
	for _, v := range q["unit"] {
		spec, items, _ := strings.Cut(v, ":")
		id, star, hasStar := strings.Cut(spec, "*")
		u := store.UnitCond{ID: id}
		if !exploreID.MatchString(id) {
			return f, fmt.Errorf("invalid unit %q", id)
		}
		if hasStar {
			if u.MinStar, err = parseBounded(star, 1, 4); err != nil {
				return f, fmt.Errorf("invalid star level in %q", v)
			}
		}
		if items != "" {
			for _, it := range strings.Split(items, ",") {
				if !exploreID.MatchString(it) {
					return f, fmt.Errorf("invalid item %q", it)
				}
				u.Items = append(u.Items, it)
			}
			if len(u.Items) > 3 {
				return f, fmt.Errorf("a unit holds at most 3 items")
			}
		}
		f.Units = append(f.Units, u)
	}
	for _, it := range q["item"] {
		if !exploreID.MatchString(it) {
			return f, fmt.Errorf("invalid item %q", it)
		}
		f.Items = append(f.Items, it)
	}
	for _, v := range q["trait"] {
		id, n, hasN := strings.Cut(v, "*")
		t := store.TraitCond{ID: id}
		if !exploreID.MatchString(id) {
			return f, fmt.Errorf("invalid trait %q", id)
		}
		if hasN {
			lo, hi, isRange := strings.Cut(n, "-")
			if t.MinUnits, err = parseBounded(lo, 1, 20); err != nil {
				return f, fmt.Errorf("invalid unit count in %q", v)
			}
			if isRange && hi != "" {
				if t.MaxUnits, err = parseBounded(hi, t.MinUnits, 20); err != nil {
					return f, fmt.Errorf("invalid unit count range in %q", v)
				}
			}
		}
		f.Traits = append(f.Traits, t)
	}
	return f, nil
}

func parseBounded(s string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%q not in %d-%d", s, lo, hi)
	}
	return n, nil
}
