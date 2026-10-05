package riotapi

import "fmt"

// PlatformRegion identifies a specific game server (e.g. "na1"). It routes
// tft/summoner-v1 and tft/league-v1 calls.
type PlatformRegion string

// RoutingRegion identifies a continental routing value (e.g. "americas"). It
// routes account-v1 and tft/match-v1 calls — these are NOT platform-routed,
// which is a common source of bugs when calling the Riot API.
type RoutingRegion string

const (
	PlatformNA1  PlatformRegion = "na1"
	PlatformBR1  PlatformRegion = "br1"
	PlatformLA1  PlatformRegion = "la1"
	PlatformLA2  PlatformRegion = "la2"
	PlatformOC1  PlatformRegion = "oc1"
	PlatformEUW1 PlatformRegion = "euw1"
	PlatformEUN1 PlatformRegion = "eun1"
	PlatformTR1  PlatformRegion = "tr1"
	PlatformRU   PlatformRegion = "ru"
	PlatformKR   PlatformRegion = "kr"
	PlatformJP1  PlatformRegion = "jp1"
	PlatformVN2  PlatformRegion = "vn2"
	PlatformSG2  PlatformRegion = "sg2"

	RoutingAmericas RoutingRegion = "americas"
	RoutingEurope   RoutingRegion = "europe"
	RoutingAsia     RoutingRegion = "asia"
	RoutingSea      RoutingRegion = "sea"
)

var platformToRouting = map[PlatformRegion]RoutingRegion{
	PlatformNA1: RoutingAmericas,
	PlatformBR1: RoutingAmericas,
	PlatformLA1: RoutingAmericas,
	PlatformLA2: RoutingAmericas,
	PlatformOC1: RoutingAmericas,

	PlatformEUW1: RoutingEurope,
	PlatformEUN1: RoutingEurope,
	PlatformTR1:  RoutingEurope,
	PlatformRU:   RoutingEurope,

	PlatformKR:  RoutingAsia,
	PlatformJP1: RoutingAsia,

	PlatformVN2: RoutingSea,
	PlatformSG2: RoutingSea,
}

// RoutingForPlatform derives the correct regional routing host for a given
// platform, so callers only ever need to supply a PlatformRegion (from user
// input or the DB) and can't accidentally send account-v1/match-v1 calls to
// the wrong host.
func RoutingForPlatform(p PlatformRegion) (RoutingRegion, error) {
	r, ok := platformToRouting[p]
	if !ok {
		return "", fmt.Errorf("unknown platform region %q", p)
	}
	return r, nil
}

func platformHost(p PlatformRegion) string {
	return fmt.Sprintf("https://%s.api.riotgames.com", p)
}

func routingHost(r RoutingRegion) string {
	return fmt.Sprintf("https://%s.api.riotgames.com", r)
}
