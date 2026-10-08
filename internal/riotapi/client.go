package riotapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"
)

// Client is the shared Riot API client. Exactly one instance (with one
// RateLimiter) should exist per process, injected into every caller —
// ingestion code and API handlers alike — rather than constructed ad hoc,
// so rate-limit accounting stays accurate.
type Client struct {
	httpClient *http.Client
	keySource  KeySource
	limiter    *RateLimiter
	// budget, when set (WithBudget), is this view's share of the key; every
	// attempt draws from it before the shared limiter.
	budget        *Budget
	budgetMaxWait time.Duration
}

// WithBudget returns a view of c whose requests also draw from budget, a
// fixed share of the key, before the shared RateLimiter (which still
// guards the key as a whole). maxWait > 0 makes a request fail with
// *ErrBudgetExhausted instead of waiting longer than that for the share
// (for interactive lookups); 0 waits as long as the context allows.
func (c *Client) WithBudget(budget *Budget, maxWait time.Duration) *Client {
	v := *c
	v.budget, v.budgetMaxWait = budget, maxWait
	return &v
}

type Option func(*Client)

func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

func NewClient(keySource KeySource, limiter *RateLimiter, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		keySource:  keySource,
		limiter:    limiter,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

const maxAttempts = 4

// maxResponseBytes caps how much of a Riot response is read. The largest
// legitimate one, a full match, is ~100 KB.
const maxResponseBytes = 4 << 20

// maxErrorBodyBytes caps how much of an error response is kept in the
// error message (which reaches logs, never clients).
const maxErrorBodyBytes = 512

// do executes a single Riot API GET request against url, identified by
// methodKey for rate-limit accounting (e.g. "tft-match-v1.get-match"),
// decoding the JSON response body into out (skipped if out is nil).
// Retry/backoff is centralized here so every endpoint method gets identical
// behavior: 429s and 5xx/network errors are retried (429 exactly per
// Retry-After, 5xx/network with jittered exponential backoff up to
// maxAttempts); 400/403/404 are never retried and map to typed errors.
func (c *Client) do(ctx context.Context, methodKey, url string, out interface{}) error {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if c.budget != nil {
			if err := c.budget.Acquire(ctx, c.budgetMaxWait); err != nil {
				return err
			}
		}
		if err := c.limiter.Acquire(ctx, methodKey); err != nil {
			return err
		}

		key, err := c.keySource.CurrentKey()
		if err != nil {
			return fmt.Errorf("riot api key: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Riot-Token", key)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = &ErrUnavailable{Detail: "riot api request failed", Err: err}
			if attempt < maxAttempts {
				time.Sleep(backoff(attempt))
				continue
			}
			return lastErr
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		if readErr == nil && len(body) > maxResponseBytes {
			readErr = fmt.Errorf("response larger than %d bytes", maxResponseBytes)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			c.limiter.ReportResponse(methodKey, resp.Header)
			if readErr != nil {
				return fmt.Errorf("riot api read response: %w", readErr)
			}
			if out != nil {
				if err := json.Unmarshal(body, out); err != nil {
					return fmt.Errorf("riot api decode response (%s): %w", methodKey, err)
				}
			}
			return nil

		case http.StatusTooManyRequests:
			retryAfter := c.limiter.ReportRateLimited(methodKey, resp.Header)
			return &ErrRateLimited{RetryAfter: retryAfter, LimitType: resp.Header.Get("X-Rate-Limit-Type")}

		case http.StatusUnauthorized, http.StatusForbidden:
			return &ErrKeyExpired{}

		case http.StatusNotFound:
			return &ErrNotFound{Resource: methodKey}

		case http.StatusBadRequest:
			return fmt.Errorf("riot api bad request (%s): %s", methodKey, errorBody(body))

		default:
			if resp.StatusCode >= 500 && attempt < maxAttempts {
				lastErr = &ErrUnavailable{Detail: fmt.Sprintf("riot api server error %d (%s): %s", resp.StatusCode, methodKey, errorBody(body))}
				time.Sleep(backoff(attempt))
				continue
			}
			if resp.StatusCode >= 500 {
				return &ErrUnavailable{Detail: fmt.Sprintf("riot api server error %d (%s): %s", resp.StatusCode, methodKey, errorBody(body))}
			}
			return fmt.Errorf("riot api unexpected status %d (%s): %s", resp.StatusCode, methodKey, errorBody(body))
		}
	}
	return lastErr
}

// pathSegment escapes a caller-supplied id (PUUID, match id, Riot ID part)
// as one URL path segment. "." and ".." are refused: escaping leaves them
// as-is and they'd move the request to another endpoint.
func pathSegment(s string) (string, error) {
	if s == "" || s == "." || s == ".." {
		return "", fmt.Errorf("riot api: invalid path parameter %q", s)
	}
	return url.PathEscape(s), nil
}

func errorBody(body []byte) string {
	if len(body) > maxErrorBodyBytes {
		return string(body[:maxErrorBodyBytes]) + "..."
	}
	return string(body)
}

func backoff(attempt int) time.Duration {
	base := 500 * time.Millisecond * time.Duration(int64(1)<<uint(attempt-1))
	jitter := time.Duration(rand.Int63n(int64(base)/2 + 1))
	return base + jitter
}
