import { FormEvent, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ApiError, PlayerMatches, PlayerProfile, getPlayerMatches, getPlayerProfile, staleSuffix } from "../api/client";
import { profileIconUrl, useManifest } from "../assets/tft";

const PLATFORMS = ["na1", "euw1", "eun1", "kr", "jp1", "br1", "la1", "la2", "oc1", "tr1", "ru"];

// While the server is syncing match history in the background, re-fetch at
// this interval so new matches appear as they're ingested.
const SYNC_POLL_MS = 3000;

export default function ProfilePage() {
  const { region, name, tag } = useParams();
  const navigate = useNavigate();

  const [riotId, setRiotId] = useState(name && tag ? `${name}#${tag}` : "");
  const [platform, setPlatform] = useState(region ?? "na1");

  const [profile, setProfile] = useState<PlayerProfile | null>(null);
  const [history, setHistory] = useState<PlayerMatches | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const manifest = useManifest();

  // Opening a profile loads its saved match history, and makes the server
  // sync newer matches from Riot if the history is out of date.
  useEffect(() => {
    if (!region || !name || !tag) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function loadMatches(puuid: string) {
      return getPlayerMatches(puuid, region!, 20).then((h) => {
        if (cancelled) return;
        setHistory(h);
        if (h.refreshing) {
          timer = setTimeout(() => loadMatches(puuid).catch(() => {}), SYNC_POLL_MS);
        }
      });
    }

    setLoading(true);
    setError(null);
    setProfile(null);
    setHistory(null);
    getPlayerProfile(region, name, tag)
      .then((p) => {
        if (cancelled) return;
        setProfile(p);
        return loadMatches(p.puuid);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "failed to load profile");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [region, name, tag]);

  const matches = history?.matches ?? [];

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const [n, t] = riotId.split("#");
    if (!n || !t) {
      setError('Riot ID must be in "Name#TAG" form');
      return;
    }
    navigate(`/players/${platform}/${encodeURIComponent(n)}/${encodeURIComponent(t)}`);
  }

  return (
    <div>
      <div className="panel">
        <form className="search-form" onSubmit={handleSubmit}>
          <select value={platform} onChange={(e) => setPlatform(e.target.value)}>
            {PLATFORMS.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
          <input
            name="riotId"
            placeholder="GameName#TAG"
            value={riotId}
            onChange={(e) => setRiotId(e.target.value)}
          />
          <button type="submit">Search</button>
        </form>
      </div>

      {loading && <p className="muted">Loading...</p>}
      {error && <div className="error-box">{error}</div>}

      {profile && (
        <div className="panel profile-header">
          {profileIconUrl(manifest, profile.profileIconId) && (
            <img
              className="profile-icon"
              src={profileIconUrl(manifest, profile.profileIconId)!}
              alt=""
              width={64}
              height={64}
            />
          )}
          <div>
            <h2>
              {profile.gameName}
              <span className="tag">#{profile.tagLine}</span>
            </h2>
            <p className="muted">
              {profile.platformRegion.toUpperCase()} &middot; Level {profile.summonerLevel} &middot; source:{" "}
              {profile.source}
            </p>
          </div>
        </div>
      )}

      {profile && (
        <div className="panel">
          <h3>Recent Matches</h3>
          {history && (
            <p className="muted">
              {history.syncedAt ? `Updated ${new Date(history.syncedAt).toLocaleTimeString()}` : "Not yet updated"}
              {history.refreshing && " · fetching new matches…"}
            </p>
          )}
          {history?.stale && (
            <div className="warning-box">
              Showing saved matches — couldn't update from Riot
              {staleSuffix(history.staleReason)}.
            </div>
          )}
          {history && matches.length === 0 && (
            <p className="muted">{history.refreshing ? "Loading matches from Riot…" : "No matches found for this player."}</p>
          )}
          {matches.length > 0 && (
            <table>
              <thead>
                <tr>
                  <th>Placement</th>
                  <th>Level</th>
                  <th>Set</th>
                  <th>Date</th>
                </tr>
              </thead>
              <tbody>
                {matches.map((m) => (
                  <tr key={m.matchId} className="clickable" onClick={() => navigate(`/matches/${m.matchId}`)}>
                    <td>
                      <span className={`placement placement-${m.placement}`}>#{m.placement}</span>
                    </td>
                    <td>{m.level}</td>
                    <td>Set {m.tftSetNumber}</td>
                    <td>{new Date(m.gameDatetime).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  );
}
