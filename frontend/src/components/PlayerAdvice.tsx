import { ReactNode, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { AdviceBuild, AdviceUnit, PlayerAdvice as Advice, getPlayerAdvice } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Names, avg } from "./stats";

/**
 * What stands out in a player's games against everyone's in the same scope:
 * units they do clearly worse or better with than their usual (everyone's
 * average with the unit, shifted by how much better or worse than everyone
 * they usually place), and builds everyone does clearly better with than
 * the one they use. Only differences beyond a few games' noise are listed.
 */
export function PlayerAdvice({
  puuid,
  scope,
  refreshKey,
  names,
  unitIcon,
}: {
  puuid: string;
  /** set/queue query, as for stats. */
  scope: string;
  /** Changes when the player's stored games change. */
  refreshKey: string;
  names: Names;
  unitIcon: (id: string, size: number) => ReactNode;
}) {
  const [advice, setAdvice] = useState<Advice | null>(null);

  useEffect(() => {
    let cancelled = false;
    getPlayerAdvice(puuid, scope)
      .then((a) => !cancelled && setAdvice(a))
      .catch(() => !cancelled && setAdvice(null));
    return () => {
      cancelled = true;
    };
  }, [puuid, scope, refreshKey]);

  if (!advice) return null;
  const empty = advice.weak.length + advice.strong.length + advice.builds.length === 0;
  const edge = advice.edge;

  const items = (ids: string[]) => (
    <span className="advice-items">
      {ids.map((id, i) => (
        <GameIcon key={i} kind="items" id={id} size={20} fallbackSrc={names.icon(id)} fallbackName={names.name(id)} />
      ))}
    </span>
  );

  const unitRow = (u: AdviceUnit, worse: boolean) => (
    <li key={u.unit}>
      <span className="game-label">
        {unitIcon(u.unit, 22)}
        {names.name(u.unit)}
      </span>
      <span>
        <strong className={worse ? "bad" : "good"}>{avg(u.avg)}</strong>
        <span className="muted">
          {" "}
          avg in {u.games} games · expected {avg(u.expected)} (everyone {avg(u.metaAvg)})
        </span>
      </span>
    </li>
  );

  const buildRow = (b: AdviceBuild) => {
    const q = new URLSearchParams(scope);
    q.append("unit", `${b.unit}:${b.better.items.join(",")}`);
    return (
      <li key={b.unit} className="advice-build">
        <span className="game-label">
          {unitIcon(b.unit, 22)}
          {names.name(b.unit)}
        </span>
        <span>
          {items(b.theirs.items)}
          <span className="muted">
            {" "}
            their usual ({b.theirs.boards} games) · everyone{" "}
            <span className="bad">{avg(b.theirsMeta.avgPlacement)}</span> in {b.theirsMeta.boards}
          </span>
        </span>
        <span>
          <span className="muted" aria-hidden="true">
            →{" "}
          </span>
          <Link to={`/explore?${q}`} title="Open in the explorer">
            {items(b.better.items)}
          </Link>
          <span className="muted">
            {" "}
            everyone <span className="good">{avg(b.better.avgPlacement)}</span> in {b.better.boards}
          </span>
        </span>
      </li>
    );
  };

  return (
    <div className="panel">
      <h3>Advice</h3>
      <p className="muted">
        Against everyone's games in the same scope. They usually place{" "}
        {Math.abs(edge) < 0.05
          ? "like everyone"
          : `${avg(Math.abs(edge))} ${edge < 0 ? "better" : "worse"} than everyone`}
        , so a unit's expected average is everyone's shifted by that. Only differences beyond a few games' luck are
        listed.
      </p>
      {empty ? (
        <p className="muted">Nothing stands out yet: a unit needs at least 5 of their games here.</p>
      ) : (
        <div className="advice-lists">
          {advice.weak.length > 0 && (
            <div>
              <div className="meta-section-title muted">Worse than usual with</div>
              <ul className="advice-list">{advice.weak.map((u) => unitRow(u, true))}</ul>
            </div>
          )}
          {advice.strong.length > 0 && (
            <div>
              <div className="meta-section-title muted">Better than usual with</div>
              <ul className="advice-list">{advice.strong.map((u) => unitRow(u, false))}</ul>
            </div>
          )}
          {advice.builds.length > 0 && (
            <div className="advice-builds">
              <div className="meta-section-title muted">Builds others do better with</div>
              <ul className="advice-list">{advice.builds.map(buildRow)}</ul>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
