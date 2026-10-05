package setdata

import (
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// varLookup resolves a template variable name to its display text.
type varLookup func(name string) (string, bool)

var (
	// "@Name@", "@Name*100@" — the multiplier converts ratios to percents.
	templateVar = regexp.MustCompile(`@([A-Za-z0-9_.{}]+)(?:\*([0-9.]+))?@`)
	// One or more adjacent stat icons, e.g. "%i:scaleHealth%%i:scaleAP%".
	iconRun = regexp.MustCompile(`(?:%i:[A-Za-z0-9_]+%)+`)
	iconTag = regexp.MustCompile(`%i:([A-Za-z0-9_]+)%`)
	htmlTag = regexp.MustCompile(`<[^>]*>`)
	blanks  = regexp.MustCompile(`[ \t]+`)
	breaks  = regexp.MustCompile(`\n{3,}`)
)

// iconLabels names the stat icons that matter for reading a description
// ("deals 200 (AP) magic damage"); other icons are dropped.
var iconLabels = map[string]string{
	"scaleAP":        "AP",
	"scaleAD":        "AD",
	"scaleHealth":    "HP",
	"scaleArmor":     "Armor",
	"scaleMR":        "MR",
	"scaleAS":        "AS",
	"scaleMana":      "Mana",
	"scaleManaRegen": "Mana Regen",
	"scaleDR":        "Durability",
	"goldCoins":      "gold",
}

// render fills a CommunityDragon description template and reduces its
// markup to plain text with "\n" line breaks.
func render(tmpl string, lookup varLookup) string {
	s := templateVar.ReplaceAllStringFunc(tmpl, func(m string) string {
		parts := templateVar.FindStringSubmatch(m)
		text, ok := lookup(parts[1])
		if !ok {
			return "?"
		}
		if parts[2] != "" {
			mult, _ := strconv.ParseFloat(parts[2], 64)
			text = scaleText(text, mult)
		}
		return text
	})
	s = iconRun.ReplaceAllStringFunc(s, func(run string) string {
		var labels []string
		for _, m := range iconTag.FindAllStringSubmatch(run, -1) {
			if l, ok := iconLabels[m[1]]; ok {
				labels = append(labels, l)
			}
		}
		if len(labels) == 0 {
			return ""
		}
		return "(" + strings.Join(labels, ", ") + ")"
	})

	// Riot's text mixes real newlines, literal "\n" sequences and <br>.
	s = strings.NewReplacer("\r", "", `\n`, "\n", "<br>", "\n", "<br/>", "\n", "<br />", "\n").Replace(s)
	s = htmlTag.ReplaceAllString(s, "")
	s = blanks.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(breaks.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// scaleText multiplies every number in a rendered value ("0.25" or
// "0.1/0.2/0.3") by mult.
func scaleText(text string, mult float64) string {
	parts := strings.Split(text, "/")
	for i, p := range parts {
		if v, err := strconv.ParseFloat(p, 64); err == nil {
			parts[i] = formatNumber(v * mult)
		}
	}
	return strings.Join(parts, "/")
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(roundTo(v, 2), 'f', -1, 64)
}

func roundTo(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// varHash is CommunityDragon's key for a variable whose name it couldn't
// recover: FNV-1a (32-bit) of the lowercased name, e.g. "{b027c2f9}".
func varHash(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return fmt.Sprintf("{%08x}", h.Sum32())
}

// namedValues re-keys a CommunityDragon variables map by the readable names
// used in tmpl wherever a hashed key matches, so values (and their diffs)
// read "StonebarkTreeBonusHealth" instead of "{b027c2f9}". Null values are
// dropped.
func namedValues(raw map[string]*float64, templates ...string) map[string]float64 {
	byHash := map[string]string{}
	for _, t := range templates {
		for _, m := range templateVar.FindAllStringSubmatch(t, -1) {
			byHash[varHash(m[1])] = m[1]
		}
	}
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		if v == nil {
			continue
		}
		if name, ok := byHash[k]; ok {
			k = name
		}
		// Riot stores float32s; round off the noise (0.15000000596 -> 0.15).
		out[k] = roundTo(*v, 4)
	}
	return out
}

// lookupIn resolves template variables against values, case-insensitively.
func lookupIn(values map[string]float64) varLookup {
	return func(name string) (string, bool) {
		if v, ok := values[name]; ok {
			return formatNumber(v), true
		}
		for k, v := range values {
			if strings.EqualFold(k, name) {
				return formatNumber(v), true
			}
		}
		return "", false
	}
}
