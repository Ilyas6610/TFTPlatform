import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError, Leaderboard, getLeaderboard } from "../api/client";

const PLATFORMS = ["na1", "euw1", "eun1", "kr", "jp1", "br1", "la1", "la2", "oc1", "tr1", "ru"];

// While the server is resolving Riot IDs in the background, re-fetch at this
// interval so names fill in as they're found.
const RESOLVE_POLL_MS = 3000;

const STALE_REASONS: Record<string, string> = {
  riot_api_key_expired: "the Riot API key needs rotation",
  riot_api_rate_limited: "Riot API rate limit reached",
  riot_api_timeout: "Riot API is busy",
};

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

  const entries = board?.entries ?? [];

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
          {board.staleReason && STALE_REASONS[board.staleReason] ? ` (${STALE_REASONS[board.staleReason]})` : ""}.
        </div>
      )}

      {!loading && !error && board && (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>#</th>
                <th>Player</th>
                <th>Tier</th>
                <th>LP</th>
                <th>W / L</th>
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
                  <td>
                    {e.wins} / {e.losses}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
