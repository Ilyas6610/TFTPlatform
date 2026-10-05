import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { ApiError, MatchDetail, getMatch } from "../api/client";
import { GameIcon } from "../assets/tft";

export default function MatchDetailPage() {
  const { matchId } = useParams();
  const [match, setMatch] = useState<MatchDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!matchId) return;
    setLoading(true);
    setError(null);
    getMatch(matchId)
      .then(setMatch)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "failed to load match"))
      .finally(() => setLoading(false));
  }, [matchId]);

  if (loading) return <p className="muted">Loading...</p>;
  if (error) return <div className="error-box">{error}</div>;
  if (!match) return null;

  const participants = [...match.info.participants].sort((a, b) => a.placement - b.placement);

  return (
    <div>
      <div className="panel">
        <h2>{match.metadata.match_id}</h2>
        <p className="muted">
          Set {match.info.tft_set_number} &middot; {match.info.tft_game_type} &middot;{" "}
          {new Date(match.info.game_datetime).toLocaleString()} &middot; source: {match.source}
        </p>
      </div>

      {participants.map((p) => (
        <div className="panel" key={p.puuid}>
          <h3>
            <span className={`placement placement-${p.placement}`}>#{p.placement}</span> Level {p.level}
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
      ))}
    </div>
  );
}
