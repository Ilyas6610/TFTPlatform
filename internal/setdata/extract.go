package setdata

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CommunityDragon's cdragon/tft/en_us.json, reduced to the fields used.
type cdFile struct {
	Items   []cdItem `json:"items"`
	SetData []cdSet  `json:"setData"`
}

type cdSet struct {
	Number    int          `json:"number"`
	Mutator   string       `json:"mutator"`
	Augments  []string     `json:"augments"`
	Items     []string     `json:"items"`
	Champions []cdChampion `json:"champions"`
	Traits    []cdTrait    `json:"traits"`
}

type cdChampion struct {
	APIName  string              `json:"apiName"`
	TileIcon string              `json:"tileIcon"`
	Name     string              `json:"name"`
	Cost     int                 `json:"cost"`
	Traits   []string            `json:"traits"`
	Stats    map[string]*float64 `json:"stats"`
	Ability  struct {
		Name      string `json:"name"`
		Desc      string `json:"desc"`
		Variables []struct {
			Name  string    `json:"name"`
			Value []float64 `json:"value"`
		} `json:"variables"`
	} `json:"ability"`
}

type cdTrait struct {
	APIName string `json:"apiName"`
	Icon    string `json:"icon"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Effects []struct {
		MinUnits  int                 `json:"minUnits"`
		MaxUnits  int                 `json:"maxUnits"`
		Style     int                 `json:"style"`
		Variables map[string]*float64 `json:"variables"`
	} `json:"effects"`
}

type cdItem struct {
	APIName          string              `json:"apiName"`
	Name             string              `json:"name"`
	Desc             string              `json:"desc"`
	Icon             string              `json:"icon"`
	Effects          map[string]*float64 `json:"effects"`
	Composition      []string            `json:"composition"`
	AssociatedTraits []string            `json:"associatedTraits"`
}

// mainMutator is the set's standard-mode setData entry; others are
// alternate modes (Pairs, Turbo, ...) sharing the number.
func mainMutator(set int) string { return fmt.Sprintf("TFTSet%d", set) }

var setMutator = regexp.MustCompile(`^TFTSet(\d+)$`)

// LatestSet returns the highest set number with a standard-mode entry in
// the export.
func LatestSet(raw []byte) (int, error) {
	var f struct {
		SetData []struct {
			Mutator string `json:"mutator"`
		} `json:"setData"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, fmt.Errorf("decode export: %w", err)
	}
	latest := 0
	for _, s := range f.SetData {
		if m := setMutator.FindStringSubmatch(s.Mutator); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > latest {
				latest = n
			}
		}
	}
	if latest == 0 {
		return 0, fmt.Errorf("no standard set found in export")
	}
	return latest, nil
}

// Extract builds the normalized SetData for set from a CommunityDragon
// export. Only shop units (cost 1-5 with traits) are kept, which drops
// neutral monsters, summons and other non-buyable characters.
func Extract(raw []byte, set int, version string) (*SetData, error) {
	var f cdFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("decode export: %w", err)
	}
	var cs *cdSet
	for i := range f.SetData {
		if f.SetData[i].Mutator == mainMutator(set) {
			cs = &f.SetData[i]
		}
	}
	if cs == nil {
		return nil, fmt.Errorf("set %d not in export", set)
	}

	items := make(map[string]cdItem, len(f.Items))
	for _, it := range f.Items {
		items[it.APIName] = it
	}
	traitNames := make(map[string]string, len(cs.Traits))
	for _, t := range cs.Traits {
		traitNames[t.APIName] = t.Name
	}

	out := &SetData{SetNumber: set, Version: version}
	patch := PatchOf(version)
	for _, c := range cs.Champions {
		if c.Cost < 1 || c.Cost > 5 || len(c.Traits) == 0 || c.Name == "" {
			continue
		}
		out.Units = append(out.Units, extractUnit(c, patch))
	}
	for _, t := range cs.Traits {
		if t.Name == "" {
			continue
		}
		out.Traits = append(out.Traits, extractTrait(t, patch))
	}
	for _, api := range cs.Augments {
		it, ok := items[api]
		if !ok || it.Name == "" {
			continue
		}
		out.Augments = append(out.Augments, extractAugment(it, traitNames, patch))
	}

	// Component = something a completed item in this set is built from.
	components := map[string]bool{}
	for _, api := range cs.Items {
		for _, c := range items[api].Composition {
			components[c] = true
		}
	}
	for _, api := range cs.Items {
		it, ok := items[api]
		if !ok || it.Name == "" || it.Desc == "" || isRewardPlaceholder(api) {
			continue
		}
		item := extractItem(it, components[api], patch)
		if item.Kind == "wisp" {
			out.Wisps = append(out.Wisps, item)
		} else {
			out.Items = append(out.Items, item)
		}
	}

	sort.Slice(out.Units, func(i, j int) bool {
		a, b := out.Units[i], out.Units[j]
		if a.Cost != b.Cost {
			return a.Cost < b.Cost
		}
		return a.Name < b.Name
	})
	sort.Slice(out.Traits, func(i, j int) bool { return out.Traits[i].Name < out.Traits[j].Name })
	sort.Slice(out.Augments, func(i, j int) bool {
		a, b := out.Augments[i], out.Augments[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.APIName < b.APIName
	})
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Name < out.Items[j].Name })
	sort.Slice(out.Wisps, func(i, j int) bool {
		a, b := out.Wisps[i], out.Wisps[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.APIName < b.APIName
	})
	return out, nil
}

func extractUnit(c cdChampion, patch string) Unit {
	u := Unit{
		APIName: c.APIName,
		Name:    c.Name,
		Icon:    iconURL(patch, c.TileIcon),
		Cost:    c.Cost,
		Traits:  c.Traits,
		Stats:   map[string]float64{},
		Ability: Ability{Name: c.Ability.Name, Values: map[string][]float64{}},
	}
	for k, v := range c.Stats {
		if v != nil {
			u.Stats[k] = roundTo(*v, 4)
		}
	}
	for _, v := range c.Ability.Variables {
		vals := make([]float64, len(v.Value))
		for i, x := range v.Value {
			vals[i] = roundTo(x, 4)
		}
		u.Ability.Values[v.Name] = vals
	}
	u.Ability.Desc = render(c.Ability.Desc, func(name string) (string, bool) {
		for k, vals := range u.Ability.Values {
			if strings.EqualFold(k, name) && len(vals) > 0 {
				return starValues(vals), true
			}
		}
		return "", false
	})
	return u
}

// starValues renders 1-3 star values as "a/b/c", or "a" if all equal.
// vals must be non-empty.
func starValues(vals []float64) string {
	if len(vals) < 4 {
		return formatNumber(vals[len(vals)-1])
	}
	stars := []string{formatNumber(vals[1]), formatNumber(vals[2]), formatNumber(vals[3])}
	if stars[0] == stars[1] && stars[1] == stars[2] {
		return stars[0]
	}
	return strings.Join(stars, "/")
}

var traitRow = regexp.MustCompile(`(?s)<row>(.*?)</row>`)

func extractTrait(t cdTrait, patch string) Trait {
	tr := Trait{APIName: t.APIName, Name: t.Name, Icon: iconURL(patch, t.Icon)}

	// Trait descriptions are an intro followed by one <row> per breakpoint,
	// each templated against that breakpoint's variables.
	rows := traitRow.FindAllStringSubmatch(t.Desc, -1)
	intro := t.Desc
	if loc := traitRow.FindStringIndex(t.Desc); loc != nil {
		intro = t.Desc[:loc[0]]
	}

	var firstValues map[string]float64
	for i, e := range t.Effects {
		bp := Breakpoint{
			MinUnits: e.MinUnits,
			MaxUnits: e.MaxUnits,
			Style:    e.Style,
			Values:   namedValues(e.Variables, t.Desc),
		}
		if i == 0 {
			firstValues = bp.Values
		}
		if i < len(rows) {
			withUnits := map[string]float64{"MinUnits": float64(e.MinUnits), "MaxUnits": float64(e.MaxUnits)}
			for k, v := range bp.Values {
				withUnits[k] = v
			}
			bp.Text = render(rows[i][1], lookupIn(withUnits))
		}
		tr.Breakpoints = append(tr.Breakpoints, bp)
	}
	// The intro's own variables (if any) are shared across breakpoints.
	tr.Desc = render(intro, lookupIn(firstValues))
	return tr
}

var augmentTier = regexp.MustCompile(`(?i)[-_](i{1,3}|t[1-3])\.(tex|png)$`)

func extractAugment(it cdItem, traitNames map[string]string, patch string) Augment {
	values := namedValues(it.Effects, it.Desc)
	a := Augment{
		APIName: it.APIName,
		Name:    it.Name,
		Icon:    iconURL(patch, it.Icon),
		Desc:    render(it.Desc, lookupIn(values)),
		Values:  values,
	}
	if m := augmentTier.FindStringSubmatch(it.Icon); m != nil {
		switch strings.ToLower(m[1]) {
		case "i", "t1":
			a.Tier = 1
		case "ii", "t2":
			a.Tier = 2
		case "iii", "t3":
			a.Tier = 3
		}
	}
	for _, t := range it.AssociatedTraits {
		if name, ok := traitNames[t]; ok {
			a.Traits = append(a.Traits, name)
		}
	}
	return a
}

// isRewardPlaceholder reports whether an item only describes a reward
// bundle ("2 gold", "3 Radiant Items") rather than being a real item.
func isRewardPlaceholder(api string) bool {
	return strings.HasPrefix(api, "TFT_Assist_") || strings.Contains(api, "ArmoryItem")
}

func extractItem(it cdItem, isComponent bool, patch string) Item {
	values := namedValues(it.Effects, it.Desc)
	item := Item{
		APIName:     it.APIName,
		Name:        it.Name,
		Icon:        iconURL(patch, it.Icon),
		Desc:        render(it.Desc, lookupIn(values)),
		Composition: it.Composition,
		Values:      values,
	}
	item.Kind, item.Variant = classifyItem(it.APIName, isComponent, len(it.Composition) == 2)
	return item
}

func classifyItem(api string, isComponent, isCompleted bool) (kind, variant string) {
	lower := strings.ToLower(api)
	switch {
	case isComponent:
		return "component", ""
	case isCompleted:
		return "completed", ""
	case strings.Contains(lower, "emblem"):
		return "emblem", ""
	case strings.Contains(lower, "artifact") || strings.Contains(lower, "ornn"):
		return "artifact", ""
	case strings.Contains(lower, "radiant"):
		return "radiant", ""
	case strings.Contains(lower, "consumable"):
		return "consumable", ""
	case strings.HasPrefix(api, "DA_"):
		// Set 18's remaining DA_* items are Wisps, its set mechanic.
		switch {
		case strings.Contains(lower, "_prismatic"):
			variant = "prismatic"
		case strings.Contains(lower, "_upgrade"):
			variant = "upgraded"
		case strings.Contains(lower, "_charm"):
			variant = "charm"
		}
		return "wisp", variant
	}
	return "other", ""
}

// iconURL converts a game asset path ("assets/ux/traiticons/x.tex") to its
// PNG on CommunityDragon, pinned to the snapshot's patch so old snapshots
// keep their icons. The UI prefers locally downloaded Data Dragon icons and
// uses this as the fallback.
func iconURL(patch, assetPath string) string {
	if assetPath == "" || !strings.HasSuffix(strings.ToLower(assetPath), ".tex") {
		return ""
	}
	p := strings.ToLower(strings.TrimSuffix(assetPath, assetPath[len(assetPath)-4:])) + ".png"
	return "https://raw.communitydragon.org/" + patch + "/game/" + p
}
