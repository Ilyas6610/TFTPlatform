import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError, Leaderboard, LeaderboardEntry, getLeaderboard, staleSuffix } from "../api/client";

const PLATFORMS = ["na1", "euw1", "eun1", "kr", "jp1", "br1", "la1", "la2", "oc1", "tr1", "ru"];

// While the server is resolving Riot IDs in the background, re-fetch at this
// interval so names fill in as they're found.
const RESOLVE_POLL_MS = 3000;

// Below this many stored games a player's 1st-place rate and average say
// little (one bad game reads as an 8.00 average), so they're dimmed.
const MIN_STORED_GAMES = 10;

// Apex tiers have no divisions, so Riot's "I" says nothing there.
const APEX = new Set(["MASTER", "GRANDMASTER", "CHALLENGER"]);
const tierLabel = (tier: string, rank: string | null) =>
  tier.charAt(0) + tier.slice(1).toLowerCase() + (rank && !APEX.has(tier) ? ` ${rank}` : "");

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
            Games and top 4 rate cover the whole ranked season (Riot). 1st place rate and average placement come from the ranked
            games stored here (the number after the dot is how many; dimmed under {MIN_STORED_GAMES} games), so they firm up as more games are
            collected.
          </p>
          <table className="leaderboard-table">
            <thead>
              <tr>
                <th>#</th>
                <th>Player</th>
                <th className="col-tier">Tier</th>
                <th>LP</th>
                <th className="col-games" title="Ranked games this season (Riot)">Games</th>
                <th title="Share of ranked games finished in the top 4 this season (Riot)">Top 4</th>
                <th title="Share of stored ranked games won (1st place); the number after the dot is how many games we have">1st</th>
                <th title="Average placement over the ranked games we have stored; the number after the dot is how many games we have">Avg</th>
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
                    <span className="tier-inline muted">{tierLabel(e.tier, e.rank)}</span>
                  </td>
                  <td className="col-tier">{tierLabel(e.tier, e.rank)}</td>
                  <td>{e.leaguePoints}</td>
                  <td className="col-games">{e.games}</td>
                  <td>{e.top4Rate == null ? "–" : pct(e.top4Rate)}</td>
                  <StoredCells stored={e.stored} />
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

function StoredCells({ stored }: { stored: LeaderboardEntry["stored"] }) {
  if (!stored)
    return (
      <>
        <td className="muted">–</td>
        <td className="muted">–</td>
      </>
    );
  const few = stored.games < MIN_STORED_GAMES;
  const cls = few ? "muted few-games" : undefined;
  const title = few ? `Only ${stored.games} stored ranked games` : `${stored.games} stored ranked games`;
  return (
    <>
      <td className={cls} title={title}>
        {pct(stored.winRate)}
        <span className="muted stored-n"> · {stored.games}</span>
      </td>
      <td className={cls} title={title}>
        {stored.avgPlacement.toFixed(2)}
        <span className="muted stored-n"> · {stored.games}</span>
      </td>
    </>
  );
}
