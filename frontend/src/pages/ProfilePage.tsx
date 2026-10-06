import { FormEvent, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  ApiError,
  PlayerMatchSummary,
  PlayerMatches,
  PlayerProfile,
  RankEntry,
  SetData,
  getPlayerProfile,
  getSetData,
} from "../api/client";
import { profileIconUrl, useManifest } from "../assets/tft";
import { LPHistory } from "../components/LPHistory";
import { MatchHistory } from "../components/MatchHistory";
import { PlayerStats } from "../components/PlayerStats";
import { buildNames } from "../components/stats";
import { CURRENT_TFT_SET } from "../config";

const PLATFORMS = ["na1", "euw1", "eun1", "kr", "jp1", "br1", "la1", "la2", "oc1", "tr1", "ru"];

export default function ProfilePage() {
  const { region, name, tag } = useParams();
  const navigate = useNavigate();

  const [riotId, setRiotId] = useState(name && tag ? `${name}#${tag}` : "");
  const [platform, setPlatform] = useState(region ?? "na1");

  const [profile, setProfile] = useState<PlayerProfile | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [set, setSet] = useState(CURRENT_TFT_SET);
  const [setData, setSetData] = useState<SetData | null>(null);
  // The newest games (first history page), for the stats' recent form.
  const [recent, setRecent] = useState<PlayerMatchSummary[]>([]);
  // Bumped when the first history page changes, so stats include new games.
  const [statsVersion, setStatsVersion] = useState(0);
  const manifest = useManifest();
  // Update button: bumping the signal makes the history sync now; `syncing`
  // follows the history's own flag while the server fetches games.
  const [refreshSignal, setRefreshSignal] = useState(0);
  const [updating, setUpdating] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [updateNote, setUpdateNote] = useState<string | null>(null);

  // Opening a profile looks up the player; the history and stats panels
  // then load (and the history makes the server sync newer games).
  useEffect(() => {
    if (!region || !name || !tag) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setProfile(null);
    setRecent([]);
    setRanks([]);
    setRefreshSignal(0);
    setUpdating(false);
    setSyncing(false);
    setUpdateNote(null);
    getPlayerProfile(region, name, tag)
      .then((p) => !cancelled && setProfile(p))
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "failed to load profile");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [region, name, tag]);

  useEffect(() => {
    let cancelled = false;
    getSetData(set)
      .then((d) => !cancelled && setSetData(d))
      .catch(() => !cancelled && setSetData(null));
    return () => {
      cancelled = true;
    };
  }, [set]);

  const names = useMemo(() => buildNames(setData), [setData]);
  const costs = useMemo(() => new Map(setData?.units.map((u) => [u.apiName, u.cost]) ?? []), [setData]);
  const [ranks, setRanks] = useState<RankEntry[]>([]);
  const onFirstPage = (p: PlayerMatches) => {
    setSyncing(p.refreshing);
    setRecent(p.matches);
    if (p.ranks) setRanks(p.ranks);
  };
  // Games were added to the history (newest synced or an older page fetched):
  // stats are computed from stored games, so reload them.
  const onGamesAdded = () => setStatsVersion((v) => v + 1);

  // Re-read the profile (name, icon, level) and sync the newest games now;
  // stats and LP reload as the games arrive.
  function handleUpdate() {
    if (!region || !name || !tag) return;
    setUpdating(true);
    setUpdateNote(null);
    setRefreshSignal((n) => n + 1);
    getPlayerProfile(region, name, tag)
      .then(setProfile)
      .catch(() => {});
  }
  const onRefreshResult = (p: PlayerMatches | null) => {
    setUpdating(false);
    if (!p) setUpdateNote("couldn't update the profile");
    else if (p.refreshTooSoon) setUpdateNote("Already up to date.");
  };

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
          <input name="riotId" placeholder="GameName#TAG" value={riotId} onChange={(e) => setRiotId(e.target.value)} />
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
              {profile.platformRegion.toUpperCase()}
              {/* Riot reports level 1 for many accounts since TFT's own client; that says nothing. */}
              {profile.summonerLevel > 1 && <> &middot; Level {profile.summonerLevel}</>}
            </p>
          </div>
          {ranks.length > 0 && (
            <div className="rank-badges">
              {ranks.map((r) => (
                <RankBadge key={r.queueType} entry={r} />
              ))}
            </div>
          )}
          <div className="profile-update">
            <button type="button" onClick={handleUpdate} disabled={updating || syncing}>
              {updating || syncing ? "Updating…" : "Update"}
            </button>
            {updateNote && <span className="muted">{updateNote}</span>}
          </div>
        </div>
      )}

      {profile && (
        <LPHistory
          puuid={profile.puuid}
          reloadKey={ranks.map((r) => `${r.queueType}:${r.leaguePoints}:${r.wins + r.losses}`).join(",")}
        />
      )}

      {profile && (
        <PlayerStats
          key={profile.puuid}
          refreshKey={statsVersion}
          puuid={profile.puuid}
          set={set}
          onSet={setSet}
          names={names}
          costs={costs}
          recent={recent}
          region={profile.platformRegion}
        />
      )}

      {profile && (
        <MatchHistory
          puuid={profile.puuid}
          region={profile.platformRegion}
          names={names}
          costs={costs}
          onFirstPage={onFirstPage}
          onGamesAdded={onGamesAdded}
          refreshSignal={refreshSignal}
          onRefreshResult={onRefreshResult}
        />
      )}
    </div>
  );
}

const QUEUE_LABEL: Record<string, string> = { RANKED_TFT: "Ranked", RANKED_TFT_DOUBLE_UP: "Double Up" };
const APEX = new Set(["MASTER", "GRANDMASTER", "CHALLENGER"]);

function RankBadge({ entry: r }: { entry: RankEntry }) {
  const games = r.wins + r.losses;
  const tier = r.tier.charAt(0) + r.tier.slice(1).toLowerCase();
  return (
    <div
      className={`rank-badge tier-${r.tier.toLowerCase()}`}
      title={`As of ${new Date(r.fetchedAt).toLocaleString()}`}
    >
      <span className="muted">{QUEUE_LABEL[r.queueType] ?? r.queueType}</span>
      <strong>
        {tier}
        {APEX.has(r.tier) ? "" : ` ${r.rank}`} · {r.leaguePoints} LP
      </strong>
      <span className="muted">
        {games} games · {r.wins} top 4 ({Math.round((r.wins / Math.max(1, games)) * 100)}%)
      </span>
    </div>
  );
}
