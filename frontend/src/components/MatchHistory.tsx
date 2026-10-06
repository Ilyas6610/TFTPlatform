import { Fragment, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { PlayerMatchSummary, PlayerMatches, getPlayerMatches, staleSuffix } from "../api/client";
import { GameIcon } from "../assets/tft";
import { CURRENT_TFT_SET } from "../config";
import { Names, avg, pct, queueName } from "./stats";

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
  /** Called with each version of the first page (recent form, current rank). */
  onFirstPage?: (page: PlayerMatches) => void;
  /** Called when a page gained games (synced or fetched from Riot). */
  onGamesAdded?: () => void;
}) {
  const navigate = useNavigate();
  // pages[i] is the response for offset i*PAGE.
  const [pages, setPages] = useState<PlayerMatches[]>([]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [mode, setMode] = useState<number | "all">("all");
  const [error, setError] = useState<string | null>(null);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const alive = useRef(true);
  // Games per loaded page, to notice pages that grew.
  const counts = useRef<number[]>([]);

  useEffect(() => {
    alive.current = true;
    setPages([]);
    setMode("all");
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
        if (index === 0) onFirstPage?.(p);
        if (p.refreshing) timers.current.push(setTimeout(() => loadPage(index).catch(() => {}), POLL_MS));
      })
      .catch(() => {
        if (alive.current) setError("couldn't load match history");
      });
  }

  const first = pages[0];
  const last = pages[pages.length - 1];
  const loaded = pages.flatMap((p) => p?.matches ?? []);
  // Modes among the loaded games, most played first. Riot's match list can't
  // be asked for one mode, so filtering happens here and "Load more" keeps
  // paging through every mode.
  const modes = [
    ...loaded.reduce((m, g) => m.set(g.queueId, (m.get(g.queueId) ?? 0) + 1), new Map<number, number>()),
  ].sort((a, b) => b[1] - a[1]);
  const matches = mode === "all" ? loaded : loaded.filter((m) => m.queueId === mode);
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
      {modes.length > 1 && (
        <div className="mode-tabs" role="tablist" aria-label="Game mode">
          <button type="button" role="tab" aria-selected={mode === "all"} onClick={() => setMode("all")}>
            All <span className="muted">{loaded.length}</span>
          </button>
          {modes.map(([q, n]) => (
            <button type="button" role="tab" key={q} aria-selected={mode === q} onClick={() => setMode(q)}>
              {queueName(q)} <span className="muted">{n}</span>
            </button>
          ))}
        </div>
      )}
      {first?.stale && (
        <div className="warning-box">
          Showing saved matches — couldn't update from Riot{staleSuffix(first.staleReason)}.
        </div>
      )}
      {error && <div className="error-box">{error}</div>}
      {mode !== "all" && (
        <p className="muted">
          {matches.length} {queueName(mode)} {matches.length === 1 ? "game" : "games"} among the {loaded.length} loaded
          {canLoadMore ? " — load more to look further back." : "."}
        </p>
      )}
      {first && loaded.length === 0 && (
        <p className="muted">{first.refreshing ? "Loading matches from Riot…" : "No matches found for this player."}</p>
      )}

      <div className="history-list">
        {patchGroups(matches).map((g) => (
          <Fragment key={g.patch + g.matches[0].matchId}>
            <div className="patch-head">
              <strong>{g.patch ? `Patch ${g.patch}` : "Patch unknown"}</strong>
              <span className="muted">
                {g.matches.length} {g.matches.length === 1 ? "game" : "games"} · {avg(g.avg)} avg · top 4 {pct(g.top4)}
              </span>
              {g.lpGames > 0 && (
                <span className={g.lp >= 0 ? "good" : "bad"} title={`Known LP change over ${g.lpGames} ranked games`}>
                  {g.lp > 0 ? "+" : ""}
                  {g.lp} LP
                </span>
              )}
            </div>
            {g.matches.map((m) => (
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
                  <span>
                    {queueName(m.queueId)}
                    {m.lp && <LPBadge change={m.lp} />}
                  </span>
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
          </Fragment>
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

/** Consecutive games grouped by patch (the list is newest first). */
function patchGroups(matches: PlayerMatchSummary[]) {
  const groups: {
    patch: string;
    matches: PlayerMatchSummary[];
    avg: number;
    top4: number;
    lp: number;
    lpGames: number;
  }[] = [];
  for (const m of matches) {
    const patch = m.patch ?? "";
    let g = groups[groups.length - 1];
    if (!g || g.patch !== patch) {
      g = { patch, matches: [], avg: 0, top4: 0, lp: 0, lpGames: 0 };
      groups.push(g);
    }
    g.matches.push(m);
    if (m.lp) {
      g.lp += m.lp.delta;
      g.lpGames += m.lp.games;
    }
  }
  for (const g of groups) {
    g.avg = g.matches.reduce((n, m) => n + m.placement, 0) / g.matches.length;
    g.top4 = g.matches.filter((m) => m.placement <= 4).length / g.matches.length;
  }
  return groups;
}

/** A ranked game's LP change; over several games when snapshots were further apart. */
function LPBadge({ change }: { change: { delta: number; games: number } }) {
  const sign = change.delta > 0 ? "+" : "";
  return (
    <span
      className={`lp-badge ${change.delta >= 0 ? "good" : "bad"}`}
      title={
        change.games > 1
          ? `${sign}${change.delta} LP over the last ${change.games} ranked games (rank checked before and after them)`
          : `${sign}${change.delta} LP this game`
      }
    >
      {sign}
      {change.delta} LP{change.games > 1 ? ` / ${change.games}` : ""}
    </span>
  );
}
