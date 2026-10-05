package riotapi

import (
	"context"
	"fmt"
)

type Account struct {
	PUUID    string `json:"puuid"`
	GameName string `json:"gameName"`
	TagLine  string `json:"tagLine"`
}

// GetAccountByRiotID resolves a Riot ID (gameName#tagLine) to a PUUID via the
// region-routed (NOT platform-routed) account-v1 endpoint.
func (c *Client) GetAccountByRiotID(ctx context.Context, routing RoutingRegion, gameName, tagLine string) (*Account, error) {
	name, err := pathSegment(gameName)
	if err != nil {
		return nil, err
	}
	tag, err := pathSegment(tagLine)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/riot/account/v1/accounts/by-riot-id/%s/%s", routingHost(routing), name, tag)

	var account Account
	if err := c.do(ctx, "account-v1.get-by-riot-id", u, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// GetAccountByPUUID resolves a PUUID back to its current Riot ID. Used to
// backfill names for PUUIDs discovered without one (leaderboard seeding,
// match participants).
func (c *Client) GetAccountByPUUID(ctx context.Context, routing RoutingRegion, puuid string) (*Account, error) {
	seg, err := pathSegment(puuid)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/riot/account/v1/accounts/by-puuid/%s", routingHost(routing), seg)

	var account Account
	if err := c.do(ctx, "account-v1.get-by-puuid", u, &account); err != nil {
		return nil, err
	}
	return &account, nil
}
