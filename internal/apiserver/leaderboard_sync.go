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

// leaderboardSync coordinates per-platform leaderboard refreshes and
// background name resolution across concurrent requests. The zero value is
// ready to use.
type leaderboardSync struct {
	mu        sync.Mutex
	refreshMu map[string]*sync.Mutex
	resolving map[string]bool
	retryAt   map[string]time.Time
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

// startNameResolution resolves Riot IDs for puuids in the background unless
// a run for platform is already in flight or cooling down. It reports
// whether a run is in flight after the call.
func (s *Server) startNameResolution(platform riotapi.PlatformRegion, puuids []string) bool {
	ls := &s.leaderboard
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.resolving == nil {
		ls.resolving = make(map[string]bool)
		ls.retryAt = make(map[string]time.Time)
	}
	key := string(platform)
	if ls.resolving[key] {
		return true
	}
	if len(puuids) == 0 || time.Now().Before(ls.retryAt[key]) {
		return false
	}
	ls.resolving[key] = true

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), nameResolveTimeout)
		defer cancel()
		result, err := ingest.ResolveNames(ctx, s.Riot, s.Store, platform, puuids)
		if err != nil {
			log.Printf("leaderboard %s: resolve names: %v", platform, err)
		} else if result.Stopped != "" {
			log.Printf("leaderboard %s: resolve names stopped early: %s", platform, result.Stopped)
		}

		ls.mu.Lock()
		defer ls.mu.Unlock()
		ls.resolving[key] = false
		if err != nil || result.Resolved < len(puuids) {
			ls.retryAt[key] = time.Now().Add(nameResolveCooldown)
		}
	}()
	return true
}
