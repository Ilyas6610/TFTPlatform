import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { PlayerMatchSummary, PlayerMatches, getPlayerMatches, staleSuffix } from "../api/client";
import { GameIcon } from "../assets/tft";
import { CURRENT_TFT_SET } from "../config";
import { Names, queueName } from "./stats";

const PAGE = 20;
// The server pages back at most this far (it mirrors matchHistoryMaxOffset).
const MAX_OFFSET = 500;
// While the server fetches games from Riot in the background, re-fetch the
// page at this interval so they appear as they're stored.
const POLL_MS = 3000;

function timeAgo(iso: string): string {
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 60) return `${Math.max(1, mins)}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

/**
 * A player's games, newest first, a page at a time. The first page also
 * makes the server sync the newest games; "Load more" asks for the next
 * page, which the server fetches from Riot when it isn't stored yet.
 */
export function MatchHistory({
  puuid,
  region,
  names,
  costs,
  onFirstPage,
  onGamesAdded,
}: {
  puuid: string;
  region: string;
  names: Names;
  costs: Map<string, number>;
  /** Called with each version of the first page (to show recent form). */
  onFirstPage?: (matches: PlayerMatchSummary[]) => void;
  /** Called when a page gained games (synced or fetched from Riot). */
  onGamesAdded?: () => void;
}) {
  const navigate = useNavigate();
  // pages[i] is the response for offset i*PAGE.
  const [pages, setPages] = useState<PlayerMatches[]>([]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const alive = useRef(true);
  // Games per loaded page, to notice pages that grew.
  const counts = useRef<number[]>([]);

  useEffect(() => {
    alive.current = true;
    setPages([]);
    counts.current = [];
    setError(null);
    loadPage(0);
    return () => {
      alive.current = false;
      timers.current.forEach(clearTimeout);
      timers.current = [];
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [puuid, region]);

  function loadPage(index: number): Promise<void> {
    return getPlayerMatches(puuid, region, PAGE, index * PAGE)
      .then((p) => {
        if (!alive.current) return;
        const before = counts.current[index];
        counts.current[index] = p.matches.length;
        // A re-fetched page that grew (games stored meanwhile), or an older
        // page the stats haven't seen yet.
        if (
          (before !== undefined && p.matches.length > before) ||
          (before === undefined && index > 0 && p.matches.length > 0)
        )
          onGamesAdded?.();
        setPages((prev) => {
          const next = [...prev];
          next[index] = p;
          return next;
        });
        if (index === 0) onFirstPage?.(p.matches);
        if (p.refreshing) timers.current.push(setTimeout(() => loadPage(index).catch(() => {}), POLL_MS));
      })
      .catch(() => {
        if (alive.current) setError("couldn't load match history");
      });
  }

  const first = pages[0];
  const last = pages[pages.length - 1];
  const matches = pages.flatMap((p) => p?.matches ?? []);
  const canLoadMore = !!last && last.hasMore && !last.refreshing && !loadingMore && pages.length * PAGE <= MAX_OFFSET;

  return (
    <div className="panel">
      <div className="history-head">
        <h3>Match history</h3>
        {first && (
          <span className="muted">
            {first.syncedAt ? `Updated ${new Date(first.syncedAt).toLocaleTimeString()}` : "Not yet updated"}
            {pages.some((p) => p?.refreshing) && " · fetching games from Riot…"}
          </span>
        )}
      </div>
      {first?.stale && (
        <div className="warning-box">
          Showing saved matches — couldn't update from Riot{staleSuffix(first.staleReason)}.
        </div>
      )}
      {error && <div className="error-box">{error}</div>}
      {first && matches.length === 0 && (
        <p className="muted">{first.refreshing ? "Loading matches from Riot…" : "No matches found for this player."}</p>
      )}

      <div className="history-list">
        {matches.map((m) => (
          <div
            key={m.matchId}
            className={`history-row placement-row-${m.placement <= 4 ? "top" : "bottom"}`}
            role="link"
            tabIndex={0}
            onClick={() => navigate(`/matches/${m.matchId}`)}
            onKeyDown={(e) => e.key === "Enter" && navigate(`/matches/${m.matchId}`)}
          >
            <div className="history-meta">
              <span className={`placement placement-${m.placement}`}>#{m.placement}</span>
              <span>{queueName(m.queueId)}</span>
              <span className="muted" title={new Date(m.gameDatetime).toLocaleString()}>
                {timeAgo(m.gameDatetime)} · lvl {m.level}
                {m.tftSetNumber && m.tftSetNumber !== CURRENT_TFT_SET ? ` · set ${m.tftSetNumber}` : ""}
              </span>
            </div>
            <div className="history-traits">
              {m.traits.slice(0, 6).map((t) => (
                <span
                  className={`trait-badge trait-style-${Math.min(t.style, 4)}`}
                  key={t.id}
                  title={`${names.name(t.id)} ${t.units}`}
                >
                  <GameIcon
                    kind="traits"
                    id={t.id}
                    size={18}
                    fallbackSrc={names.icon(t.id)}
                    fallbackName={names.name(t.id)}
                  />
                  {t.units > 0 && t.units}
                </span>
              ))}
            </div>
            <div className="history-units">
              {m.units.map((u, i) => (
                <span key={i} className="history-unit" title={`${names.name(u.id)} ${"★".repeat(u.star || 1)}`}>
                  <span className={`unit-stars star-${u.star}`}>{"★".repeat(u.star || 1)}</span>
                  <GameIcon
                    kind="champions"
                    id={u.id}
                    size={34}
                    fallbackSrc={names.icon(u.id)}
                    fallbackName={names.name(u.id)}
                    className={costs.get(u.id) ? `cost-${costs.get(u.id)}` : ""}
                  />
                  <span className="unit-items">
                    {u.items.map((it, j) => (
                      <GameIcon
                        key={j}
                        kind="items"
                        id={it}
                        size={12}
                        fallbackSrc={names.icon(it)}
                        fallbackName={names.name(it)}
                      />
                    ))}
                  </span>
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>

      {last?.refreshing && pages.length > 1 && <p className="muted">Fetching older games from Riot…</p>}
      {canLoadMore && (
        <button
          type="button"
          className="load-more"
          onClick={() => {
            setLoadingMore(true);
            loadPage(pages.length).finally(() => alive.current && setLoadingMore(false));
          }}
        >
          Load {PAGE} more
        </button>
      )}
      {last && !last.hasMore && matches.length > 0 && pages.length > 1 && (
        <p className="muted">That's the whole history Riot keeps for this player.</p>
      )}
    </div>
  );
}
