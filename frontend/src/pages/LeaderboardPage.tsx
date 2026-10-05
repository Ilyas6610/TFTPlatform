import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ApiError, LeaderboardEntry, getLeaderboard } from "../api/client";

const PLATFORMS = ["na1", "euw1", "eun1", "kr", "jp1", "br1", "la1", "la2", "oc1", "tr1", "ru"];

export default function LeaderboardPage() {
  const { platform = "na1" } = useParams();
  const navigate = useNavigate();
  const [entries, setEntries] = useState<LeaderboardEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    getLeaderboard(platform, 100)
      .then(setEntries)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "failed to load leaderboard"))
      .finally(() => setLoading(false));
  }, [platform]);

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

      {!loading && !error && (
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
                  <td>{e.gameName ? `${e.gameName}#${e.tagLine}` : <span className="muted">unresolved</span>}</td>
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
