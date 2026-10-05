package store

import "context"

// StartIngestRun records the start of a bounded ingestion batch (a single
// ingestcli invocation or worker cycle). runType is a free-form label such
// as "crawl_matches" or "seed_leaderboard".
func (s *Store) StartIngestRun(ctx context.Context, runType string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO ingest_runs (run_type) VALUES ($1) RETURNING id`, runType).Scan(&id)
	return id, err
}

// FinishIngestRun closes out a run with its outcome. status is one of
// "completed", "rate_limited_stopped", "request_budget_exhausted_stopped",
// "failed_key_expired", or "failed_error" — the first three are expected,
// non-error outcomes under personal-key constraints.
func (s *Store) FinishIngestRun(ctx context.Context, id int64, status string, requestsMade, matchesIngested int, errorDetail string) error {
	var errDetail *string
	if errorDetail != "" {
		errDetail = &errorDetail
	}
	_, err := s.Pool.Exec(ctx, `
		UPDATE ingest_runs
		SET finished_at = now(), status = $2, requests_made = $3, matches_ingested = $4, error_detail = $5
		WHERE id = $1
	`, id, status, requestsMade, matchesIngested, errDetail)
	return err
}
