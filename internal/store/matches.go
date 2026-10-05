package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Match struct {
	MatchID       string
	RoutingRegion string
	GameDatetime  time.Time
	GameLength    float64
	GameVersion   string
	TFTSetNumber  int
	QueueID       int
	TFTGameType   string
	RawPayload    []byte
}

type MatchParticipant struct {
	PUUID                string
	Placement            int
	Level                int
	LastRound            int
	PlayersEliminated    int
	TotalDamageToPlayers int
	GoldLeft             int
	TimeEliminated       float64
	Units                json.RawMessage
	Traits               json.RawMessage
	Augments             json.RawMessage
	Companion            json.RawMessage
	RawParticipant       json.RawMessage
}

// ExistingMatchIDs returns the subset of ids already present in matches, so
// the crawler can dedupe a whole page of fetched match IDs in one query
// instead of one round trip per ID.
func (s *Store) ExistingMatchIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT match_id FROM matches WHERE match_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		existing[id] = true
	}
	return existing, rows.Err()
}

// InsertMatchWithParticipants stores a match and all of its participants in
// a single transaction, so a mid-crawl crash (or a rate-limit stop between
// matches) never leaves a match half-written.
func (s *Store) InsertMatchWithParticipants(ctx context.Context, m Match, participants map[string]MatchParticipant) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO matches (match_id, routing_region, game_datetime, game_length, game_version, tft_set_number, queue_id, tft_game_type, raw_payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (match_id) DO NOTHING
	`, m.MatchID, m.RoutingRegion, m.GameDatetime, m.GameLength, m.GameVersion, m.TFTSetNumber, m.QueueID, m.TFTGameType, m.RawPayload)
	if err != nil {
		return err
	}

	for puuid, p := range participants {
		_, err = tx.Exec(ctx, `
			INSERT INTO match_participants (
				match_id, puuid, placement, level, last_round, players_eliminated,
				total_damage_to_players, gold_left, time_eliminated, units, traits, augments, companion, raw_participant
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (match_id, puuid) DO NOTHING
		`, m.MatchID, puuid, p.Placement, p.Level, p.LastRound, p.PlayersEliminated,
			p.TotalDamageToPlayers, p.GoldLeft, p.TimeEliminated, p.Units, p.Traits, p.Augments, p.Companion, p.RawParticipant)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetMatchRaw returns (nil, "", false, nil) on a cache miss, not an error.
func (s *Store) GetMatchRaw(ctx context.Context, matchID string) (raw []byte, routingRegion string, found bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT raw_payload, routing_region FROM matches WHERE match_id = $1`, matchID).
		Scan(&raw, &routingRegion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	return raw, routingRegion, true, nil
}
