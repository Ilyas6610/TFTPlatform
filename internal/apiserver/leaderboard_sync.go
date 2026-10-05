package apiserver

import (
	"context"
	"log"
	"sync"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
)

const (
	// leaderboardStaleAfter is how old a platform's snapshot may get before
	// the next leaderboard request refreshes it from Riot (3 requests).
	leaderboardStaleAfter = 2 * time.Minute
	// leaderboardRefreshTimeout bounds how long a request waits on a refresh
	// (e.g. behind the shared rate limiter) before serving the cached
	// snapshot instead.
	leaderboardRefreshTimeout = 10 * time.Second
	// nameResolveTimeout bounds one background name-resolution run. At
	// personal-key limits (100 req / 2 min) a full page of names can take
	// most of a 2-minute window.
	nameResolveTimeout = 3 * time.Minute
	// nameResolveCooldown spaces out retries after a run that couldn't
	// resolve everything (rate limit, dead key, PUUIDs Riot doesn't know),
	// so polling clients can't turn it into a retry loop.
	nameResolveCooldown = time.Minute
)

// leaderboardSync serializes per-platform leaderboard refreshes across
// concurrent requests. The zero value is ready to use.
type leaderboardSync struct {
	mu        sync.Mutex
	refreshMu map[string]*sync.Mutex
}

// refreshLock returns the mutex serializing refreshes for platform, so
// concurrent requests trigger one Riot fetch rather than one each.
func (ls *leaderboardSync) refreshLock(platform string) *sync.Mutex {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.refreshMu == nil {
		ls.refreshMu = make(map[string]*sync.Mutex)
	}
	m, ok := ls.refreshMu[platform]
	if !ok {
		m = &sync.Mutex{}
		ls.refreshMu[platform] = m
	}
	return m
}

// refreshIfStale re-seeds platform's leaderboard from Riot if the stored
// snapshot is missing or older than leaderboardStaleAfter.
func (s *Server) refreshIfStale(ctx context.Context, platform riotapi.PlatformRegion) error {
	lock := s.leaderboard.refreshLock(string(platform))
	lock.Lock()
	defer lock.Unlock()

	// Re-check under the lock: a concurrent request may have just refreshed.
	fetchedAt, err := s.Store.LeaderboardFetchedAt(ctx, string(platform))
	if err != nil {
		return err
	}
	if fetchedAt != nil && time.Since(*fetchedAt) < leaderboardStaleAfter {
		return nil
	}

	// Detached from the request so a client navigating away mid-fetch
	// doesn't waste the requests already spent.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), leaderboardRefreshTimeout)
	defer cancel()
	result, err := ingest.SeedLeaderboard(ctx, s.Riot, s.Store, platform)
	if err != nil {
		return err
	}
	if result.Stopped != "" {
		log.Printf("leaderboard %s: refresh stopped early: %s", platform, result.Stopped)
	}
	return nil
}

func nameResolutionKey(platform riotapi.PlatformRegion) string {
	return "names:" + string(platform)
}

// startNameResolution resolves Riot IDs for puuids in the background (see
// backgroundJobs). It reports whether a run is in flight after the call.
func (s *Server) startNameResolution(platform riotapi.PlatformRegion, puuids []string) bool {
	key := nameResolutionKey(platform)
	if len(puuids) == 0 {
		return s.jobs.isRunning(key)
	}
	return s.jobs.start(key, nameResolveTimeout, nameResolveCooldown, func(ctx context.Context) (bool, error) {
		result, err := ingest.ResolveNames(ctx, s.Riot, s.Store, platform, puuids)
		if err != nil {
			return false, err
		}
		if result.Stopped != "" {
			log.Printf("leaderboard %s: resolve names stopped early: %s", platform, result.Stopped)
		}
		return result.Resolved == len(puuids), nil
	})
}
