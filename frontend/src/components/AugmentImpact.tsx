import { useMemo, useState } from "react";
import { SetData } from "../api/client";
import { GameIcon } from "../assets/tft";
import { AUGMENT_SLOTS, augmentTier, availableAt } from "../planner/augmentStages";
import { SHOP } from "../planner/augmentPlan";
import { ASSUMPTIONS, AugmentScore, Confidence, rankAugments } from "../planner/augmentScore";
import { BOARD_VALUE_FIT, ITEM_GAIN } from "../planner/boardValue";
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

/** A gain in placement as the change in average placement: a gain of 0.31 reads "−0.31" (a lower placement is better). */
const place = (x: number) => `${x >= 0 ? "−" : "+"}${Math.abs(x).toFixed(2)}`;

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
        An estimate, not a measurement: Riot's match data has no augments, so nothing here is learned from augment results. Items, star-ups and stat
        bonuses are priced in average placement with a model fitted on {BOARD_VALUE_FIT.fittedOn.lobbies.toLocaleString()} stored lobbies; the rest is converted through the
        shop. A lower number is better. Open a row for the arithmetic.
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
        <summary className="muted">How it's worked out, and what to trust</summary>
        <ul>
          <li>
            <strong>Fitted from boards</strong> (not guessed): within a lobby, a board's placement against the stars of its units by cost, the items it
            holds (by who holds them) and its level. On the newest 40% of lobbies it ranks boards with a Spearman correlation of {BOARD_VALUE_FIT.heldOut.spearman}
            (level alone {BOARD_VALUE_FIT.heldOut.levelOnlySpearman}, gold cost alone {BOARD_VALUE_FIT.heldOut.goldCostSpearman}). An average item is worth {ITEM_GAIN.toFixed(2)} placement,
            a star-up its fitted gain split over the copies it needs. This is correlation (strong players hold more of everything), so it's scaled
            down by {ASSUMPTIONS.calibration} and totals are capped near {ASSUMPTIONS.softCap}.
          </li>
          <li>
            <strong>Stat bonuses</strong>: team strength (the geometric mean of total effective HP and total DPS from base stats, stars and items) is recomputed with the
            bonus on the units it covers; the lift is converted to placement through what one extra item does to the same strength. Ability numbers, traits and
            positioning beyond front and back rows aren't modelled.
          </li>
          <li>
            <strong>Copies</strong>: a copy of a unit still short of its goal star ({SHOP.slots} shop slots, {SHOP.rerollGold} gold a reroll, the standard odds by level) is worth its share of the
            star-up; a board of cheap carries counts copies up to {1 + ASSUMPTIONS.rerollTempo}× and a board of expensive carries barely (the tempo of a reroll comp's early star-ups
            can't be measured from final boards: that factor, and {Math.round(ASSUMPTIONS.tempoPerStage * 100)}% extra per stage left for early gold, copies and items, are assumptions).
          </li>
          <li>Built around a trait the board doesn't play (Elderwood, Solar...): counted at {ASSUMPTIONS.offTraitShare * 100}%.</li>
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
          <span className="augment-impact-value">{place(impact)}</span>
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
                  <td className="num">{place(p.dp)}</td>
                  <td className="muted">{p.note ?? ""}</td>
                </tr>
              ))}
              <tr>
                <th>Total</th>
                <th className="num">{place(impact)}</th>
                <th className="muted">average placement</th>
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
