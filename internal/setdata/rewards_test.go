package setdata

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRewards_EmbeddedSet18Loads(t *testing.T) {
	r := rewardsBySet[18]["DA_MagicRoll"]
	if r == nil {
		t.Fatal("expected Magic Roll rewards for set 18")
	}
	if r.SourceName != "Little Buddy Bot" || !strings.HasPrefix(r.SourceURL, "https://www.littlebuddybot.com/") || r.Updated == "" {
		t.Errorf("expected cited source and date, got %+v", r)
	}

}

func rewardsFS(body string) fstest.MapFS {
	return fstest.MapFS{"rewards/x.json": {Data: []byte(body)}}
}

func TestLoadRewards_Validation(t *testing.T) {
	cases := map[string]string{
		"row/column mismatch": `{"set":18,"source":{"name":"S"},"augments":[{"apiNames":["A"],"sourceUrl":"u","tables":[{"title":"t","columns":["Odds","Reward"],"rows":[["1%"]]}]}]}`,
		"duplicate apiName":   `{"set":18,"source":{"name":"S"},"augments":[{"apiNames":["A","A"],"sourceUrl":"u","tables":[{"title":"t","columns":["R"],"rows":[["x"]]}]}]}`,
		"missing source url":  `{"set":18,"source":{"name":"S"},"augments":[{"apiNames":["A"],"tables":[{"title":"t","columns":["R"],"rows":[["x"]]}]}]}`,
		"missing source name": `{"set":18,"source":{},"augments":[]}`,
		"bad json":            `{`,
	}
	for name, body := range cases {
		if _, err := loadRewards(rewardsFS(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAttachRewards(t *testing.T) {
	d := &SetData{SetNumber: 18, Augments: []Augment{{APIName: "DA_MagicRoll"}, {APIName: "DA_SomethingElse"}}}
	AttachRewards(d)
	if d.Augments[0].Rewards == nil || d.Augments[1].Rewards != nil {
		t.Errorf("expected rewards only on Magic Roll, got %+v", d.Augments)
	}

	other := &SetData{SetNumber: 17, Augments: []Augment{{APIName: "DA_MagicRoll"}}}
	AttachRewards(other)
	if other.Augments[0].Rewards != nil {
		t.Error("rewards are per set; set 17 must not get set 18's tables")
	}
}
