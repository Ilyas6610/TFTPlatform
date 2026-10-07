package riotapi

import (
	"fmt"
	"time"
)

// ErrRateLimited is returned when Riot responds 429. Callers decide how to
// react: the ingestion crawler stops its current batch cleanly and resumes
// next run; the API server's live-fetch path returns a 502 to the frontend
// instead of hanging.
type ErrRateLimited struct {
	RetryAfter time.Duration
	LimitType  string // "application", "method", or "service" (per X-Rate-Limit-Type)
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("riot api rate limited (%s), retry after %s", e.LimitType, e.RetryAfter)
}

// ErrKeyExpired is returned on 403/401, which for a personal key almost
// always means it has expired (personal keys expire every 24h) or was
// revoked. Distinct from ErrRateLimited so callers can surface a clear
// "needs manual key rotation" signal instead of silently retrying.
type ErrKeyExpired struct{}

func (e *ErrKeyExpired) Error() string {
	return "riot api key invalid or expired"
}

// ErrNotFound is returned on 404 (e.g. unknown summoner or match), so
// callers can return their own 404 rather than retrying a request that will
// never succeed.
type ErrNotFound struct {
	Resource string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("riot api: %s not found", e.Resource)
}

// ErrUnavailable is returned when Riot itself can't answer after the client's
// retries: a 5xx response or a network failure (timeout, DNS, connection
// reset). It says nothing about the request, so callers working through many
// players treat it as "stop for now" rather than as one player's fault. The
// underlying error (a net.Error, say) is available through errors.Unwrap.
type ErrUnavailable struct {
	Detail string
	Err    error
}

func (e *ErrUnavailable) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Detail, e.Err)
	}
	return e.Detail
}

func (e *ErrUnavailable) Unwrap() error { return e.Err }
