package apiserver

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/lp"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
)

const (
	// matchHistoryStaleAfter is how long after a sync a profile view
	// triggers another one (1 request, plus 1 per new match).
	matchHistoryStaleAfter = 2 * time.Minute
	// matchHistorySyncCount is how many recent matches a sync covers — one
	// profile page's worth.
	matchHistorySyncCount = 20
	// matchHistoryRefreshMin is how soon after a sync the profile's Update
	// button can start another (?refresh=1). Much shorter than
	// matchHistoryStaleAfter, but it still bounds what one viewer can spend
	// of the shared Riot key.
	matchHistoryRefreshMin = 20 * time.Second
	matchSyncTimeout       = 2 * time.Minute
	matchSyncCooldown      = time.Minute
	// matchHistoryMaxOffset bounds paging back through a history (Riot keeps
	// about a thousand recent match ids; a profile rarely needs more).
	matchHistoryMaxOffset = 500
)

type PlayerMatchesResponse struct {
	// Matches is the requested page, newest first, with each final board,
	// the patch it was played on and, for ranked games, the LP it gained or
	// lost when rank snapshots bracket it.
	Matches []PlayerMatchView `json:"matches"`
	// Ranks is the player's latest known rank per ranked queue (first page
	// only), refreshed by the newest-games sync.
	Ranks []store.RankSnapshot `json:"ranks,omitempty"`
	// HasMore is set when an older page may exist: this page is full, or
	// its missing games are still being fetched from Riot.
	HasMore bool `json:"hasMore"`
	// SyncedAt is when this player's history was last synced from Riot, or
	// null if never.
	SyncedAt *string `json:"syncedAt"`
	// Refreshing is set while a sync is running in the background; clients
	// should re-fetch to pick up new matches.
	Refreshing bool `json:"refreshing"`
	// Stale is set when the last sync attempt failed; StaleReason says why.
	Stale       bool   `json:"stale"`
	StaleReason string `json:"staleReason,omitempty"`
	// RefreshTooSoon answers ?refresh=1 when the history was synced under
	// matchHistoryRefreshMin ago: nothing was started.
	RefreshTooSoon bool `json:"refreshTooSoon,omitempty"`
}

// PlayerMatchView is a history game with what's derived for display.
type PlayerMatchView struct {
	store.PlayerMatch
	Patch string     `json:"patch,omitempty"` // "18.3"
	LP    *lp.Change `json:"lp,omitempty"`
}

func matchSyncKey(puuid string) string {
	return "matches:" + puuid
}

func olderMatchesKey(puuid string, offset int) string {
	return "matches-older:" + puuid + ":" + strconv.Itoa(offset)
}

// handlePlayerMatches serves GET /api/v1/players/{puuid}/matches
// [?offset=0&limit=20&region=<platform>]. Ingested matches are always served
// from Postgres, newest first, offset games skipped. With region, the first
// page also kicks off a background sync of the newest games when the
// history wasn't synced within matchHistoryStaleAfter, and an older page
// that isn't fully stored kicks off a background fetch of that page from
// Riot (see backgroundJobs); clients re-fetch while refreshing. Without
// region the response is cache-only.
func (s *Server) handlePlayerMatches(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")
	ctx := r.Context()
	// Checked before the store or the background job table see it, so junk
	// is never queued for crawling or kept in memory.
	if !validPUUID(puuid) {
		writeError(w, http.StatusBadRequest, "invalid_puuid", "puuid must be 1-100 letters, digits, '-' or '_'")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > matchHistoryMaxOffset {
			writeError(w, http.StatusBadRequest, "invalid_offset", "offset must be 0 to "+strconv.Itoa(matchHistoryMaxOffset))
			return
		}
		offset = n
	}

	matches, err := s.Store.PlayerMatches(ctx, puuid, offset, limit)
	if err != nil {
		writeDBError(w, r, err)
		return
	}

	views, ranks, err := s.decorateMatches(ctx, puuid, matches)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	resp := PlayerMatchesResponse{Matches: views}
	if offset == 0 {
		resp.Ranks = ranks
	}
	key := matchSyncKey(puuid)
	if offset > 0 {
		key = olderMatchesKey(puuid, offset)
	}

	syncedAt, err := s.Store.MatchHistorySyncedAt(ctx, puuid)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if region := r.URL.Query().Get("region"); region != "" {
		platform := riotapi.PlatformRegion(region)
		if _, err := riotapi.RoutingForPlatform(platform); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_region", err.Error())
			return
		}
		switch {
		case offset > 0 && len(matches) < limit:
			// An older page not fully stored: fetch that page of Riot's ids.
			// A finished run reports incomplete, which holds off a rerun for
			// matchSyncCooldown, so a page Riot has nothing more for isn't
			// re-requested by every poll.
			resp.Refreshing = s.jobs.start(key, matchSyncTimeout, matchSyncCooldown, func(ctx context.Context) (bool, error) {
				_, err := ingest.SyncOlderMatches(ctx, s.Riot, s.Store, platform, puuid, offset, limit)
				return false, err
			})
		case offset == 0 && syncDue(r, syncedAt, &resp):
			resp.Refreshing = s.jobs.start(key, matchSyncTimeout, matchSyncCooldown, func(ctx context.Context) (bool, error) {
				result, err := ingest.SyncPlayerMatches(ctx, s.Riot, s.Store, platform, puuid, matchHistorySyncCount)
				if err == nil && result.Stopped != "" {
					log.Printf("match sync %s: stopped early: %s", puuid, result.Stopped)
				}
				// Rank right after the games: the snapshot brackets them for
				// per-game LP. Best effort; a failure doesn't fail the sync.
				if err == nil && result.IDsFetched {
					if _, rerr := ingest.SyncPlayerRank(ctx, s.Riot, s.Store, platform, puuid); rerr != nil {
						log.Printf("rank sync %s: %v", puuid, rerr)
					}
				}
				return result.Stopped == "", err
			})
		}
	}
	if !resp.Refreshing {
		resp.Refreshing = s.jobs.isRunning(key)
	}
	if err := s.jobs.lastError(key); err != nil && !resp.Refreshing {
		resp.Stale = true
		resp.StaleReason = riotErrorCode(err)
	}
	if syncedAt != nil {
		t := syncedAt.Format(time.RFC3339)
		resp.SyncedAt = &t
	}

	resp.HasMore = len(matches) == limit || resp.Refreshing
	writeJSON(w, http.StatusOK, resp)
}

// syncDue reports whether the newest games should be synced now: the history
// was never synced or has aged past matchHistoryStaleAfter. ?refresh=1 (the
// profile's Update button) waits only matchHistoryRefreshMin, and a refresh
// that comes sooner is flagged on resp rather than run.
func syncDue(r *http.Request, syncedAt *time.Time, resp *PlayerMatchesResponse) bool {
	after := matchHistoryStaleAfter
	if r.URL.Query().Get("refresh") == "1" {
		after = matchHistoryRefreshMin
		resp.RefreshTooSoon = syncedAt != nil && time.Since(*syncedAt) < after
	}
	return syncedAt == nil || time.Since(*syncedAt) >= after
}

// decorateMatches adds each game's patch and LP change, and returns the
// player's latest rank per queue.
func (s *Server) decorateMatches(ctx context.Context, puuid string, matches []store.PlayerMatch) ([]PlayerMatchView, []store.RankSnapshot, error) {
	cal, err := s.Store.PatchCalendar(ctx)
	if err != nil {
		return nil, nil, err
	}
	history, err := s.Store.RankHistory(ctx, puuid)
	if err != nil {
		return nil, nil, err
	}
	var changes map[string]lp.Change
	if len(history) > 1 {
		games, err := s.Store.PlayerGamesSince(ctx, puuid, history[0].FetchedAt)
		if err != nil {
			return nil, nil, err
		}
		changes = lp.Attribute(history, games)
	}

	views := make([]PlayerMatchView, len(matches))
	for i, m := range matches {
		views[i] = PlayerMatchView{PlayerMatch: m, Patch: setdata.PatchOfGame(cal, m.TFTSetNumber, m.GameDatetime, m.GameVersion)}
		if c, ok := changes[m.MatchID]; ok {
			views[i].LP = &c
		}
	}
	return views, latestRanks(history), nil
}

// latestRanks keeps the newest snapshot per queue, Ranked first.
func latestRanks(history []store.RankSnapshot) []store.RankSnapshot {
	last := map[string]store.RankSnapshot{}
	for _, r := range history { // oldest first
		last[r.QueueType] = r
	}
	out := []store.RankSnapshot{}
	for _, q := range []string{"RANKED_TFT", "RANKED_TFT_DOUBLE_UP"} {
		if r, ok := last[q]; ok {
			out = append(out, r)
			delete(last, q)
		}
	}
	for _, r := range last {
		out = append(out, r)
	}
	return out
}
