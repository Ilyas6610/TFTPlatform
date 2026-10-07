// Package lobby rates how strong a player's opponents were in a game, from
// the opponents' Ranked standings around it (store.LobbyPlayers), and
// splits a player's games by that. Pure logic.
package lobby

import (
	"sort"

	"tft-platform/internal/lp"
	"tft-platform/internal/store"
)

// doubleUpQueue: teammates (a consecutive placement pair) aren't opponents.
const doubleUpQueue = 1160

// Strength is a player's lobby in one game: the average Ranked standing of
// the opponents whose rank is known, on lp.Value's scale.
type Strength struct {
	Value     int `json:"value"`
	Known     int `json:"known"`     // opponents with a known rank
	Current   int `json:"current"`   // of those, ranked by today's ladder rather than near the game
	Opponents int `json:"opponents"` // 7, or 6 in Double Up
}

// For rates puuid's lobby in each match of players (LobbyPlayers rows for
// those matches). A match is left out when under half the opponents'
// ranks are known: the average of a few would say little.
func For(players []store.LobbyPlayer, puuid string) map[string]Strength {
	byMatch := map[string][]store.LobbyPlayer{}
	for _, p := range players {
		byMatch[p.MatchID] = append(byMatch[p.MatchID], p)
	}
	out := map[string]Strength{}
	for id, ps := range byMatch {
		var me *store.LobbyPlayer
		for i := range ps {
			if ps[i].PUUID == puuid {
				me = &ps[i]
			}
		}
		if me == nil {
			continue
		}
		var s Strength
		sum := 0
		for _, p := range ps {
			if p.PUUID == puuid || (p.QueueID == doubleUpQueue && (p.Placement+1)/2 == (me.Placement+1)/2) {
				continue
			}
			s.Opponents++
			if p.Rank == nil {
				continue
			}
			s.Known++
			sum += lp.Value(*p.Rank)
			if p.Current {
				s.Current++
			}
		}
		if s.Known == 0 || s.Known*2 < s.Opponents {
			continue
		}
		s.Value = sum / s.Known
		out[id] = s
	}
	return out
}

// Game is one of a player's games with a rated lobby.
type Game struct {
	Lobby     int // Strength.Value
	Placement int
}

// Third is a player's results in a third of their games, by lobby strength.
type Third struct {
	Label string `json:"label"` // easiest, middle, toughest
	// Lobby is the average lobby value of the third's games; Min and Max
	// bound them.
	Lobby    int `json:"lobby"`
	MinLobby int `json:"minLobby"`
	MaxLobby int `json:"maxLobby"`
	store.PlacementStats
}

// MinGames is how many rated games Thirds needs (three per third).
const MinGames = 9

// Thirds splits games into easiest, middle and toughest thirds by lobby
// strength (a ranking among the player's own lobbies, so it means the same
// at any rank) with the results in each. Nil under MinGames games.
func Thirds(games []Game) []Third {
	if len(games) < MinGames {
		return nil
	}
	sorted := append([]Game(nil), games...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Lobby < sorted[j].Lobby })
	labels := []string{"easiest", "middle", "toughest"}
	out := make([]Third, 0, 3)
	for k, label := range labels {
		part := sorted[k*len(sorted)/3 : (k+1)*len(sorted)/3]
		t := Third{Label: label, MinLobby: part[0].Lobby, MaxLobby: part[len(part)-1].Lobby}
		var lobbySum, placeSum, top4, wins int
		for _, g := range part {
			lobbySum += g.Lobby
			placeSum += g.Placement
			if g.Placement <= 4 {
				top4++
			}
			if g.Placement == 1 {
				wins++
			}
		}
		n := len(part)
		t.Lobby = lobbySum / n
		t.Boards = n
		t.AvgPlacement = float64(placeSum) / float64(n)
		t.Top4Rate = float64(top4) / float64(n)
		t.WinRate = float64(wins) / float64(n)
		out = append(out, t)
	}
	return out
}
