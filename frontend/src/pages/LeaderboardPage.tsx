import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError, Leaderboard, getLeaderboard, staleSuffix } from "../api/client";

const PLATFORMS = ["na1", "euw1", "eun1", "kr", "jp1", "br1", "la1", "la2", "oc1", "tr1", "ru"];

// While the server is resolving Riot IDs in the background, re-fetch at this
// interval so names fill in as they're found.
const RESOLVE_POLL_MS = 3000;

const pct = (x: number) => `${(x * 100).toFixed(1)}%`;

export default function LeaderboardPage() {
  const { platform = "na1" } = useParams();
  const navigate = useNavigate();
  const [board, setBoard] = useState<Leaderboard | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Opening the page or switching region fetches the board, which makes the
  // server refresh rankings from Riot if its snapshot is stale.
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function load(initial: boolean) {
      if (initial) {
        setLoading(true);
        setError(null);
      }
      getLeaderboard(platform, 100)
        .then((b) => {
          if (cancelled) return;
          setBoard(b);
          if (b.resolving) timer = setTimeout(() => load(false), RESOLVE_POLL_MS);
        })
        .catch((e: unknown) => {
          if (!cancelled && initial) setError(e instanceof ApiError ? e.message : "failed to load leaderboard");
        })
        .finally(() => {
          if (!cancelled && initial) setLoading(false);
        });
    }

    setBoard(null);
    load(true);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [platform]);

  // Show the top 50 first; the rest of the fetched 100 on request.
  const [showAll, setShowAll] = useState(false);
  useEffect(() => setShowAll(false), [platform]);
  const allEntries = board?.entries ?? [];
  const entries = showAll ? allEntries : allEntries.slice(0, 50);

  return (
    <div>
      <div className="set-picker">
        <label htmlFor="platform-select" className="muted">
          Region:
        </label>
        <select id="platform-select" value={platform} onChange={(e) => navigate(`/leaderboard/${e.target.value}`)}>
          {PLATFORMS.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </div>

      {loading && <p className="muted">Loading...</p>}
      {error && <div className="error-box">{error}</div>}

      {board && (
        <p className="muted leaderboard-status">
          {board.fetchedAt ? `Updated ${new Date(board.fetchedAt).toLocaleTimeString()}` : "Not yet updated"}
          {board.resolving && " · resolving player names…"}
        </p>
      )}
      {board?.stale && (
        <div className="warning-box">
          Showing saved rankings — couldn't refresh from Riot
          {staleSuffix(board.staleReason)}.
        </div>
      )}

      {!loading && !error && board && (
        <div className="panel">
          <p className="muted leaderboard-note">
            Games and top 4 rate cover the whole ranked season (Riot). Win rate (1st place) and average placement come from the
            ranked games stored here (the number after the dot is how many), so they firm up as more games are collected.
          </p>
          <table>
            <thead>
              <tr>
                <th>#</th>
                <th>Player</th>
                <th>Tier</th>
                <th>LP</th>
                <th title="Ranked games this season (Riot)">Games</th>
                <th title="Share of ranked games finished in the top 4 this season (Riot)">Top 4</th>
                <th title="Share of stored ranked games won (1st place); the number after the dot is how many games we have">Win rate</th>
                <th title="Average placement over the ranked games we have stored">Avg</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e, i) => (
                <tr key={e.puuid}>
                  <td>{i + 1}</td>
                  <td>
                    {e.gameName ? (
                      <Link to={`/players/${platform}/${encodeURIComponent(e.gameName)}/${encodeURIComponent(e.tagLine ?? "")}`}>
                        {e.gameName}
                        <span className="muted">#{e.tagLine}</span>
                      </Link>
                    ) : (
                      <span className="muted">{board.resolving ? "resolving…" : "unknown"}</span>
                    )}
                  </td>
                  <td>
                    {e.tier}
                    {e.rank ? ` ${e.rank}` : ""}
                  </td>
                  <td>{e.leaguePoints}</td>
                  <td>{e.games}</td>
                  <td>{e.top4Rate == null ? "–" : pct(e.top4Rate)}</td>
                  <td title={e.stored ? `${e.stored.games} stored ranked games` : undefined}>
                    {e.stored ? (
                      <>
                        {pct(e.stored.winRate)}
                        <span className="muted"> · {e.stored.games}</span>
                      </>
                    ) : (
                      <span className="muted">–</span>
                    )}
                  </td>
                  <td>{e.stored ? e.stored.avgPlacement.toFixed(2) : <span className="muted">–</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!showAll && allEntries.length > entries.length && (
            <button type="button" className="show-more" onClick={() => setShowAll(true)}>
              Show top {allEntries.length}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
