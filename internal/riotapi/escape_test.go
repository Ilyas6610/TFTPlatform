package riotapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
)

// Caller-supplied ids must stay one path segment: no "/", "?" or ".."
// may steer the keyed request to another Riot endpoint.
func TestPathParamsAreEscaped(t *testing.T) {
	var paths []string
	riot := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusNotFound)
	}))
	ctx := context.Background()
	for _, bad := range []string{"x/../../lol/status/v4/platform-data", "a?count=100&b=c", "%2F", "..", "a#b"} {
		paths = nil
		riot.GetTFTMatchIDsByPUUID(ctx, riotapi.RoutingAmericas, bad, 20)
		riot.GetTFTMatch(ctx, riotapi.RoutingAmericas, bad)
		riot.GetTFTSummonerByPUUID(ctx, "na1", bad)
		riot.GetAccountByPUUID(ctx, riotapi.RoutingAmericas, bad)
		if bad != ".." && len(paths) != 4 {
			t.Fatalf("%q: got %d requests, want 4", bad, len(paths))
		}
		for _, p := range paths {
			path, query, _ := strings.Cut(p, "?")
			for _, seg := range strings.Split(path, "/") {
				if seg == ".." {
					t.Errorf("%q: path %q has a .. segment", bad, path)
				}
			}
			if query != "" && query != "count=20" {
				t.Errorf("%q: query %q was injected", bad, query)
			}
			if !strings.HasPrefix(path, "/tft/") && !strings.HasPrefix(path, "/riot/") {
				t.Errorf("%q: unexpected path %q", bad, path)
			}
		}
	}
}

func TestDotSegmentsAreRefused(t *testing.T) {
	calls := 0
	riot := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	for _, bad := range []string{"..", ".", ""} {
		if _, err := riot.GetAccountByPUUID(context.Background(), riotapi.RoutingAmericas, bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
		if _, err := riot.GetAccountByRiotID(context.Background(), riotapi.RoutingAmericas, "name", bad); err == nil {
			t.Errorf("tag %q: expected an error", bad)
		}
	}
	if calls != 0 {
		t.Errorf("%d requests sent, want none", calls)
	}
}

func TestOversizedResponseIsRejected(t *testing.T) {
	riot := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"metadata":{},"info":{},"pad":"`))
		w.Write([]byte(strings.Repeat("x", 5<<20)))
		w.Write([]byte(`"}`))
	}))
	if _, _, err := riot.GetTFTMatch(context.Background(), riotapi.RoutingAmericas, "NA1_1"); err == nil {
		t.Fatal("expected an error for a response over the size cap")
	}
}
