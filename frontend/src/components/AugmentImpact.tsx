import { useMemo, useState } from "react";
import { SetData } from "../api/client";
import { GameIcon } from "../assets/tft";
import { AUGMENT_SLOTS, augmentTier, availableAt } from "../planner/augmentStages";
import { ASSUMPTIONS, AugmentScore, Confidence, rankAugments } from "../planner/augmentScore";
import { Board } from "../planner/board";
import { Names } from "./stats";

const TIER: Record<number, string> = { 1: "Silver", 2: "Gold", 3: "Prismatic" };
const SHOWN = 12;
const CONFIDENCE_HINT: Record<Confidence, string> = {
  high: "All of the augment's text was read",
  medium: "Part of the text wasn't read, or an assumption was needed",
  low: "Only a small part of the text was read",
  none: "Not scored",
};

const pct = (x: number) => `${x >= 0 ? "+" : ""}${(x * 100).toFixed(1)}%`;

/**
 * Prototype: what each augment would add to the board in the planner, as a
 * share of the board's value. An estimate from set data (see
 * planner/augmentScore.ts), not from game results: Riot's match data has no
 * augments, so there is nothing to measure it against.
 */
export function AugmentImpact({
  board,
  data,
  names,
  onPick,
}: {
  board: Board;
  data: SetData;
  names: Names;
  onPick: (slot: number, apiName: string) => void;
}) {
  // Default to the first slot that's still empty.
  const firstOpen = board.augments.findIndex((a) => !a);
  const [slotChoice, setSlot] = useState<number | null>(null);
  const slot = slotChoice ?? (firstOpen >= 0 ? firstOpen : 0);
  const stage = AUGMENT_SLOTS[slot].stage;
  const [all, setAll] = useState(false);

  const ranked = useMemo(() => {
    const offered = data.augments.filter((a) => availableAt(a, slot) && !board.augments.some((c, i) => c === a.apiName && i !== slot));
    return rankAugments(offered, board, data, stage);
  }, [board, data, slot, stage]);

  const scored = ranked.filter((r) => r.impact !== null);
  const unscored = ranked.filter((r) => r.impact === null);
  const top = scored[0]?.impact ?? 0;
  const shown = all ? scored : scored.slice(0, SHOWN);

  return (
    <div className="panel augment-impact">
      <h3>
        Augment impact <span className="chip">prototype</span>
      </h3>
      <p className="muted augment-impact-note">
        An estimate from the set's numbers, not from game results (Riot's match data has no augments). Stats are worth what they add to this
        board's strength; gold, items and units are priced in gold. Open a row for the arithmetic.
      </p>
      <div className="mode-tabs" role="tablist" aria-label="Augment stage">
        {AUGMENT_SLOTS.map((s, i) => (
          <button key={s.stage} type="button" role="tab" aria-selected={slot === i} onClick={() => setSlot(i)}>
            {s.stage} <span className="muted">{board.augments[i] ? names.name(board.augments[i]!) : s.label.split(" ")[0]}</span>
          </button>
        ))}
      </div>

      {board.units.length === 0 ? (
        <p className="muted">Put units on the board to see how much each augment would add.</p>
      ) : (
        <>
          <ul className="augment-impact-list">
            {shown.map((r) => (
              <AugmentRow key={r.apiName} r={r} top={top} names={names} data={data} onPick={() => onPick(slot, r.apiName)} />
            ))}
          </ul>
          {scored.length > SHOWN && (
            <button type="button" className="show-more" onClick={() => setAll((v) => !v)}>
              {all ? "Show the top 12" : `Show all ${scored.length}`}
            </button>
          )}
          {unscored.length > 0 && (
            <details className="augment-impact-unscored">
              <summary className="muted">Not scored ({unscored.length}): their effect isn't something these numbers can price</summary>
              <ul>
                {unscored.map((r) => (
                  <li key={r.apiName} title={r.caveats.join("\n")}>
                    {r.name}
                    <span className="muted"> · {r.caveats[0] ?? ""}</span>
                  </li>
                ))}
              </ul>
            </details>
          )}
        </>
      )}

      <details className="augment-impact-assumptions">
        <summary className="muted">Assumptions behind the numbers</summary>
        <ul>
          <li>
            Team strength = the geometric mean of total effective HP and total DPS. Each held item adds{" "}
            {ASSUMPTIONS.itemHealth * 100}% health, {ASSUMPTIONS.itemDamage * 100}% attack damage and {ASSUMPTIONS.itemAbility * 100}% ability power;
            ability damage is {ASSUMPTIONS.abilityShare * 100}% of a unit's damage; bonuses are averaged over a {ASSUMPTIONS.fightSeconds}s fight.
          </li>
          <li>
            Prices in gold: a reroll {ASSUMPTIONS.ge.reroll}, a component {ASSUMPTIONS.ge.component}, a completed item {ASSUMPTIONS.ge.completed}, an
            artifact {ASSUMPTIONS.ge.artifact}, an emblem {ASSUMPTIONS.ge.emblem}; a unit costs its cost × 1, 3 or 9 by star. Only {ASSUMPTIONS.usefulness.gold * 100}% of
            gold, {ASSUMPTIONS.usefulness.item * 100}% of items and {ASSUMPTIONS.usefulness.unit * 100}% of units turn into board value.
          </li>
          <li>The board's value is its units' gold cost plus {ASSUMPTIONS.ge.itemOnBoard} per item. Early gold is worth a little more.</li>
          <li>Traits, positioning beyond front and back rows, opponents and ability numbers aren't modelled.</li>
        </ul>
      </details>
    </div>
  );
}

function AugmentRow({ r, top, names, data, onPick }: { r: AugmentScore; top: number; names: Names; data: SetData; onPick: () => void }) {
  const impact = r.impact ?? 0;
  const width = top > 0 ? Math.max(2, (Math.max(0, impact) / top) * 100) : 0;
  const desc = data.augments.find((a) => a.apiName === r.apiName)?.desc;
  return (
    <li className="augment-impact-row">
      <details>
        <summary>
          <GameIcon kind="augments" id={r.apiName} size={28} fallbackSrc={names.icon(r.apiName)} fallbackName={names.name(r.apiName)} />
          <span className="augment-impact-name" title={desc}>
            {r.name}
            {TIER[augmentTier(r)] && <span className="muted"> · {TIER[augmentTier(r)]}</span>}
          </span>
          <span className="augment-impact-bar" aria-hidden>
            <span className={impact < 0 ? "neg" : undefined} style={{ width: `${width}%` }} />
          </span>
          <span className="augment-impact-value">{pct(impact)}</span>
          <span className={`augment-impact-conf conf-${r.confidence}`} title={CONFIDENCE_HINT[r.confidence]}>
            {r.confidence}
          </span>
        </summary>
        <div className="augment-impact-detail">
          {desc && <p className="muted">{desc}</p>}
          <table>
            <tbody>
              {r.parts.map((p, i) => (
                <tr key={i}>
                  <td>{p.label}</td>
                  <td className="num">{p.ge >= 0 ? "+" : ""}{p.ge.toFixed(1)} gold</td>
                  <td className="muted">{p.note ?? ""}</td>
                </tr>
              ))}
              <tr>
                <th>Total</th>
                <th className="num">{r.ge!.toFixed(1)} gold</th>
                <th className="muted">{pct(impact)} of the board's value</th>
              </tr>
            </tbody>
          </table>
          {r.caveats.length > 0 && (
            <ul className="muted">
              {r.caveats.map((c, i) => (
                <li key={i}>{c}</li>
              ))}
            </ul>
          )}
          <button type="button" onClick={onPick}>
            Use this augment
          </button>
        </div>
      </details>
    </li>
  );
}
