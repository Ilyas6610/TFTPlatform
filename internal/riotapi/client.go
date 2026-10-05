package riotapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
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
			lastErr = fmt.Errorf("riot api request failed: %w", err)
			if attempt < maxAttempts {
				time.Sleep(backoff(attempt))
				continue
			}
			return lastErr
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

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
			return fmt.Errorf("riot api bad request (%s): %s", methodKey, string(body))

		default:
			if resp.StatusCode >= 500 && attempt < maxAttempts {
				lastErr = fmt.Errorf("riot api server error %d (%s): %s", resp.StatusCode, methodKey, string(body))
				time.Sleep(backoff(attempt))
				continue
			}
			return fmt.Errorf("riot api unexpected status %d (%s): %s", resp.StatusCode, methodKey, string(body))
		}
	}
	return lastErr
}

func backoff(attempt int) time.Duration {
	base := 500 * time.Millisecond * time.Duration(int64(1)<<uint(attempt-1))
	jitter := time.Duration(rand.Int63n(int64(base)/2 + 1))
	return base + jitter
}
