// Package riotapitest points a real *riotapi.Client at a fake Riot API, so
// tests exercise the actual URL building, decoding and error mapping.
package riotapitest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"tft-platform/internal/riotapi"
)

// OriginalHostHeader carries the Riot host a request was addressed to
// (e.g. "americas.api.riotgames.com"), since every request is redirected to
// the one test server.
const OriginalHostHeader = "X-Original-Host"

// NewClient returns a client whose requests, to any Riot host, are served by
// handler. The rate limiter is generous enough never to block a test.
func NewClient(t *testing.T, handler http.Handler) *riotapi.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set(OriginalHostHeader, r.URL.Host)
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(r)
	})
	return riotapi.NewClient(
		riotapi.EnvKeySource{Key: "test-key"},
		riotapi.NewRateLimiter(1000, 1000),
		riotapi.WithHTTPClient(&http.Client{Transport: transport}),
	)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
