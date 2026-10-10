import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { SetData, SuggestComp, getExploreSuggest } from "../api/client";
import { GameIcon } from "../assets/tft";
import { AUGMENT_SLOTS, augmentTier, availableAt } from "../planner/augmentStages";
import { SHOP } from "../planner/augmentPlan";
import { ASSUMPTIONS, AugmentScore, BoardMode, Confidence, picksCoverage, rankAugments } from "../planner/augmentScore";
import { archetypeFrom, archetypeOf, needsOf, rankByNeeds } from "../planner/augmentNeeds";
import { BOARD_VALUE_FIT, ITEM_GAIN } from "../planner/boardValue";
import { Board, COLS, encodeBoard } from "../planner/board";
import { Names } from "./stats";

/** Suggested boards need this many stored games: a comp seen a handful of times isn't a plan. */
const MIN_COMP_BOARDS = 20;

/** A real comp's most played board as a planner board: melee units in front, the rest behind, itemized carries last row. */
function compBoard(sg: SuggestComp, data: SetData): Board {
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  let front = 0;
  let back = 3 * COLS;
  let mid = 2 * COLS;
  const units = sg.comp.board
    .filter((u) => defs.has(u.id))
    .map((u) => {
      const melee = (defs.get(u.id)!.stats.range ?? 1) <= 1;
      const pos = melee && front < COLS ? front++ : back < 4 * COLS ? back++ : mid++;
      return { id: u.id, star: Math.max(1, Math.min(3, u.star)) as 1 | 2 | 3, pos, items: u.items.slice(0, 3), extra: [] as string[] };
    });
  const slots = sg.comp.board.reduce((a, u) => a + (u.id.includes("ElderDragon") ? 2 : 1), 0);
  return { set: data.setNumber, title: "", level: Math.max(7, Math.min(10, slots)), augments: [null, null, null], units };
}

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
 * Prototype: the average placement each augment would gain on the board in the
 * planner. An estimate (see planner/augmentScore.ts), not a measurement: Riot's
 * match data has no augments, so there is nothing to measure it against.
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
  // "goal": the board is what the player is building towards (the usual use of a planner); "now": what they have.
  const [mode, setMode] = useState<BoardMode>("goal");
  // Default to the first slot that's still empty.
  const firstOpen = board.augments.findIndex((a) => !a);
  const [slotChoice, setSlot] = useState<number | null>(null);
  // "What I have now": the stage follows the board's level (level 8 is usually 4-2) until a tab is chosen.
  const levelSlot = board.level >= 8 ? 2 : board.level >= 6 ? 1 : 0;
  const slot = slotChoice ?? (mode === "now" && board.units.length ? Math.max(levelSlot, firstOpen >= 0 ? firstOpen : 0) : firstOpen >= 0 ? firstOpen : 0);
  const stage = AUGMENT_SLOTS[slot].stage;
  const [all, setAll] = useState(false);
  // An in-game offer is three augments of one tier: 0 shows every tier, 1-3 one of them.
  const [tierShown, setTierShown] = useState(0);

  // Ranking all the augments takes tens of milliseconds: let the board editing stay responsive and rank when it settles.
  const settled = useDeferredValue(board);

  // "What I have now": the boards the player could be building, from real comps that share their units and items
  // (the Explorer's build advisor); the chosen one is the goal the augments are scored towards.
  const [suggestions, setSuggestions] = useState<SuggestComp[] | null>(null);
  const [goalChoice, setGoalChoice] = useState(0);
  const haveKey = useMemo(
    () => [...settled.units.map((u) => u.id), "|", ...settled.units.flatMap((u) => u.items)].join(","),
    [settled],
  );
  useEffect(() => {
    if (mode !== "now" || settled.units.length === 0) return;
    const q = new URLSearchParams({ set: String(data.setNumber), queue: "1100" });
    settled.units.forEach((u) => q.append("have_unit", u.id));
    settled.units.forEach((u) => u.items.forEach((i) => q.append("have_item", i)));
    let live = true;
    getExploreSuggest(q.toString())
      .then(
        (r) =>
          live &&
          setSuggestions(
            r.comps
              .filter((c) => c.comp.boards >= MIN_COMP_BOARDS && c.have.length >= 2)
              // A reroll comp is no plan for a board that is past its reroll (level 8 without its carries at 2 stars).
              .filter((c) => {
                const goal = compBoard(c, data);
                return archetypeFrom(goal, data, settled) === archetypeOf(goal, data);
              })
              .slice(0, 3),
          ),
      )
      .catch(() => live && setSuggestions([]));
    setGoalChoice(0);
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, haveKey, data.setNumber]);
  const suggested = mode === "now" ? suggestions?.[goalChoice] : undefined;
  const suggestedGoal = useMemo(() => (suggested ? compBoard(suggested, data) : null), [suggested, data]);
  const { ranked, moved } = useMemo(() => {
    const offered = data.augments.filter((a) => availableAt(a, slot) && !settled.augments.some((c, i) => c === a.apiName && i !== slot));
    // Augments picked at earlier stages are already working: each one here is scored on top of them.
    const earlier = settled.augments.slice(0, slot).filter((a): a is string => !!a);
    // "My goal": what the board still needs (econ, items, combat, copies) at this stage. "What I have now": the same,
    // towards the chosen suggested board, starting from the player's board (without a suggestion, the older pricing).
    const rank = (given: string[]) =>
      mode === "goal"
        ? rankByNeeds(offered, settled, data, stage, given)
        : suggestedGoal
          ? rankByNeeds(offered, suggestedGoal, data, stage, given, undefined, settled)
          : rankAugments(offered, settled, data, stage, undefined, given, mode);
    const withPicks = rank(earlier);
    // What the earlier picks changed at the top of the list (the rows themselves say "was −0.xx").
    if (!earlier.length) return { ranked: withPicks, moved: null };
    const top = (r: AugmentScore[]) => r.filter((x) => x.impact !== null).slice(0, 5).map((x) => x.name);
    const before = top(rank([]));
    const after = top(withPicks);
    return { ranked: withPicks, moved: { out: before.filter((n) => !after.includes(n)), in: after.filter((n) => !before.includes(n)) } };
  }, [settled, data, slot, stage, mode, suggestedGoal]);
  const coverage = useMemo(() => {
    const earlier = settled.augments.slice(0, slot).filter((a): a is string => !!a);
    return earlier.length && settled.units.length && mode === "now" && !suggestedGoal ? picksCoverage(earlier, settled, data, stage, mode) : null;
  }, [settled, data, slot, stage, mode, suggestedGoal]);
  const earlierNames = board.augments
    .slice(0, slot)
    .filter((a): a is string => !!a)
    .map((a) => names.name(a));

  // After a pick, move on to the next stage still open: its list is the one the pick changes.
  const pick = (apiName: string) => {
    onPick(slot, apiName);
    const next = board.augments.findIndex((a, i) => i > slot && !a);
    setSlot(next >= 0 ? next : slot);
  };

  const inTier = ranked.filter((r) => tierShown === 0 || augmentTier(r) === tierShown);
  const scored = inTier.filter((r) => r.impact !== null);
  const unscored = inTier.filter((r) => r.impact === null);
  const tierCount = (t: number) => ranked.filter((r) => r.impact !== null && augmentTier(r) === t).length;
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
        shop. Each row shows the gain in average placement: the longer the bar, the bigger the gain ("−0.31" means 0.31 places better). Open a row for the arithmetic.
      </p>
      <div className="augment-impact-mode" role="radiogroup" aria-label="What the board is">
        <span className="muted">This board is</span>
        {(
          [
            ["goal", "My goal"],
            ["now", "What I have now"],
          ] as const
        ).map(([m, label]) => (
          <button key={m} type="button" role="radio" aria-checked={mode === m} className={mode === m ? "active" : undefined} onClick={() => setMode(m)}>
            {label}
          </button>
        ))}
      </div>
      <p className="muted augment-impact-given">
        {mode === "goal" && board.units.length
          ? `What an average game is short of on the way to this board, and what each augment fills: ${needsOf(settled, data).notes.join("; ")}. Econ is worth most at 2-1, items until the carries and tank are full, combat once they are.`
          : suggestedGoal
            ? `Scored towards the board below, from yours: ${needsOf(suggestedGoal, data, settled, stage).notes.join("; ")}.`
            : suggestions && suggestions.length === 0
              ? "No stored comp shares enough of these units yet: priced on the board as it is."
              : "Priced on the board as it is: its stars and items are already yours, so only what it still lacks counts."}
      </p>
      {mode === "now" && suggestions && suggestions.length > 0 && (
        <div className="augment-impact-goals">
          <span className="muted">Boards you could be building</span>
          <ul role="radiogroup" aria-label="Board you're building">
            {suggestions.map((sg, i) => {
              const carries = sg.comp.board.filter((u) => u.items.length > 0).map((u) => names.name(u.id));
              const goal = compBoard(sg, data);
              return (
                <li key={i}>
                  <button type="button" role="radio" aria-checked={goalChoice === i} className={goalChoice === i ? "active" : undefined} onClick={() => setGoalChoice(i)}>
                    <span className="goal-units">
                      {sg.comp.board.map((u) => (
                        <span key={u.id} className={sg.have.includes(u.id) ? "have" : undefined} title={`${names.name(u.id)} ${"★".repeat(u.star)}${sg.have.includes(u.id) ? " (on your board)" : ""}`}>
                          <GameIcon kind="champions" id={u.id} size={22} fallbackName={names.name(u.id)} />
                        </span>
                      ))}
                    </span>
                    <span className="goal-meta">
                      {carries.slice(0, 2).join(" + ") || names.name(sg.comp.board[0].id)} · you have {sg.have.length} of {sg.comp.board.length} · avg {sg.comp.avgPlacement.toFixed(2)} over {sg.comp.boards} games
                    </span>
                  </button>
                  <a className="muted goal-open" href={`?${encodeBoard({ ...goal, augments: board.augments }).toString()}`}>open as my goal</a>
                </li>
              );
            })}
          </ul>
        </div>
      )}
      <div className="mode-tabs" role="tablist" aria-label="Augment stage">
        {AUGMENT_SLOTS.map((s, i) => (
          <button key={s.stage} type="button" role="tab" aria-selected={slot === i} onClick={() => setSlot(i)}>
            {s.stage} <span className="muted">{board.augments[i] ? names.name(board.augments[i]!) : s.label.split(" ")[0]}</span>
          </button>
        ))}
      </div>

      <div className="augment-impact-tiers" role="radiogroup" aria-label="Augment tier">
        {[0, 1, 2, 3].map((t) => (
          <button key={t} type="button" role="radio" aria-checked={tierShown === t} className={tierShown === t ? "active" : undefined} onClick={() => setTierShown(t)}>
            {t === 0 ? "All tiers" : `${TIER[t]} (${tierCount(t)})`}
          </button>
        ))}
      </div>

      {earlierNames.length > 0 && board.units.length > 0 && (
        <p className="muted augment-impact-given">
          Scored on top of your {earlierNames.length > 1 ? "earlier picks" : "earlier pick"}: {earlierNames.join(", ")}.{" "}
          {moved && (moved.out.length > 0 ? `Out of the top 5 because of ${earlierNames.length > 1 ? "them" : "it"}: ${moved.out.join(", ")}; in: ${moved.in.join(", ")}. ` : "The top 5 is the same without it: it fills a different need than these. ")}
          {coverage && coverageText(coverage)}
        </p>
      )}
      {board.units.length === 0 ? (
        <p className="muted">Put units on the board to see how much each augment would add.</p>
      ) : (
        <>
          <ul className="augment-impact-list">
            {shown.map((r) => (
              <AugmentRow key={r.apiName} r={r} top={top} names={names} data={data} earlier={earlierNames} onPick={() => pick(r.apiName)} />
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

/** What the earlier picks already cover: the next augment is worth less only for what goes past what's still open. */
function coverageText(c: ReturnType<typeof picksCoverage>): string {
  const said: string[] = [];
  if (c.copiesCovered >= 0.5) {
    const left = Math.max(0, Math.round(c.copiesWanted - c.copiesCovered));
    said.push(`about ${Math.round(c.copiesCovered)} of the ~${Math.round(c.copiesWanted)} copies the board still wants (${left} left)`);
  }
  if (c.items >= 0.25) {
    const left = Math.max(0, c.itemSlots - c.items);
    said.push(`${+c.items.toFixed(1)} items' worth of the ${c.itemSlots} item slots that count most (${left > 0 ? `${+left.toFixed(1)} left` : "all filled"})`);
  }
  if (said.length === 0) return "Every augment adds less once earlier picks have lifted the board (it can only place so much higher).";
  return `It covers ${said.join(" and ")}. More of the same counts only for what's left, and every augment adds less once earlier picks have lifted the board (it can only place so much higher).`;
}

function AugmentRow({ r, top, names, data, earlier, onPick }: { r: AugmentScore; top: number; names: Names; data: SetData; earlier: string[]; onPick: () => void }) {
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
          <span className="augment-impact-value">
            {place(impact)}
            {r.alone !== undefined && (
              <small className="augment-impact-alone" title={`Without ${earlier.join(", ")}: ${place(r.alone)}`}>
                {Math.abs(r.alone - impact) < 0.005 ? "unchanged" : `was ${place(r.alone)}`}
              </small>
            )}
          </span>
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
