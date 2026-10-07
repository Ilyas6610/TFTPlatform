import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  ApiError,
  MatchDetail,
  MatchLobby,
  MatchLobbyPlayer,
  MatchParticipant,
  getMatch,
  getMatchLobby,
} from "../api/client";
import { GameIcon } from "../assets/tft";
import { lobbyText, rankText } from "../components/LPChart";

const DOUBLE_UP = 1160;

/**
 * Double Up teams, best first. Riot ranks a team's two players
 * consecutively (1-2, 3-4, ...); Set 17 payloads also name the team
 * (partner_group_id), Set 18's don't.
 */
function doubleUpTeams(participants: MatchParticipant[]): MatchParticipant[][] {
  const teams = new Map<string, MatchParticipant[]>();
  for (const p of participants) {
    const key = p.partner_group_id != null ? `g${p.partner_group_id}` : `p${Math.ceil(p.placement / 2)}`;
    teams.set(key, [...(teams.get(key) ?? []), p]);
  }
  return [...teams.values()].sort((a, b) => a[0].placement - b[0].placement);
}

export default function MatchDetailPage() {
  const { matchId } = useParams();
  const [match, setMatch] = useState<MatchDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Each player's rank around the game and the lobby's average; loaded
  // after the match, from stored data only (a just-fetched match has its
  // rows by then).
  const [lobby, setLobby] = useState<MatchLobby | null>(null);

  useEffect(() => {
    if (!matchId) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setLobby(null);
    getMatch(matchId)
      .then((m) => {
        if (cancelled) return;
        setMatch(m);
        getMatchLobby(matchId)
          .then((l) => !cancelled && setLobby(l))
          .catch(() => {});
      })
      .catch((e: unknown) => !cancelled && setError(e instanceof ApiError ? e.message : "failed to load match"))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [matchId]);

  if (loading) return <p className="muted">Loading...</p>;
  if (error) return <div className="error-box">{error}</div>;
  if (!match) return null;

  const participants = [...match.info.participants].sort((a, b) => a.placement - b.placement);
  // Match ids are prefixed with their platform, e.g. "NA1_5654662880".
  const platform = match.metadata.match_id.split("_")[0].toLowerCase();
  const ranks = new Map(lobby?.players.map((r) => [r.puuid, r]) ?? []);

  return (
    <div>
      <div className="panel">
        <h2>{match.metadata.match_id}</h2>
        <p className="muted">
          Set {match.info.tft_set_number} &middot; {match.info.tft_game_type} &middot;{" "}
          {new Date(match.info.game_datetime).toLocaleString()} &middot; source: {match.source}
        </p>
        {lobby && lobby.known > 0 && (
          <p
            className="muted"
            title={
              "Average Ranked standing of the ranked players around this game" +
              (lobby.known < lobby.total
                ? ". Unranked players are usually below Master, so the lobby was likely weaker than this average"
                : "")
            }
          >
            Lobby ≈ {lobbyText(lobby.average, lobby.averageTier)} ({lobby.known} of {lobby.total} players ranked)
          </p>
        )}
      </div>

      {match.info.queue_id === DOUBLE_UP
        ? doubleUpTeams(participants).map((team, i) => (
            <div className={`du-team du-team-${i < 2 ? "top" : "bottom"}`} key={team[0].puuid}>
              <div className="du-team-head">
                <span className={`placement placement-${i * 2 + 1}`}>#{i + 1}</span> Team
              </div>
              {team.map((p) => (
                <Participant key={p.puuid} p={p} platform={platform} rank={ranks.get(p.puuid)} />
              ))}
            </div>
          ))
        : participants.map((p) => <Participant key={p.puuid} p={p} platform={platform} rank={ranks.get(p.puuid)} />)}
    </div>
  );
}

function Participant({ p, platform, rank }: { p: MatchParticipant; platform: string; rank?: MatchLobbyPlayer }) {
  return (
    <div className="panel">
      <h3 className="participant-header">
        <span className={`placement placement-${p.placement}`}>#{p.placement}</span>
        {p.riotIdGameName ? (
          <Link
            to={`/players/${platform}/${encodeURIComponent(p.riotIdGameName)}/${encodeURIComponent(p.riotIdTagline ?? "")}`}
          >
            {p.riotIdGameName}
            <span className="muted">#{p.riotIdTagline}</span>
          </Link>
        ) : (
          <span className="muted">Unknown player</span>
        )}
        {rank && (
          <span
            className="muted participant-rank"
            title={
              rank.current ? "Today's ladder rank (none recorded near this game)" : "Ranked standing around this game"
            }
          >
            {rankText({ tier: rank.tier, rank: rank.rank ?? "", leaguePoints: rank.leaguePoints })}
            {rank.current ? " (now)" : ""}
          </span>
        )}
        <span className="muted participant-level">Level {p.level}</span>
      </h3>
      <div className="trait-row">
        {p.traits
          .filter((t) => t.tier_current > 0)
          .sort((a, b) => b.style - a.style || b.num_units - a.num_units)
          .map((t) => (
            <span className={`trait-badge trait-style-${t.style}`} key={t.name}>
              <GameIcon kind="traits" id={t.name} size={20} />
              {t.num_units}
            </span>
          ))}
        {p.augments?.map((a) => (
          <GameIcon kind="augments" id={a} size={28} key={a} className="augment-icon" />
        ))}
      </div>
      <div className="unit-grid">
        {p.units.map((u, i) => (
          <div className="unit-card" key={i}>
            <span className="unit-stars">{"★".repeat(u.tier)}</span>
            <GameIcon kind="champions" id={u.character_id} size={48} />
            <span className="unit-items">
              {u.itemNames.map((item, j) => (
                <GameIcon kind="items" id={item} size={16} key={j} />
              ))}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
