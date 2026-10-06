package apiserver

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"tft-platform/internal/comps"
	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
	"tft-platform/internal/suggest"
)

const (
	suggestMaxUnits = 15
	suggestMaxItems = 30
	// suggestBuildsPerUnit is wider than the Meta page's: a build is only
	// useful here if the inventory covers it, so there must be enough to
	// choose from.
	suggestBuildsPerUnit = 30
)

// handleExploreSuggest serves GET /api/v1/explore/suggest: what to build
// from the units and items a player holds right now.
//
//	set=18                  required
//	queue=1100, level=8-    scope of the boards the advice comes from
//	have_unit=ID            repeatable: units on the board and bench; with
//	                        none, the units the items fit best are suggested
//	have_item=ID            repeatable, once per copy: inventory, completed
//	                        items or components
func (s *Server) handleExploreSuggest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope := url.Values{}
	for _, k := range []string{"set", "queue", "level"} {
		scope[k] = q[k]
	}
	f, err := parseExploreFilter(scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	units, err := parseIDList(q["have_unit"], "have_unit", suggestMaxUnits)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	items, err := parseIDList(q["have_item"], "have_item", suggestMaxItems)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	if len(units) == 0 && len(items) == 0 {
		writeJSON(w, http.StatusOK, suggest.Advise(suggest.Input{}))
		return
	}

	ctx := r.Context()
	itemData, err := s.setItemData(ctx, f.Set)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	meta, err := s.meta.get(ctx, metaCacheKey("builds-wide", f), func(ctx context.Context) (any, error) {
		return s.Store.MetaBuilds(ctx, f, metaMinBuildGames, suggestBuildsPerUnit, metaItemsPerUnit)
	})
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	compList, err := s.metaComps(ctx, f)
	if err != nil {
		writeDBError(w, r, err)
		return
	}

	builds := map[string][]store.MetaBuild{}
	for _, u := range meta.(*store.MetaResult).Units {
		builds[u.ID] = u.Builds
	}
	writeJSON(w, http.StatusOK, suggest.Advise(suggest.Input{
		Units: units, Items: items, Recipes: itemData.recipes, EmblemTraits: itemData.emblemTraits,
		Enablers: itemData.enablers,
		Builds:   builds, Comps: compList.Comps,
	}))
}

// parseIDList validates repeated id parameters (duplicates are kept: an item
// held twice is listed twice).
func parseIDList(vals []string, name string, max int) ([]string, error) {
	if len(vals) > max {
		return nil, tooManyError(name, max)
	}
	for _, v := range vals {
		if !exploreID.MatchString(v) {
			return nil, invalidIDError(name, v)
		}
	}
	return vals, nil
}

type paramError string

func (e paramError) Error() string { return string(e) }

func tooManyError(name string, max int) error {
	return paramError("at most " + strconv.Itoa(max) + " " + name + " values")
}

func invalidIDError(name, v string) error {
	return paramError("invalid " + name + " " + strconv.Quote(v))
}

// setItems is what the advisor needs from the newest stored set data.
type setItems struct {
	recipes      suggest.Recipes   // completed item -> its two components
	emblemTraits map[string]string // emblem item -> the trait it grants
	enablers     map[string]bool   // emblems and artifacts
}

// setItemData returns the set's item recipes and emblem traits from the
// newest stored set data; both empty if none is stored yet, in which case
// only whole items can be matched and emblems match no board. An emblem's
// trait is the trait named like it without " Emblem" ("Brawler Emblem" ->
// Brawler), which holds for every Set 18 emblem.
func (s *Server) setItemData(ctx context.Context, set int) (setItems, error) {
	v, err := s.meta.get(ctx, "setitems|"+strconv.Itoa(set), func(ctx context.Context) (any, error) {
		snaps, err := setdata.LoadSnapshots(ctx, s.Store, set)
		if err != nil {
			return nil, err
		}
		out := setItems{recipes: suggest.Recipes{}, emblemTraits: map[string]string{}, enablers: map[string]bool{}}
		if len(snaps) == 0 {
			return out, nil
		}
		newest := snaps[len(snaps)-1]
		traitByName := map[string]string{}
		for _, t := range newest.Traits {
			traitByName[strings.ToLower(t.Name)] = t.APIName
		}
		for _, it := range newest.Items {
			if len(it.Composition) == 2 {
				out.recipes[it.APIName] = [2]string{it.Composition[0], it.Composition[1]}
			}
			if it.Kind == "emblem" || it.Kind == "artifact" {
				out.enablers[it.APIName] = true
			}
			if it.Kind == "emblem" {
				name := strings.TrimSuffix(strings.ToLower(it.Name), " emblem")
				if trait, ok := traitByName[name]; ok {
					out.emblemTraits[it.APIName] = trait
				}
			}
		}
		return out, nil
	})
	if err != nil {
		return setItems{}, err
	}
	return v.(setItems), nil
}

// metaComps computes the comps for scope f, shared (and cached) with the
// Meta page's comps endpoint.
func (s *Server) metaComps(ctx context.Context, f store.ExploreFilter) (MetaCompsResponse, error) {
	v, err := s.meta.get(ctx, metaCacheKey("comps", f), func(ctx context.Context) (any, error) {
		boards, err := s.Store.FinalBoards(ctx, f, compMinLevel)
		if err != nil {
			return nil, err
		}
		return MetaCompsResponse{Boards: len(boards), Comps: comps.Build(boards, comps.Options{})}, nil
	})
	if err != nil {
		return MetaCompsResponse{}, err
	}
	return v.(MetaCompsResponse), nil
}
