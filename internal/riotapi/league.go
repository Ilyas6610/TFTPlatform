package riotapi

import (
	"context"
	"fmt"
)

type LeagueEntry struct {
	PUUID        string `json:"puuid"`
	SummonerID   string `json:"summonerId"`
	LeaguePoints int    `json:"leaguePoints"`
	Wins         int    `json:"wins"`
	Losses       int    `json:"losses"`
	HotStreak    bool   `json:"hotStreak"`
	Rank         string `json:"rank,omitempty"`
}

type LeagueList struct {
	Tier    string        `json:"tier"`
	Entries []LeagueEntry `json:"entries"`
}

// GetChallenger, GetGrandmaster, and GetMaster fetch the apex TFT ranked
// leaderboards via the platform-routed tft/league-v1 endpoint. These are the
// PUUID discovery source used to seed the ingestion queue (see
// internal/ingest/seeder.go) — apex players get crawled first since they
// carry the highest information density for meta stats.
func (c *Client) GetChallenger(ctx context.Context, platform PlatformRegion) (*LeagueList, error) {
	return c.getLeagueList(ctx, platform, "challenger")
}

func (c *Client) GetGrandmaster(ctx context.Context, platform PlatformRegion) (*LeagueList, error) {
	return c.getLeagueList(ctx, platform, "grandmaster")
}

func (c *Client) GetMaster(ctx context.Context, platform PlatformRegion) (*LeagueList, error) {
	return c.getLeagueList(ctx, platform, "master")
}

func (c *Client) getLeagueList(ctx context.Context, platform PlatformRegion, tier string) (*LeagueList, error) {
	u := fmt.Sprintf("%s/tft/league/v1/%s", platformHost(platform), tier)

	var list LeagueList
	if err := c.do(ctx, fmt.Sprintf("tft-league-v1.get-%s", tier), u, &list); err != nil {
		return nil, err
	}
	if list.Tier == "" {
		list.Tier = tier
	}
	return &list, nil
}

// RankedEntry is a player's standing in one ranked queue.
type RankedEntry struct {
	QueueType    string `json:"queueType"` // RANKED_TFT, RANKED_TFT_DOUBLE_UP, ...
	Tier         string `json:"tier"`      // IRON .. CHALLENGER
	Rank         string `json:"rank"`      // I .. IV (I for apex tiers)
	LeaguePoints int    `json:"leaguePoints"`
	Wins         int    `json:"wins"`
	Losses       int    `json:"losses"`
}

// GetRankedEntries fetches a player's current rank in every ranked queue
// they've played this set via the platform-routed tft/league-v1 by-puuid
// endpoint. Unranked players get an empty list.
func (c *Client) GetRankedEntries(ctx context.Context, platform PlatformRegion, puuid string) ([]RankedEntry, error) {
	seg, err := pathSegment(puuid)
	if err != nil {
		return nil, err
	}
	var entries []RankedEntry
	u := fmt.Sprintf("%s/tft/league/v1/by-puuid/%s", platformHost(platform), seg)
	if err := c.do(ctx, "tft-league-v1.get-by-puuid", u, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
