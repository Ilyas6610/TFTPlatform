import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { ApiError, MatchDetail, getMatch } from "../api/client";

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
          <div className="unit-grid">
            {p.units.map((u, i) => (
              <span className="unit-chip" key={i}>
                {formatId(u.character_id)} {"★".repeat(u.tier)}
              </span>
            ))}
          </div>
          <div className="unit-grid" style={{ marginTop: 8 }}>
            {p.traits
              .filter((t) => t.tier_current > 0)
              .map((t, i) => (
                <span className="unit-chip" key={i}>
                  {formatId(t.name)} ({t.num_units})
                </span>
              ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function formatId(id: string): string {
  const idx = id.indexOf("_");
  return idx >= 0 ? id.slice(idx + 1) : id;
}
