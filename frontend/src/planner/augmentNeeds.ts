// Augment choice as "what does this board still need": pick the type first, then the augment.
//
// Strong players choose an augment type from the board's state (TFTSense, BunnyMuffins, Broseph):
//   - econ when the board is poor for its next timing, best at 2-1 (interest compounds; nearly dead at stage 4);
//   - items until the carries and tank are itemized (most valuable early, when they win streaks);
//   - combat once the carry holds its items (a combat augment is a "fourth item"; weakest at 2-1 with ~5 units);
//   - copies and rerolls for reroll comps before their roll;
//   - direction (trait augments) only when the board plays the trait.
// This model turns that into numbers. An augment supplies amounts of each type (augmentScore's readers: gold, XP,
// rerolls, items after build fit, copies of wanted units, stat lift); the board needs each type by an amount that
// comes from an average game (no streak, no highroll) on the way to it; each type's value shrinks as its need is
// filled, so a run of picks spreads over econ, items and combat. Earlier picks fill needs first: an augment's value
// is what it adds on top of them.
//
// Every number is in NEEDS: unmeasured assumptions from how the game is usually played, to be checked and later fitted.

import { SetData } from "../api/client";
import { ASSUMPTIONS, AugmentEffects, AugmentScore, Confidence, EFFECTS, Part, augmentSupply, Supply } from "./augmentScore";
import { SHOP_ODDS } from "./augmentPlan";
import { Board } from "./board";

export type Archetype = "reroll1" | "reroll2" | "reroll3" | "fast8" | "fast9";

export const NEEDS = {
  /** Gold an average game earns in each stage (base 5 a round, interest, creep rounds, the carousel unit) and at 2-1. */
  income: { "2": 46, "3": 61, "4": 65, "5": 65 } as Record<string, number>,
  startGold: 14,
  roundsPerStage: 6,
  /** Gold held in the board's units at the spike. */
  boardGold: 20,
  /** Completed items' worth an average game has collected by each stage, and by the end. */
  itemsBy: { "2-1": 2, "3-2": 4, "4-2": 6, "5-1": 7, end: 8 } as Record<string, number>,
  /** XP from level L to L+1; 2 XP a round comes free. */
  xpToNext: { 4: 10, 5: 20, 6: 36, 7: 48, 8: 76, 9: 84 } as Record<number, number>,
  /** Copies of each target held when serious rolling starts. */
  held: { 1: 3, 2: 3, 3: 3, 4: 1, 5: 0 } as Record<number, number>,
  /** Roll gold budgeted to hit in most games rather than half of them. */
  hitMargin: 1.25,
  /** Gold taken at a stage is worth more the earlier it comes: interest compounds and it shapes the game. */
  goldRate: { "2-1": 1.6, "3-2": 1.25, "4-2": 0.9 } as Record<string, number>,
  /** Gold arriving over the rest of the game counts at this share of gold now. */
  laterGold: 0.6,
  /** Gold per item while it fills a shortfall (hitting the board), and spare gold (levels, interest, safety) beyond it. */
  shortGoldPerItem: 22,
  spareGoldPerItem: 45,
  /** Spare gold saturates over this many gold. */
  spareScale: 40,
  /** XP is worth this share of gold for each archetype (a reroll comp levels late on purpose). */
  xpShare: { reroll1: 0.3, reroll2: 0.5, reroll3: 0.7, fast8: 1, fast9: 1 } as Record<Archetype, number>,
  /** Free rerolls are worth this share of gold (a fast board only rolls at 8 or 9). */
  rerollShare: { reroll1: 1, reroll2: 1, reroll3: 1, fast8: 0.75, fast9: 0.6 } as Record<Archetype, number>,
  /** Items beyond what the board can use (carries' and tank's slots) count at this share, saturating over spareItemScale items. */
  spareItemShare: 0.3,
  spareItemScale: 3,
  /** Every board wants at least this many items (two carry sets and tank items), even before its carries are on it. */
  minItemDemand: 10,
  /** Combat readiness also grows with how itemized the carries are: × (combatItemBase + (1 − combatItemBase) × itemized share). */
  combatItemBase: 0.5,
  /** Items early win streaks and save HP. */
  itemTempo: { "2-1": 1.15, "3-2": 1.1, "4-2": 1 } as Record<string, number>,
  /** Combat bonuses: how ready the board is to use them at the stage (units fielded, carries itemized). */
  combatReady: { "2-1": 0.45, "3-2": 0.75, "4-2": 1 } as Record<string, number>,
  /** A trait augment whose trait the board plays: its unread trait effect, in items (worth most early, when it shapes the comp). */
  directionPrior: { "2-1": 1, "3-2": 0.9, "4-2": 0.7 } as Record<string, number>,
  /** Item units to average placement, for display (an item is ~0.1 placement after calibration). */
  placementPerItem: 0.1,
};

const STAGES = ["2-1", "3-2", "4-2"];
const idx = (st: string) => {
  const [a, b] = st.split("-").map(Number);
  return (a - 2) * NEEDS.roundsPerStage + Math.min(b, NEEDS.roundsPerStage) - 1;
};
const incomeUntil = (st: string) => {
  const [a, b] = st.split("-").map(Number);
  let g = NEEDS.startGold;
  for (let s = 2; s < a; s++) g += NEEDS.income[String(s)] ?? 65;
  return g + ((NEEDS.income[String(a)] ?? 65) * (Math.min(b, NEEDS.roundsPerStage) - 1)) / NEEDS.roundsPerStage;
};

interface PlanSpec {
  levels: [string, number][];
  spike: [string, number];
  label: string;
}
const PLANS: Record<Archetype, PlanSpec> = {
  reroll1: { label: "1-cost reroll", levels: [["2-5", 5]], spike: ["3-5", 5] },
  reroll2: { label: "2-cost reroll", levels: [["2-5", 5], ["3-2", 6]], spike: ["4-1", 6] },
  reroll3: { label: "3-cost reroll", levels: [["2-5", 5], ["3-2", 6], ["3-5", 7]], spike: ["5-1", 7] },
  fast8: { label: "Fast 8", levels: [["2-5", 5], ["3-2", 6], ["4-1", 7], ["4-2", 8]], spike: ["4-2", 8] },
  fast9: { label: "Fast 9", levels: [["2-5", 5], ["3-2", 6], ["4-1", 7], ["4-2", 8], ["5-1", 9]], spike: ["5-1", 9] },
};

/** What the goal board needs from an average game, before any augment. */
export interface Needs {
  archetype: Archetype;
  label: string;
  spike: string;
  /** Gold short of the plan by the spike (levels, hitting the copies, the board), and gold per wanted copy. */
  shortfall: number;
  perCopy: number;
  /** Items the board can use (a set per carry-capable unit up to 3, one more each beyond, 2 for tanks) and the room left after an average game's items. */
  itemDemand: number;
  itemRoom: number;
  /** Items the game will have by the end before augments. */
  itemsEnd: number;
  notes: string[];
}

function unitInfo(goal: Board, data: SetData) {
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  return goal.units.filter((u) => defs.has(u.id)).map((u) => ({ ...u, cost: defs.get(u.id)!.cost }));
}

export function archetypeOf(goal: Board, data: SetData): Archetype {
  const us = unitInfo(goal, data);
  const three = us.filter((u) => u.star === 3 && u.cost <= 3);
  if (three.length) {
    const byCost = [1, 2, 3].map((c) => three.filter((u) => u.cost === c).length);
    const cost = byCost.indexOf(Math.max(...byCost)) + 1;
    return (["reroll1", "reroll2", "reroll3"] as const)[cost - 1];
  }
  const carries = us.filter((u) => u.items.length >= 2);
  return carries.some((u) => u.cost === 5) || goal.level >= 9 ? "fast9" : "fast8";
}

// Per goal, per current board (or none), per stage.
const needsCache = new WeakMap<Board, { avg?: Needs; byNow: WeakMap<Board, Map<string, Needs>> }>();
/**
 * `now` (with its `stage`): the player's board when known. Copies of goal units they already hold and items already on
 * their units count as progress; without it an average game's holdings at the roll are assumed.
 */
export function needsOf(goal: Board, data: SetData, now?: Board, stage?: string): Needs {
  let entry = needsCache.get(goal);
  if (!entry) needsCache.set(goal, (entry = { byNow: new WeakMap() }));
  let perStage: Map<string, Needs> | undefined;
  if (now) {
    perStage = entry.byNow.get(now);
    if (!perStage) entry.byNow.set(now, (perStage = new Map()));
    const hit = perStage.get(stage ?? "");
    if (hit) return hit;
  } else if (entry.avg) return entry.avg;
  const arche = archetypeOf(goal, data);
  const plan = PLANS[arche];
  const [spike, spikeLevel] = plan.spike;
  const us = unitInfo(goal, data);
  const unitsOfCost: Record<number, number> = {};
  for (const name of new Set(data.units.map((u) => `${u.cost}|${u.name.replace(/\s*\(.*\)$/, "")}`))) {
    const c = Number(name.split("|")[0]);
    unitsOfCost[c] = (unitsOfCost[c] ?? 0) + 1;
  }
  // Copies to hit by the spike: the reroll's 3-stars, or a fast board's 2-star 4/5-costs.
  const rerollCost = arche === "reroll1" ? 1 : arche === "reroll2" ? 2 : arche === "reroll3" ? 3 : 0;
  // Copies held at the roll: the player's (3 per star step) when ahead of an average game, else an average game's.
  const heldOf = (id: string, cost: number) => {
    if (!now) return NEEDS.held[cost];
    const mine = now.units.filter((u) => u.id === id).reduce((a, u) => a + 3 ** (u.star - 1), 0);
    // By the roll an average game has picked up NEEDS.held copies anyway; a board already ahead keeps its lead.
    return Math.max(mine, NEEDS.held[cost]);
  };
  const targets = (rerollCost
    ? us.filter((u) => u.star === 3 && u.cost <= rerollCost).map((u) => ({ cost: u.cost, need: 9 - heldOf(u.id, u.cost) }))
    : us.filter((u) => u.star >= 2 && u.cost >= 4).map((u) => ({ cost: u.cost, need: 3 - heldOf(u.id, u.cost) }))
  ).filter((t) => t.need > 0);
  const odds = SHOP_ODDS[Math.min(10, Math.max(3, spikeLevel))];
  const per = targets.map((t) => (5 * (odds[t.cost - 1] ?? 0)) / 100 / Math.max(1, unitsOfCost[t.cost] ?? 1));
  let rolls = 0;
  const got = targets.map(() => 0);
  while (targets.some((t, i) => got[i] < t.need - 1e-9) && rolls < 500 && per.every((p) => p > 0)) {
    rolls++;
    targets.forEach((t, i) => got[i] < t.need && (got[i] += per[i]));
  }
  const copies = targets.reduce((a, t) => a + t.need, 0);
  const rollGold = rolls * 2 + targets.reduce((a, t) => a + t.cost * t.need, 0);
  let lvl = 4;
  let xp = 0;
  for (const [, target] of plan.levels) {
    for (let l = lvl; l < target; l++) xp += NEEDS.xpToNext[l] ?? 0;
    lvl = Math.max(lvl, target);
  }
  const levelGold = Math.max(0, xp - 2 * idx(spike));
  const earned = incomeUntil(spike);
  const shortfall = Math.max(0, rollGold * NEEDS.hitMargin + levelGold + NEEDS.boardGold - earned);
  const capable = us.filter((u) => u.items.length >= 2 || u.star === 3 || (u.cost >= 4 && u.star >= 2)).length;
  const itemDemand = Math.max(NEEDS.minItemDemand, 3 * Math.min(capable, 3) + Math.max(0, capable - 3) + 2);
  // Items a game will have by the end: an average game's, or what the player holds now plus what's still to come.
  const holdable = new Set(data.items.filter((i) => ["completed", "emblem", "artifact", "radiant"].includes(i.kind)).map((i) => i.apiName));
  const heldItems = now ? now.units.reduce((a, u) => a + u.items.filter((i) => holdable.has(i)).length, 0) : 0;
  const itemsEnd = now ? Math.max(NEEDS.itemsBy.end, heldItems + NEEDS.itemsBy.end - (NEEDS.itemsBy[stage ?? "2-1"] ?? 0)) : NEEDS.itemsBy.end;
  const n: Needs = {
    archetype: arche,
    label: plan.label,
    spike,
    shortfall,
    perCopy: copies ? rollGold / copies : 0,
    itemDemand,
    itemRoom: Math.max(0, itemDemand - itemsEnd),
    itemsEnd,
    notes: [
      `${plan.label}: spike at ${spike} on level ${spikeLevel}`,
      ...(now ? [`from your board at ${stage}: ${heldItems} item${heldItems === 1 ? "" : "s"} held`] : []),
      `${Math.round(earned)} gold earned by then − ${Math.round(levelGold)} for levels − ${NEEDS.boardGold} in the board, against ~${Math.round(rollGold * NEEDS.hitMargin)} to hit ${copies} copies: ${shortfall > 0 ? `${Math.round(shortfall)} short` : "covered"}`,
      `${itemDemand} items wanted (${capable} units can carry items), ${now ? `you'll have ~${Math.round(itemsEnd)}` : `an average game makes ~${NEEDS.itemsBy.end}`}`,
    ],
  };
  if (perStage) perStage.set(stage ?? "", n);
  else entry.avg = n;
  return n;
}

/** Totals of what a set of picks supplies, each at the stage it was picked (gold weighted by its stage). */
interface Taken {
  gold: number; // gold-equivalent, already weighted by stage
  items: number;
  combat: number; // items' worth of combat, before readiness
  direction: number;
}

function supplyOf(fx: AugmentEffects, goal: Board, data: SetData, stage: string, needs: Needs, now?: Board): { s: Supply; t: Taken } {
  const s = augmentSupply(fx, goal, data, stage, now);
  const arche = needs.archetype;
  const rate = NEEDS.goldRate[stage] ?? 1;
  const copiesGold = needs.perCopy ? s.copies * needs.perCopy : 0;
  const gold = rate * (s.goldNow + NEEDS.laterGold * s.goldLater + NEEDS.xpShare[arche] * s.xp + NEEDS.rerollShare[arche] * s.rerollGold) + copiesGold * (stage === "4-2" && arche !== "reroll3" && arche !== "fast9" ? 0.3 : 1);
  const combat = s.liftPerItem > 0 ? (s.statLift / s.liftPerItem) * (NEEDS.combatReady[stage] ?? 1) : 0;
  const gate = s.trait === "unplayed" ? ASSUMPTIONS.offTraitShare : 1;
  return {
    s,
    t: {
      gold: gold * gate,
      items: s.items * (NEEDS.itemTempo[stage] ?? 1) * gate,
      combat: combat * gate,
      direction: s.trait === "played" ? (NEEDS.directionPrior[stage] ?? 0.7) : 0,
    },
  };
}

/** The value (in items) of everything taken, against the board's needs. */
function valueOf(t: Taken, needs: Needs) {
  // Gold fills the shortfall at the short rate, then saturates as spare gold.
  const short = Math.min(t.gold, needs.shortfall);
  const spare = Math.max(0, t.gold - needs.shortfall);
  const gold = short / NEEDS.shortGoldPerItem + (NEEDS.spareScale * (1 - Math.exp(-spare / NEEDS.spareScale))) / NEEDS.spareGoldPerItem;
  // Items fill the room on the carries and tank, then count as spares (saturating).
  const extra = Math.max(0, t.items - needs.itemRoom);
  const items = Math.min(t.items, needs.itemRoom) + NEEDS.spareItemShare * NEEDS.spareItemScale * (1 - Math.exp(-extra / NEEDS.spareItemScale));
  // Combat: worth more the more itemized the carries are (items taken count), with diminishing returns between bonuses.
  const itemized = Math.min(1, (needs.itemsEnd + t.items) / needs.itemDemand);
  const combat = 2 * (1 - Math.exp(-t.combat / 2)) * (NEEDS.combatItemBase + (1 - NEEDS.combatItemBase) * itemized);
  return { gold, items, combat, direction: t.direction };
}

const add = (a: Taken, b: Taken): Taken => ({ gold: a.gold + b.gold, items: a.items + b.items, combat: a.combat + b.combat, direction: a.direction + b.direction });
const ZERO: Taken = { gold: 0, items: 0, combat: 0, direction: 0 };

/**
 * Scores one augment by the needs it fills, picked at `stage` on top of the augments picked at earlier stages (`given`,
 * in slot order: the first at 2-1, the second at 3-2).
 */
export function scoreByNeeds(
  aug: { apiName: string; name: string; tier: number },
  goal: Board,
  data: SetData,
  stage: string,
  given: string[] = [],
  effects: Record<string, AugmentEffects> = EFFECTS,
  now?: Board,
): AugmentScore {
  const base: AugmentScore = { apiName: aug.apiName, name: aug.name, tier: aug.tier, dp: null, impact: null, statLift: 0, confidence: "none", parts: [], caveats: [] };
  const fx = effects[aug.apiName];
  if (!fx || fx.confidence === "none" || goal.units.length === 0) {
    base.caveats.push(goal.units.length === 0 ? "put units on the board first" : "this augment's effect isn't modelled");
    return base;
  }
  const needs = needsOf(goal, data, now, now ? stage : undefined);
  let before = ZERO;
  given.forEach((id, i) => {
    const g = effects[id];
    if (g && g.confidence !== "none" && id !== aug.apiName) before = add(before, supplyOf(g, goal, data, STAGES[i] ?? stage, needs, now).t);
  });
  const { s, t } = supplyOf(fx, goal, data, stage, needs, now);
  const v0 = valueOf(before, needs);
  const v1 = valueOf(add(before, t), needs);
  const P = NEEDS.placementPerItem;
  const parts: Part[] = [];
  const caveats: string[] = [];
  const gold = v1.gold - v0.gold;
  const items = v1.items - v0.items;
  const combat = v1.combat - v0.combat;
  const direction = v1.direction - v0.direction;
  const fmt = (x: number) => (Math.abs(x - Math.round(x)) < 0.05 ? String(Math.round(x)) : x.toFixed(1));
  if (Math.abs(gold) > 1e-6) {
    const what = [s.goldNow && `${fmt(s.goldNow)} gold`, s.goldLater && `${fmt(s.goldLater)} gold over the game`, s.xp && `${fmt(s.xp)} XP`, s.rerollGold && `rerolls worth ${fmt(s.rerollGold)} gold`, s.copies && `${fmt(s.copies)} wanted copies`].filter(Boolean).join(", ");
    const left = Math.max(0, needs.shortfall - before.gold);
    parts.push({ label: `Econ / copies: ${what}`, dp: gold * P, note: left > 0 ? `the board is ~${Math.round(left)} gold short of its ${needs.spike} spike; gold at ${stage} counts ×${NEEDS.goldRate[stage]}` : "the spike is already covered: spare gold" });
  }
  if (Math.abs(items) > 1e-6) {
    const room = Math.max(0, needs.itemRoom - before.items);
    parts.push({ label: `Items: ${fmt(s.items)} items' worth`, dp: items * P, note: room > 0 ? `${fmt(room)} more items fit the carries and tank beyond an average game's ${NEEDS.itemsBy.end}` : "the carries and tank are covered: spare items" });
  }
  if (Math.abs(combat) > 1e-6) {
    parts.push({ label: `Combat: ${(s.statLift * 100).toFixed(1)}% team strength`, dp: combat * P, note: `≈ ${fmt(s.statLift / Math.max(1e-9, s.liftPerItem))} items on the final board, ×${NEEDS.combatReady[stage]} at ${stage} (units fielded, carries itemized)` });
  }
  if (direction > 0) parts.push({ label: "Direction: trait the board plays", dp: direction * P, note: "its trait effect isn't read; counted as an item" });
  if (s.trait === "unplayed") caveats.push(`built around ${fx.traits!.join("/")}, which this board doesn't play: counted at ${ASSUMPTIONS.offTraitShare * 100}%`);
  if (fx.unparsed?.length) caveats.push(...fx.unparsed.map((u) => `not counted: ${u}`));
  const dp = parts.reduce((a, p) => a + p.dp, 0);
  const alone = given.length ? (() => {
    const va = valueOf(t, needs);
    return (va.gold + va.items + va.combat + va.direction) * P;
  })() : undefined;
  if (alone !== undefined && Math.abs(alone) > 1e-9 && Math.abs(dp / alone - 1) >= 0.05) caveats.unshift(`with your earlier picks: worth ${Math.round((dp / alone) * 100)}% of what it adds alone`);
  const confidence: Confidence = fx.confidence;
  return { ...base, dp, impact: dp, statLift: s.statLift, confidence, parts, caveats, ...(alone !== undefined ? { alone } : {}) };
}

export function rankByNeeds(augments: { apiName: string; name: string; tier: number }[], goal: Board, data: SetData, stage: string, given: string[] = [], effects: Record<string, AugmentEffects> = EFFECTS, now?: Board): AugmentScore[] {
  return augments
    .map((a) => scoreByNeeds(a, goal, data, stage, given, effects, now))
    .sort((a, b) => (b.impact ?? -Infinity) - (a.impact ?? -Infinity) || a.name.localeCompare(b.name));
}
