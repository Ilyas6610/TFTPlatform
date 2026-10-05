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
	// whole entity, or "text" for a description reworded without a tracked
	// number changing.
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

func diffEntities(category string, old, new map[string]entity) []Change {
	var out []Change
	for _, n := range sortedByName(new) {
		o, existed := old[n.apiName]
		if !existed {
			out = append(out, Change{Category: category, APIName: n.apiName, Name: n.name, Kind: "added"})
			continue
		}
		numberChanged := false
		for _, field := range sortedKeys(n.numbers, o.numbers) {
			ov, inOld := o.numbers[field]
			nv, inNew := n.numbers[field]
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
	for _, o := range sortedByName(old) {
		if _, ok := new[o.apiName]; !ok {
			out = append(out, Change{Category: category, APIName: o.apiName, Name: o.name, Kind: "removed"})
		}
	}
	return out
}

func sortedByName(m map[string]entity) []entity {
	out := make([]entity, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].apiName < out[j].apiName
	})
	return out
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
