import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  ApiError,
  AugmentStat,
  TraitStat,
  UnitStat,
  getMetaAugments,
  getMetaTraits,
  getMetaUnits,
} from "../api/client";
import { GameLabel } from "../assets/tft";
import { CURRENT_TFT_SET } from "../config";

type Tab = "units" | "traits" | "augments";

export default function MetaStatsPage() {
  const { set = String(CURRENT_TFT_SET) } = useParams();
  const navigate = useNavigate();
  const setNumber = Number(set);

  const [tab, setTab] = useState<Tab>("units");
  const [units, setUnits] = useState<UnitStat[]>([]);
  const [traits, setTraits] = useState<TraitStat[]>([]);
  const [augments, setAugments] = useState<AugmentStat[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    Promise.all([getMetaUnits(setNumber), getMetaTraits(setNumber), getMetaAugments(setNumber)])
      .then(([u, t, a]) => {
        setUnits(u);
        setTraits(t);
        setAugments(a);
      })
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "failed to load meta stats"))
      .finally(() => setLoading(false));
  }, [setNumber]);

  return (
    <div>
      <div className="set-picker">
        <label htmlFor="set-input" className="muted">
          TFT Set:
        </label>
        <input
          id="set-input"
          type="number"
          value={set}
          style={{ width: 70 }}
          onChange={(e) => navigate(`/meta/${e.target.value}`)}
        />
        <button type="button" onClick={() => setTab("units")} disabled={tab === "units"}>
          Units
        </button>
        <button type="button" onClick={() => setTab("traits")} disabled={tab === "traits"}>
          Traits
        </button>
        <button type="button" onClick={() => setTab("augments")} disabled={tab === "augments"}>
          Augments
        </button>
      </div>

      {loading && <p className="muted">Loading...</p>}
      {error && <div className="error-box">{error}</div>}

      {!loading && !error && tab === "units" && (
        <div className="panel">
          {units.length === 0 && <p className="muted">No unit data for this set yet.</p>}
          {units.length > 0 && (
            <table>
              <thead>
                <tr>
                  <th>Unit</th>
                  <th>Games</th>
                  <th>Avg Place</th>
                  <th>Win %</th>
                  <th>Top 4 %</th>
                  <th>Pick %</th>
                </tr>
              </thead>
              <tbody>
                {units.map((u) => (
                  <tr key={u.characterId}>
                    <td>
                      <GameLabel kind="champions" id={u.characterId} />
                    </td>
                    <td>{u.gamesPlayed}</td>
                    <td>{u.avgPlacement.toFixed(2)}</td>
                    <td>{pct(u.winRate)}</td>
                    <td>{pct(u.top4Rate)}</td>
                    <td>{pct(u.pickRate)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {!loading && !error && tab === "traits" && (
        <div className="panel">
          {traits.length === 0 && <p className="muted">No trait data for this set yet.</p>}
          {traits.length > 0 && (
            <table>
              <thead>
                <tr>
                  <th>Trait</th>
                  <th>Tier</th>
                  <th>Games</th>
                  <th>Avg Place</th>
                  <th>Win %</th>
                  <th>Top 4 %</th>
                </tr>
              </thead>
              <tbody>
                {traits.map((t, i) => (
                  <tr key={`${t.traitName}-${t.traitTier}-${i}`}>
                    <td>
                      <GameLabel kind="traits" id={t.traitName} />
                    </td>
                    <td>{t.traitTier}</td>
                    <td>{t.gamesPlayed}</td>
                    <td>{t.avgPlacement.toFixed(2)}</td>
                    <td>{pct(t.winRate)}</td>
                    <td>{pct(t.top4Rate)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {!loading && !error && tab === "augments" && (
        <div className="panel">
          {augments.length === 0 && <p className="muted">No augment data for this set yet.</p>}
          {augments.length > 0 && (
            <table>
              <thead>
                <tr>
                  <th>Augment</th>
                  <th>Games</th>
                  <th>Avg Place</th>
                  <th>Win %</th>
                  <th>Top 4 %</th>
                </tr>
              </thead>
              <tbody>
                {augments.map((a) => (
                  <tr key={a.augmentId}>
                    <td>
                      <GameLabel kind="augments" id={a.augmentId} />
                    </td>
                    <td>{a.gamesPlayed}</td>
                    <td>{a.avgPlacement.toFixed(2)}</td>
                    <td>{pct(a.winRate)}</td>
                    <td>{pct(a.top4Rate)}</td>
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

function pct(n: number): string {
  return `${(n * 100).toFixed(1)}%`;
}
