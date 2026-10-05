package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnqueuePUUID adds or updates a PUUID's entry in the crawl queue. Priority
// only ever increases (GREATEST) so re-seeding never downgrades a player
// already queued at a higher priority from an earlier seed; last_crawled_at
// is left untouched so an existing queue position's crawl progress isn't reset.
func (s *Store) EnqueuePUUID(ctx context.Context, puuid, platformRegion, routingRegion string, priority int16) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO ingest_puuid_queue (puuid, platform_region, routing_region, priority)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (puuid) DO UPDATE SET
			priority = GREATEST(ingest_puuid_queue.priority, EXCLUDED.priority)
	`, puuid, platformRegion, routingRegion, priority)
	return err
}

type QueuedPUUID struct {
	PUUID          string
	PlatformRegion string
	RoutingRegion  string
	Priority       int16
}

// NextQueueBatch returns up to limit PUUIDs ordered by priority (highest
// first), then by longest-since-crawled (never-crawled first) — the crawl
// order that gives apex-tier players' matches priority for meta stats while
// still making organic progress through everyone else.
func (s *Store) NextQueueBatch(ctx context.Context, limit int) ([]QueuedPUUID, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT puuid, platform_region, routing_region, priority
		FROM ingest_puuid_queue
		ORDER BY priority DESC, last_crawled_at NULLS FIRST
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []QueuedPUUID
	for rows.Next() {
		var q QueuedPUUID
		if err := rows.Scan(&q.PUUID, &q.PlatformRegion, &q.RoutingRegion, &q.Priority); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// MarkCrawled updates queue bookkeeping after a PUUID has been crawled, so
// the next NextQueueBatch call naturally rotates to other queued players.
func (s *Store) MarkCrawled(ctx context.Context, puuid, lastMatchIDSeen string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE ingest_puuid_queue
		SET last_crawled_at = now(), last_match_id_seen = $2
		WHERE puuid = $1
	`, puuid, nullIfEmpty(lastMatchIDSeen))
	return err
}

// MatchHistorySyncedAt returns when puuid's match history was last fully
// crawled (by the ingestion pipeline or a profile view), or nil if never.
func (s *Store) MatchHistorySyncedAt(ctx context.Context, puuid string) (*time.Time, error) {
	var t *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT last_crawled_at FROM ingest_puuid_queue WHERE puuid = $1`, puuid).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}
