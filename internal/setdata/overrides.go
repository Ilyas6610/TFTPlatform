package setdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"tft-platform/internal/store"
)

// Text overrides fill in descriptions Riot's exported data can't render.
// Set 18 moved champion spell math into its new client, so CommunityDragon's
// export has placeholders where ability numbers go ("[[Magic Damage]]").
// tactics.tools publishes fully rendered tooltips for the live patch (the
// same Riot templates with the numbers filled in); their terms don't
// restrict reuse and robots.txt allows it. Overrides are fetched with the
// periodic set data sync (one ~150 KB file), stored per set, and applied
// when serving — only to the snapshot they were fetched against, and only
// where our own text still has unknown values. Each replaced description
// is attributed in the API response and the UI.

// OverrideSource fetches rendered description templates for a set.
type OverrideSource struct {
	Name    string // shown as attribution, e.g. "tactics.tools"
	PageURL string // attribution link
	BaseURL string
	HTTP    *http.Client
}

func TacticsToolsSource() OverrideSource {
	return OverrideSource{
		Name:    "tactics.tools",
		PageURL: "https://tactics.tools/info/units",
		BaseURL: "https://ap.tft.tools",
		HTTP:    &http.Client{Timeout: time.Minute},
	}
}

// windowAssign matches the `window.<name> = ` assignments in tactics.tools'
// locale file; the ones ending in "i18n" hold apiName-keyed text.
var windowAssign = regexp.MustCompile(`window\.([A-Za-z0-9_]+)\s*=\s*`)

// Fetch returns apiName -> description template for set. tactics.tools
// keys a description as "<apiName>_desc".
func (s OverrideSource) Fetch(ctx context.Context, set int) (map[string]string, error) {
	url := fmt.Sprintf("%s/static/s%d/en.js", strings.TrimRight(s.BaseURL, "/"), set)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseLocaleFile(string(body))
}

func parseLocaleFile(js string) (map[string]string, error) {
	out := map[string]string{}
	for _, m := range windowAssign.FindAllStringSubmatchIndex(js, -1) {
		name := js[m[2]:m[3]]
		rest := js[m[1]:]
		if !strings.HasSuffix(name, "i18n") || !strings.HasPrefix(rest, "{") {
			continue
		}
		var obj map[string]string
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&obj); err != nil {
			return nil, fmt.Errorf("decode window.%s: %w", name, err)
		}
		for k, v := range obj {
			if api, ok := strings.CutSuffix(k, "_desc"); ok && v != "" {
				out[api] = v
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no descriptions found (format changed?)")
	}
	return out, nil
}

// SyncTextOverrides fetches src's descriptions for set and stores them
// against set's newest snapshot version. A failed fetch leaves the previous
// overrides in place.
func SyncTextOverrides(ctx context.Context, src OverrideSource, st *store.Store, set int) (int, error) {
	snaps, err := LoadSnapshots(ctx, st, set)
	if err != nil {
		return 0, err
	}
	if len(snaps) == 0 {
		return 0, fmt.Errorf("set %d has no snapshots to attach overrides to", set)
	}
	texts, err := src.Fetch(ctx, set)
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(texts)
	if err != nil {
		return 0, err
	}
	err = st.UpsertSetTextOverrides(ctx, store.SetTextOverrides{
		SetNumber: set,
		Source:    src.Name,
		Version:   snaps[len(snaps)-1].Version,
		Data:      data,
	})
	return len(texts), err
}

// TextOverrides is one source's descriptions, as loaded for serving.
type TextOverrides struct {
	Source  string
	PageURL string
	Version string
	Texts   map[string]string
}

// knownSourcePages maps a stored source name to its attribution link.
var knownSourcePages = map[string]string{"tactics.tools": TacticsToolsSource().PageURL}

// LoadTextOverrides returns set's stored overrides from every source.
func LoadTextOverrides(ctx context.Context, st *store.Store, set int) ([]TextOverrides, error) {
	rows, err := st.ListSetTextOverrides(ctx, set)
	if err != nil {
		return nil, err
	}
	out := make([]TextOverrides, 0, len(rows))
	for _, r := range rows {
		var texts map[string]string
		if err := json.Unmarshal(r.Data, &texts); err != nil {
			return nil, fmt.Errorf("decode %s overrides: %w", r.Source, err)
		}
		out = append(out, TextOverrides{Source: r.Source, PageURL: knownSourcePages[r.Source], Version: r.Version, Texts: texts})
	}
	return out, nil
}

// AttachTextOverrides replaces descriptions that still contain unknown
// values with an override's rendered text, when the override was fetched
// for d's version and itself renders without unknowns.
func AttachTextOverrides(d *SetData, overrides []TextOverrides) {
	for _, o := range overrides {
		if o.Version != d.Version {
			continue
		}
		apply := func(apiName string, desc *string, source *DescSource) {
			// Only text that's missing (Set 18 items have none in the
			// export) or still has unknown values gets replaced.
			if *desc != "" && !strings.Contains(*desc, unknownOpen) {
				return
			}
			tmpl, ok := o.Texts[apiName]
			if !ok {
				return
			}
			rendered := render(tmpl, func(string) (string, bool) { return "", false })
			if strings.Contains(rendered, unknownOpen) {
				return
			}
			*desc = rendered
			*source = DescSource{Name: o.Source, URL: o.PageURL}
		}
		for i := range d.Units {
			u := &d.Units[i]
			apply(u.APIName, &u.Ability.Desc, &u.Ability.DescSource)
		}
		for i := range d.Augments {
			apply(d.Augments[i].APIName, &d.Augments[i].Desc, &d.Augments[i].DescSource)
		}
		for i := range d.Items {
			apply(d.Items[i].APIName, &d.Items[i].Desc, &d.Items[i].DescSource)
		}
		for i := range d.Wisps {
			apply(d.Wisps[i].APIName, &d.Wisps[i].Desc, &d.Wisps[i].DescSource)
		}
	}
}
