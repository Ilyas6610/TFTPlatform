// What a board is trying to do, for pricing the augments that give copies of
// units, rerolls or bench rerolls. A board of cheap units at 1-2 stars wants
// copies (a "reroll" board); a board of 4-5 cost carriers mostly doesn't. The
// price of a copy comes from the shop odds: how many rerolls it takes to see
// one specific unit.
//
// The shop odds and the 5 slots are Riot's standard TFT numbers, written here
// as assumptions: check them against the set (they've changed between sets
// before) and against pool depletion, which isn't modelled.

import { SetData } from "../api/client";
import { Board } from "./board";

/** % chance a shop slot shows a 1..5-cost champion, by player level. */
export const SHOP_ODDS: Record<number, number[]> = {
  1: [100, 0, 0, 0, 0],
  2: [100, 0, 0, 0, 0],
  3: [75, 25, 0, 0, 0],
  4: [55, 30, 15, 0, 0],
  5: [45, 33, 20, 2, 0],
  6: [30, 40, 25, 5, 0],
  7: [19, 30, 35, 15, 1],
  8: [16, 20, 35, 25, 4],
  9: [9, 15, 30, 30, 16],
  10: [5, 10, 20, 40, 25],
};

export const SHOP = {
  slots: 5,
  rerollGold: 2,
  /** A copy is never priced above this (the odds can be tiny: a 5-cost at level 6). */
  copyPriceCap: 60,
  /** Stars a unit of this cost is played to: 3 for the cheap ones, 2 for the rest. */
  goalStar: (cost: number) => (cost <= 3 ? 3 : 2),
  /** Share of the rounds left in which a reroll or bench slot would actually be used. */
  bareFloorRollIntent: 0.35,
  /** How much a copy of a filler unit (not a carry) is worth next to a carry's. */
  fillerWeight: 0.25,
  /** The most units counted as carries. */
  maxCarries: 3,
};

export interface Target {
  id: string;
  cost: number;
  star: number;
  /** Copies still needed to reach the unit's goal star. */
  remaining: number;
  /** 1 for a carry (it holds items), a fraction for filler: its copies matter less. */
  weight: number;
}

export interface Plan {
  level: number;
  /** Units of each cost in the set (a shop slot picks one of them). */
  unitsOfCost: Record<number, number>;
  /** Units on the board that still want copies. */
  targets: Target[];
  /** Share of the carries' value in 1-3 cost units: what kind of board this is. */
  lowCostShare: number;
  /** How much a free reroll is worth, as a share of its gold: high for a board that wants copies of cheap units. */
  rollIntent: number;
}

const copies = (star: number) => 3 ** (star - 1);

export function buildPlan(board: Board, data: SetData): Plan {
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  const unitsOfCost: Record<number, number> = {};
  for (const u of data.units) {
    if (u.name.includes("(")) continue; // a form of a unit counted once already (Lux (Coven)...)
    unitsOfCost[u.cost] = (unitsOfCost[u.cost] ?? 0) + 1;
  }
  // Carries: the units holding items; if the board holds few, its most valuable units.
  const value = (p: Board["units"][number]) => (defs.get(p.id)?.cost ?? 0) * copies(p.star);
  const held = [...board.units].filter((p) => p.items.length > 0 && defs.has(p.id)).sort((a, b) => b.items.length - a.items.length || value(b) - value(a));
  const carries = new Set(held.slice(0, SHOP.maxCarries));
  if (carries.size < 2) {
    for (const p of [...board.units].filter((x) => defs.has(x.id)).sort((a, b) => value(b) - value(a))) {
      if (carries.size >= SHOP.maxCarries) break;
      carries.add(p);
    }
  }
  const targets: Target[] = [];
  let low = 0;
  let total = 0;
  for (const p of board.units) {
    const d = defs.get(p.id);
    if (!d) continue;
    if (carries.has(p)) {
      total += value(p);
      if (d.cost <= 3) low += value(p);
    }
    const remaining = Math.max(0, copies(SHOP.goalStar(d.cost)) - copies(p.star));
    if (remaining > 0) targets.push({ id: p.id, cost: d.cost, star: p.star, remaining, weight: carries.has(p) ? 1 : SHOP.fillerWeight });
  }
  const lowCostShare = total > 0 ? low / total : 0;
  return {
    level: board.level,
    unitsOfCost,
    targets,
    lowCostShare,
    rollIntent: SHOP.bareFloorRollIntent + (1 - SHOP.bareFloorRollIntent) * lowCostShare,
  };
}

/** Gold to get one more copy of a specific unit of this cost by rerolling at this level: its price plus the rerolls it takes. */
export function copyPrice(cost: number, plan: Plan): number {
  const odds = (SHOP_ODDS[Math.max(1, Math.min(10, plan.level))] ?? SHOP_ODDS[10])[cost - 1] ?? 0;
  const perSlot = odds / 100 / Math.max(1, plan.unitsOfCost[cost] ?? 1);
  if (perSlot <= 0) return SHOP.copyPriceCap;
  const perReroll = 1 - (1 - perSlot) ** SHOP.slots;
  return Math.min(SHOP.copyPriceCap, cost + SHOP.rerollGold / perReroll);
}

/** Chance that a random unit of this cost is one of the board's targets. */
export function hitChance(cost: number, plan: Plan): number {
  const n = plan.targets.filter((t) => t.cost === cost).reduce((a, t) => a + t.weight, 0);
  return Math.min(1, n / Math.max(1, plan.unitsOfCost[cost] ?? 1));
}
