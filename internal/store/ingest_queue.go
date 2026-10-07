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
// still making organic progress through everyone else. Players backing off
// after a failed crawl (MarkCrawlFailed) are skipped until they're due.
func (s *Store) NextQueueBatch(ctx context.Context, limit int) ([]QueuedPUUID, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT puuid, platform_region, routing_region, priority
		FROM ingest_puuid_queue
		WHERE next_attempt_at IS NULL OR next_attempt_at <= now()
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
// the next NextQueueBatch call naturally rotates to other queued players. It
// also ends any failure backoff.
func (s *Store) MarkCrawled(ctx context.Context, puuid, lastMatchIDSeen string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE ingest_puuid_queue
		SET last_crawled_at = now(), last_match_id_seen = $2,
			failures = 0, last_error = NULL, next_attempt_at = NULL
		WHERE puuid = $1
	`, puuid, nullIfEmpty(lastMatchIDSeen))
	return err
}

// crawlBackoffMax caps how long a failing player is left alone.
const crawlBackoffMax = "24 hours"

// MarkCrawlFailed records a failed crawl of puuid and schedules the next
// attempt with exponential backoff (2, 4, 8, ... minutes, at most a day), so
// one persistently failing player can't hold the head of the queue.
// last_crawled_at is left alone: it also says when the history was last
// synced, which a failure doesn't change.
func (s *Store) MarkCrawlFailed(ctx context.Context, puuid, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := s.Pool.Exec(ctx, `
		UPDATE ingest_puuid_queue
		SET failures = LEAST(failures + 1, 30), last_error = $2,
			next_attempt_at = now() + LEAST(interval '1 minute' * power(2, LEAST(failures + 1, 30)), interval '`+crawlBackoffMax+`')
		WHERE puuid = $1
	`, puuid, reason)
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
