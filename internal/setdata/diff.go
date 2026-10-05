package setdata

import (
	"fmt"
	"sort"
	"strings"
)

// Change is one line of generated patch notes.
type Change struct {
	Category string `json:"category"` // "unit", "trait", "augment", "item" or "wisp"
	APIName  string `json:"apiName"`
	Name     string `json:"name"`
	// Kind is "changed" for a value that moved, "added"/"removed" for a
	// whole entity, "renamed" (Field holds the old name), or "text" for a
	// description reworded without a tracked number changing.
	Kind  string   `json:"kind"`
	Field string   `json:"field,omitempty"` // e.g. "hp", "cost", "(5) StonebarkTreeBonusHealth"
	Old   *float64 `json:"old,omitempty"`
	New   *float64 `json:"new,omitempty"`
}

// Diff returns what changed from old to new, grouped by category (units,
// traits, augments, items, wisps) and then by name.
func Diff(old, new *SetData) []Change {
	var out []Change
	out = append(out, diffEntities("unit", toEntities(old.Units, unitEntity), toEntities(new.Units, unitEntity))...)
	out = append(out, diffEntities("trait", toEntities(old.Traits, traitEntity), toEntities(new.Traits, traitEntity))...)
	out = append(out, diffEntities("augment", toEntities(old.Augments, augmentEntity), toEntities(new.Augments, augmentEntity))...)
	out = append(out, diffEntities("item", toEntities(old.Items, itemEntity), toEntities(new.Items, itemEntity))...)
	out = append(out, diffEntities("wisp", toEntities(old.Wisps, itemEntity), toEntities(new.Wisps, itemEntity))...)
	return out
}

// entity is the comparable view of a unit/trait/augment/item: its tracked
// numbers by field name, plus its text for reword detection.
type entity struct {
	apiName string
	name    string
	numbers map[string]float64
	text    string
}

func toEntities[T any](xs []T, f func(T) entity) map[string]entity {
	out := make(map[string]entity, len(xs))
	for _, x := range xs {
		e := f(x)
		out[e.apiName] = e
	}
	return out
}

func unitEntity(u Unit) entity {
	nums := map[string]float64{"cost": float64(u.Cost)}
	for k, v := range u.Stats {
		nums[k] = v
	}
	for k, vals := range u.Ability.Values {
		for star := 1; star < len(vals) && star <= 3; star++ {
			nums[fmt.Sprintf("%s (%d★)", k, star)] = vals[star]
		}
	}
	return entity{u.APIName, u.Name, nums, u.Ability.Desc + "\n" + strings.Join(u.Traits, ",")}
}

func traitEntity(t Trait) entity {
	nums := map[string]float64{}
	text := t.Desc
	for i, bp := range t.Breakpoints {
		// Breakpoints are identified by position; their unit counts are
		// tracked numbers so "(5) → (4)" shows up as a change.
		nums[fmt.Sprintf("breakpoint %d units", i+1)] = float64(bp.MinUnits)
		for k, v := range bp.Values {
			nums[fmt.Sprintf("(%d) %s", bp.MinUnits, k)] = v
		}
		text += "\n" + bp.Text
	}
	return entity{t.APIName, t.Name, nums, text}
}

func augmentEntity(a Augment) entity {
	nums := map[string]float64{"tier": float64(a.Tier)}
	for k, v := range a.Values {
		nums[k] = v
	}
	return entity{a.APIName, a.Name, nums, a.Desc}
}

func itemEntity(it Item) entity {
	return entity{it.APIName, it.Name, it.Values, it.Desc}
}

// pairEntities matches new entities to old ones: by apiName first (so a
// rename is a change, not an add + remove), then — among what's left — by
// name when the name is unique on both sides. The second pass catches Riot
// swapping an entity's id between patches (16.18
// TFT_Augment_ExpectedUnexpectedness -> 16.19 DA_ExpectedUnexpectedness).
// It returns new apiName -> old apiName, plus which pairs were id swaps.
func pairEntities(old, new map[string]entity) (pairs map[string]string, swapped map[string]bool) {
	pairs, swapped = map[string]string{}, map[string]bool{}
	pairedOld := map[string]bool{}
	for api := range new {
		if _, ok := old[api]; ok {
			pairs[api] = api
			pairedOld[api] = true
		}
	}
	byName := func(m map[string]entity, skip func(string) bool) map[string][]string {
		out := map[string][]string{}
		for api, e := range m {
			if !skip(api) {
				out[augmentKey(e.name)] = append(out[augmentKey(e.name)], api)
			}
		}
		return out
	}
	oldLeft := byName(old, func(api string) bool { return pairedOld[api] })
	newLeft := byName(new, func(api string) bool { _, ok := pairs[api]; return ok })
	for name, news := range newLeft {
		if olds := oldLeft[name]; len(news) == 1 && len(olds) == 1 {
			pairs[news[0]] = olds[0]
			swapped[news[0]] = true
		}
	}
	return pairs, swapped
}

func diffEntities(category string, old, new map[string]entity) []Change {
	pairs, swapped := pairEntities(old, new)
	pairedOld := map[string]bool{}
	for _, o := range pairs {
		pairedOld[o] = true
	}

	var out []Change
	for _, api := range sortedKeysByName(new) {
		n := new[api]
		oldAPI, existed := pairs[api]
		if !existed {
			out = append(out, Change{Category: category, APIName: n.apiName, Name: n.name, Kind: "added"})
			continue
		}
		o := old[oldAPI]
		if o.name != n.name {
			out = append(out, Change{Category: category, APIName: n.apiName, Name: n.name, Kind: "renamed", Field: o.name})
		}
		numberChanged := false
		for _, field := range sortedKeys(n.numbers, o.numbers) {
			ov, inOld := o.numbers[field]
			nv, inNew := n.numbers[field]
			// Across an id swap the two objects define different fields;
			// only values both define are comparable.
			if swapped[api] && !(inOld && inNew) {
				continue
			}
			if inOld && inNew && roundTo(ov, 4) == roundTo(nv, 4) {
				continue
			}
			c := Change{Category: category, APIName: n.apiName, Name: n.name, Kind: "changed", Field: field}
			if inOld {
				c.Old = &ov
			}
			if inNew {
				c.New = &nv
			}
			out = append(out, c)
			numberChanged = true
		}
		// Rendered text embeds the numbers, so only report a reword when
		// no number explains it.
		if !numberChanged && o.text != n.text {
			out = append(out, Change{Category: category, APIName: n.apiName, Name: n.name, Kind: "text"})
		}
	}
	for _, api := range sortedKeysByName(old) {
		if !pairedOld[api] {
			o := old[api]
			out = append(out, Change{Category: category, APIName: o.apiName, Name: o.name, Kind: "removed"})
		}
	}
	return out
}

// sortedKeysByName returns m's keys ordered by entity name, then apiName.
func sortedKeysByName(m map[string]entity) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := m[keys[i]], m[keys[j]]
		if a.name != b.name {
			return a.name < b.name
		}
		return a.apiName < b.apiName
	})
	return keys
}

func sortedKeys(maps ...map[string]float64) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}
