// What things are worth in placement, from the board value model fitted on
// stored boards (scripts/fit-board-value.mjs, boardValue.data.json): within a
// lobby, how much a board's placement shifts with the stars of its units (by
// cost), the items it holds and its level. Correlational, so it is used as an
// exchange rate between items, star-ups and stat bonuses, not as a promise
// about any one board.

import { SetData } from "../api/client";
import raw from "./boardValue.data.json";
import { Board } from "./board";

const C = raw.coefficients as Record<string, number>;

export const BOARD_VALUE_FIT = raw;

/** Placement gained by an average item (all holdable kinds share it: the fit found no real difference between them). */
export const ITEM_GAIN = raw.itemGainMean;

/** How much of a holder class's own item value is kept (the rest is the average). */
export const ITEM_SHRINK = { v: 0.5 };

/**
 * Placement gained by one more item on a unit of this cost and star: the fit's
 * value for that holder class, pulled halfway to the average (the classes are
 * noisy where boards are rare, e.g. 5-cost 3-stars) and kept in a sane range.
 */
export function itemGain(cost: number, star: number): number {
  const own = -(C[`items_cost${cost}star${Math.min(3, Math.max(1, star))}`] ?? -ITEM_GAIN);
  return Math.min(0.9, Math.max(0.06, ITEM_SHRINK.v * own + (1 - ITEM_SHRINK.v) * ITEM_GAIN));
}

/** Placement gained when a unit of this cost goes from `star` to the next (never below a small floor: the fit is noisy for rare combinations). */
export function starStepGain(cost: number, star: number): number {
  const from = C[`cost${cost}star${star}`];
  const to = C[`cost${cost}star${star + 1}`];
  if (from === undefined || to === undefined) return 0;
  return Math.max(0.02, from - to);
}

/** Copies a unit still needs to reach its next star, counting the ones it already is. */
export const copiesToNextStar = (star: number) => (star === 1 ? 2 : star === 2 ? 6 : 0);

/** Placement gained by one more copy of a unit, as progress towards its next star. */
export function copyGain(cost: number, star: number): number {
  const need = copiesToNextStar(star);
  return need > 0 ? starStepGain(cost, star) / need : 0;
}

/** The model's predicted placement shift for a board (lower is better); only differences between boards mean anything. */
export function predictedShift(board: Board, data: SetData): number {
  const cost = new Map(data.units.map((u) => [u.apiName, u.cost]));
  let s = board.level * C.level;
  for (const u of board.units) {
    const c = cost.get(u.id);
    if (!c) continue;
    s += C[`cost${c}star${Math.min(3, Math.max(1, u.star))}`] ?? 0;
    s += u.items.length * C[`items_cost${c}star${Math.min(3, Math.max(1, u.star))}`];
  }
  return s;
}
