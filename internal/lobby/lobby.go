// Package lobby rates how strong a player's opponents were in a game, from
// the opponents' Ranked standings around it (store.LobbyPlayers). Pure
// logic.
package lobby

import (
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
