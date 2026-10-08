// Augment impact estimate (prototype, v2).
//
// Question: given the board in the planner, how much would each augment add?
// Neither Set 16, 17 nor 18's match data records augments, so nothing here is
// learned from augment results. What is learned from data is the exchange rate
// between the things augments give: items, star-ups and stat bonuses are priced
// in placement with the board value model fitted on stored boards
// (boardValue.ts), and everything else is converted to those through the shop.
// Every other assumption is in ASSUMPTIONS, and each score carries a breakdown.
//
// How it works:
//  1. Items: a held item is worth about 0.24 placement (the fit), less when the
//     carries' item slots are full.
//  2. Copies: a copy of a unit that still wants stars is worth its share of the
//     star-up's placement gain (the fit, by cost); the shop odds say what a copy
//     costs to roll (augmentPlan.ts), so gold, rerolls, XP, duplicators, random
//     champions and Pandora's Bench are all valued by the copies they buy.
//  3. Stat bonuses: team strength (the geometric mean of total effective HP and
//     total DPS) is recomputed with the bonus applied to the units it covers; the
//     lift is converted to placement through what one extra item does to the same
//     strength, so the scale is anchored to the fit, not guessed.
//  4. Built around a trait the board doesn't play: worth little.
//
// Effects come from augmentEffects.data.json, extracted from the augments'
// descriptions by scripts/extract-augment-effects.mjs. An augment it couldn't
// read is "not scored", and one it only partly read has lower confidence.

import { SetAugment, SetData, SetUnit } from "../api/client";
import raw from "./augmentEffects.data.json";
import { Plan, SHOP, buildPlan, copyPrice, hitChance } from "./augmentPlan";
import { Board, COLS, PlacedUnit, ROWS, computeTraits } from "./board";
import { ITEM_GAIN, copyGain, itemGain } from "./boardValue";

export type Confidence = "high" | "medium" | "low" | "none";

export interface StatEffect {
  kind: "stat";
  scope: "team" | "holders" | "unheld" | "front" | "back" | "cost" | "trait" | "unknown";
  /** For scope "trait": the units that have it. */
  trait?: string;
  stat: "health" | "armor" | "mr" | "ad" | "ap" | "as" | "crit" | "critDmg" | "damageAmp" | "durability" | "omnivamp";
  flat?: number;
  /** The flat bonus grows by this much per player level. */
  plusPerLevel?: number;
  pct?: number;
  /** The bonus repeats per ... (see perCount). */
  per?: "trait" | "activeTrait" | "bronzeTrait" | "frontRowUnit" | "level" | "item" | "oneUnitItem" | "emblem" | "uniqueOther";
  rows?: number;
  cost?: number;
  delay?: number;
  everySeconds?: number;
  ramp?: boolean;
  forSeconds?: number;
}

export interface Resources {
  gold?: number;
  xp?: number;
  rerolls?: number;
  components?: number;
  completed?: number;
  artifacts?: number;
  emblems?: number;
  radiants?: number;
  units?: { n: number; cost: number; star: number; fixed?: boolean; name?: string }[];
  named?: { name: string; n: number; kind: string }[];
  /** Champion Duplicators: each copies a unit of up to maxCost. */
  duplicators?: { n: number; maxCost: number }[];
  /** The rightmost bench slots turn into random champions of the same cost every round. */
  benchTransform?: { slots: number };
  /** A copy of every champion of this cost. */
  allOfCost?: number;
}

export interface Recurring {
  gold?: { perRound?: number; perStage?: number };
  xp?: { perStage?: number; rounds?: number };
  rerolls?: { perRound?: number; perStage?: number };
  components?: { rounds?: number };
  /** Another copy of the champion(s) gained, every round. */
  copies?: { perRound?: number; cost?: number; n?: number };
}

export interface AugmentEffects {
  effects?: StatEffect[];
  resources?: Resources;
  recurring?: Recurring;
  confidence: Confidence;
  /** Traits the augment is built around (from the set data): worth little to a board that doesn't play one. */
  traits?: string[];
  unparsed?: string[];
  /** Sentences describing what a gained item does: read, but not counted as a further reward. */
  notes?: string[];
  /** Rewards that depend on something happening; counted at half. */
  conditional?: string[];
}

export const EFFECTS = raw.augments as unknown as Record<string, AugmentEffects>;

/** Every number the estimate rests on besides the fitted board value model. Change one and the ranking moves. */
export const ASSUMPTIONS = {
  /** TFT star scaling for health and attack damage. */
  starMultiplier: [1, 1.8, 3.24],
  /**
   * Each held item adds this much to health, to attack damage and to ability power (a generic completed item: they are
   * big, a third of a carry's base). Checked against stored boards: a higher item health predicts placements within
   * a lobby slightly better than the first guess of 12%.
   */
  itemHealth: 0.35,
  itemDamage: 0.3,
  itemAbility: 0.3,
  /** Share of a unit's damage that comes from its ability (the rest is auto-attacks). */
  abilityShare: 0.5,
  /** How much of attack speed's gain reaches ability damage (faster mana generation). */
  abilityFromAttackSpeed: 0.5,
  /** Length of a fight that bonuses are averaged over. */
  fightSeconds: 30,
  /** Healing from omnivamp, as a share of damage dealt that counts as extra health. */
  omnivampValue: 0.8,
  /** What a trait augment is worth to a board that doesn't play its trait (its grants still sell, its bonuses do nothing). */
  offTraitShare: 0.1,
  /** Units of a trait a board needs for the trait to count as played. */
  playedTraitUnits: 2,
  /** What an unclear "who gets this" bonus is assumed to cover. */
  unknownScopeCoverage: 0.5,
  /** Items on the one champion an augment grants, when it scales per item on that champion. */
  itemsOnOneUnit: 2,
  /** Allies sharing a trait with a unit, when an augment scales per such ally (average guess). */
  sharedTraitAllies: 2,
  /** How many items each kind counts as, next to a completed item. */
  itemEquivalents: { component: 0.5, completed: 1, artifact: 1.2, emblem: 0.8, radiant: 1.4 } as Record<string, number>,
  /** Items a unit can hold; how many free slots (best holders first) further items can fill before they count at a discount. */
  itemsPerCarry: 3,
  itemSlotsConsidered: 6,
  /** Items always have some use when the carries are full (spares for others, swaps, tempo): usable slots never fall below this, at a share of an average item. */
  itemSlotFloor: 3,
  spareSlotShare: 0.7,
  /** Share of an item beyond the free slots that still counts. */
  itemOverflowShare: 0.3,
  /** Placement per gold that nothing on the board needs to buy (interest, safety). */
  idleGold: 0.003,
  /**
   * Gold, copies and items picked early do more than the final board shows: they arrive before the fights they decide,
   * save rolling later and compound. Their value grows by this per stage left (the fit sees only final boards, so it
   * can't measure this: it is an assumption, and the one that decides how economy compares with stats).
   */
  tempoPerStage: 0.1,
  /** Stages left after the stage an augment is picked at, and rounds per stage, for recurring gains. */
  stagesLeft: { "2-1": 5, "3-2": 4, "4-2": 3 } as Record<string, number>,
  roundsPerStage: 6,
  /** XP matters less the higher the level (level 9 and 10 have little left to buy). */
  xpUsefulByLevel: { 6: 1, 7: 1, 8: 0.6, 9: 0.2, 10: 0 } as Record<number, number>,
  /**
   * A board of cheap carries (a reroll board) lives on hitting 3 stars early: its copies decide fights the final board
   * can't show (star-ups at level 5-6 beat the lobby), so their value is multiplied by 1 + this x the carries' low-cost
   * share. Domain knowledge, not measured: the fit sees only final boards.
   */
  rerollTempo: 6,
  /** No augment moves a placement by more than about this: totals are squeezed towards it (tanh), keeping their order. */
  softCap: 0.8,
  /** Share of the rounds left in which the re-rolled bench slots hold spare units of the cost you want. */
  benchUseShare: 0.4,
  /**
   * Scale from the fitted placement exchange rate to what one augment can really move: the fit is correlational (strong
   * players hold more of everything), so it overstates what one extra item or star causes. Set so the best augments
   * come out around half a placement and a typical one around a tenth, as augment stats do in games that record them.
   */
  calibration: 0.35,
};

export interface Part {
  label: string;
  /** Placement this part is expected to gain (positive is better). */
  dp: number;
  /** Copies of units: worth more on a board of cheap carries (see rerollTempo). */
  copies?: boolean;
  /** For stat parts: the lift in team strength, as a fraction. */
  lift?: number;
  note?: string;
}

export interface AugmentScore {
  apiName: string;
  name: string;
  tier: number;
  /** Placement the augment is expected to gain on this board; null when it couldn't be read. */
  dp: number | null;
  /** Same as dp (the ranking key, kept under its old name for the UI). */
  impact: number | null;
  /** Team strength lift from the stat effects alone. */
  statLift: number;
  confidence: Confidence;
  parts: Part[];
  /** Plain reasons this is a rough number (assumed coverage, partly read text...). */
  caveats: string[];
}

// ---------------------------------------------------------------- power model

interface UnitState {
  hp: number;
  armor: number;
  mr: number;
  ad: number;
  attackSpeed: number;
  /** Ability damage before ability power and attack speed bonuses; fixed by the unit's base stats, so attack damage doesn't scale it. */
  abilityBase: number;
  crit: number;
  critMult: number;
  apBonus: number; // ability power as a fraction added to ability damage
  damageAmp: number;
  durability: number;
  omnivamp: number;
  items: number;
  row: number;
  traits: string[];
  cost: number;
}

function unitState(u: PlacedUnit, def: SetUnit): UnitState {
  const A = ASSUMPTIONS;
  const star = A.starMultiplier[u.star - 1] ?? 1;
  const items = u.items.length;
  const s = def.stats;
  const baseAD = (s.damage ?? 50) * star;
  const attackSpeed = s.attackSpeed ?? 0.7;
  const crit = s.critChance ?? 0.25;
  const critMult = s.critMultiplier ?? 1.4;
  const baseAuto = baseAD * attackSpeed * (1 + Math.min(1, crit) * (critMult - 1));
  return {
    hp: (s.hp ?? 600) * star * (1 + A.itemHealth * items),
    armor: s.armor ?? 30,
    mr: s.magicResist ?? 30,
    ad: baseAD * (1 + A.itemDamage * items),
    attackSpeed,
    abilityBase: (baseAuto * A.abilityShare) / (1 - A.abilityShare),
    crit,
    critMult,
    apBonus: A.itemAbility * items,
    damageAmp: 0,
    durability: 0,
    omnivamp: 0,
    items,
    row: Math.floor(u.pos / COLS),
    traits: def.traits,
    cost: def.cost,
  };
}

function ehp(s: UnitState): number {
  const resist = (s.armor + s.mr) / 2;
  const base = (s.hp * (100 + resist)) / 100 / Math.max(0.2, 1 - s.durability);
  return base * (1 + s.omnivamp * ASSUMPTIONS.omnivampValue);
}

function dps(s: UnitState): number {
  const A = ASSUMPTIONS;
  const crit = Math.min(1, s.crit) * (s.critMult - 1);
  const auto = s.ad * s.attackSpeed * (1 + crit);
  // Ability damage: its share of the unit's base damage, scaled by ability power and (partly) attack speed.
  const asGain = s.attackSpeed / 0.7 - 1;
  const ability = s.abilityBase * (1 + s.apBonus) * (1 + A.abilityFromAttackSpeed * Math.max(0, asGain));
  return (auto + ability) * (1 + s.damageAmp);
}

/**
 * Team strength: the geometric mean of total effective HP and total DPS (the
 * square root of their product, as in Lanchester's square law), so +10% of
 * both makes a team 10% stronger, not 21%.
 */
export function teamPower(states: UnitState[]): number {
  if (states.length === 0) return 0;
  const hp = states.reduce((a, s) => a + ehp(s), 0);
  const dmg = states.reduce((a, s) => a + dps(s), 0);
  return Math.sqrt(hp * dmg);
}

// -------------------------------------------------------------- stat effects

/** How many times a "per ..." bonus applies to a unit. */
function perCount(e: StatEffect, states: UnitState[], board: Board, data: SetData): number {
  switch (e.per) {
    case undefined:
      return 1;
    case "trait":
      return Math.min(states.length - 1, ASSUMPTIONS.sharedTraitAllies);
    case "activeTrait":
      return computeTraits(board, data).filter((t) => t.tier >= 0 && (t.trait?.breakpoints.length ?? 0) > 1).length;
    case "bronzeTrait":
      return computeTraits(board, data).filter((t) => t.tier >= 0 && t.style === 1).length;
    case "frontRowUnit":
      return states.filter((x) => x.row === 0).length;
    case "level":
      return board.level;
    case "item":
      return states.reduce((a, x) => a + x.items, 0);
    case "oneUnitItem":
      return ASSUMPTIONS.itemsOnOneUnit;
    case "emblem":
      return 1;
    case "uniqueOther":
      return 3;
  }
}

function appliesTo(e: StatEffect, s: UnitState): boolean {
  switch (e.scope) {
    case "team":
    case "unknown":
      return true;
    case "holders":
      return s.items > 0;
    case "unheld":
      return s.items === 0;
    case "front":
      return s.row === 0;
    case "back":
      return s.row >= ROWS - (e.rows ?? 1);
    case "cost":
      return s.cost === e.cost;
    case "trait":
      return e.trait !== undefined && s.traits.includes(e.trait);
  }
}

/** The share of a fight a timed bonus covers, as a multiplier on its size. */
function timing(e: StatEffect): number {
  const T = ASSUMPTIONS.fightSeconds;
  const start = e.delay ?? 0;
  if (e.everySeconds) {
    // One more stack every N seconds once it starts: average stacks over the fight.
    const stacksAtEnd = Math.max(0, T - start) / e.everySeconds;
    return Math.min(3, (stacksAtEnd * Math.max(0, T - start)) / (2 * T));
  }
  if (e.ramp) {
    // One more stack every second (up to forSeconds): average stacks over the fight.
    const cap = e.forSeconds ?? T;
    const run = Math.max(0, T - start);
    const rise = Math.min(cap, run);
    return (rise * rise) / 2 / T + (rise * (run - rise)) / T;
  }
  if (e.delay) return Math.max(0, (T - e.delay) / T);
  if (e.forSeconds) return Math.min(e.forSeconds, T) / T;
  return 1;
}

function applyStat(s: UnitState, e: StatEffect, n: number, t: number, level: number): void {
  const k = n * t;
  const flat = ((e.flat ?? 0) + (e.plusPerLevel ?? 0) * level) * k;
  const pct = (e.pct ?? 0) * k;
  switch (e.stat) {
    case "health":
      s.hp += flat + s.hp * pct;
      break;
    case "armor":
      s.armor += flat;
      break;
    case "mr":
      s.mr += flat;
      break;
    case "ad":
      s.ad *= 1 + pct;
      break;
    case "ap":
      s.apBonus += pct;
      break;
    case "as":
      s.attackSpeed *= 1 + pct;
      break;
    case "crit":
      s.crit += pct;
      break;
    case "critDmg":
      s.critMult += pct;
      break;
    case "damageAmp":
      s.damageAmp += pct;
      break;
    case "durability":
      s.durability += pct;
      break;
    case "omnivamp":
      s.omnivamp += pct;
      break;
  }
}

// ---------------------------------------------------------------- resources

export const STAGE_LABELS = ["2-1", "3-2", "4-2"];

function stageFactors(stage: string) {
  const A = ASSUMPTIONS;
  const stages = A.stagesLeft[stage] ?? 4;
  return { stages, rounds: stages * A.roundsPerStage, tempo: 1 + A.tempoPerStage * stages };
}

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));

/**
 * Placement gained by n more items: each goes to the free slot where it is worth most
 * (a 3-star carry before a 1-star filler: the fit prices items by holder), a few spare
 * slots always count at a discount (swaps, tempo), and items beyond that count a little.
 */
function itemsDp(n: number, board: Board, data: SetData): { dp: number; note?: string } {
  const A = ASSUMPTIONS;
  const cost = new Map(data.units.map((u) => [u.apiName, u.cost]));
  const slots: { gain: number; who: string }[] = [];
  for (const u of board.units) {
    const c = cost.get(u.id);
    if (!c) continue;
    const g = itemGain(c, u.star);
    for (let i = u.items.length; i < A.itemsPerCarry; i++) slots.push({ gain: g, who: `${c}-cost ${u.star}★` });
  }
  slots.sort((a, b) => b.gain - a.gain);
  const carried = slots.slice(0, A.itemSlotsConsidered);
  while (carried.length < A.itemSlotFloor) carried.push({ gain: ITEM_GAIN * A.spareSlotShare, who: "spare" });
  // Whole items fill the best slots in turn; a fraction (a component is half an item) takes that share of the next slot.
  let dp = 0;
  const used: string[] = [];
  const whole = Math.floor(n);
  for (let i = 0; i < Math.min(whole, carried.length); i++) {
    dp += carried[i].gain;
    used.push(carried[i].who);
  }
  const frac = n - whole;
  if (frac > 0 && whole < carried.length) {
    dp += carried[whole].gain * frac;
    used.push(carried[whole].who);
  }
  const over = Math.max(0, n - carried.length);
  dp += over * ITEM_GAIN * A.itemOverflowShare;
  const where = [...new Set(used)].slice(0, 3).join(", ");
  return { dp, note: where ? `on ${where}${over > 0 ? `; ${fmt(over)} more count at ${A.itemOverflowShare * 100}%` : ""}` : undefined };
}

/**
 * Placement gained by `gold` spent on the copies the board wants: the best gain
 * per gold first (a copy's gain over what it costs to roll), only as many copies
 * as each unit still needs; the rest is idle. A board that doesn't roll uses
 * little of it that way.
 */
function goldDp(gold: number, plan: Plan): { dp: number; note: string } {
  const A = ASSUMPTIONS;
  const spendable = gold * plan.rollIntent;
  const options = plan.targets
    .map((t) => ({ t, price: copyPrice(t.cost, plan), gain: copyGain(t.cost, t.star) * t.weight }))
    .filter((o) => o.gain > 0)
    .sort((a, b) => b.gain / b.price - a.gain / a.price);
  let left = spendable;
  let dp = 0;
  let copies = 0;
  for (const o of options) {
    const take = Math.min(o.t.remaining, left / o.price);
    if (take <= 0) continue;
    dp += take * o.gain;
    copies += take;
    left -= take * o.price;
    if (left <= 0) break;
  }
  const idle = left + (gold - spendable);
  dp += idle * A.idleGold;
  const note = copies > 0.05 ? `buys about ${copies.toFixed(1)} copies the board wants` : "nothing the board still needs to buy: idle gold";
  return { dp, note };
}

function resourceParts(res: Resources, rec: Recurring | undefined, stage: string, plan: Plan, board: Board, data: SetData): Part[] {
  const A = ASSUMPTIONS;
  const f = stageFactors(stage);
  const parts: Part[] = [];
  const add = (label: string, dp: number, note?: string, copies = false) => {
    if (Math.abs(dp) > 1e-9) parts.push({ label, dp, note, copies });
  };
  const xpShare = A.xpUsefulByLevel[Math.max(6, Math.min(10, board.level))] ?? 1;
  const nameOf = (id: string) => (data.units.find((u) => u.apiName === id)?.name ?? id).replace(/\s*\(.*\)$/, "");

  // Gold: now, per round, per stage; XP counts as gold while the level still matters; a reroll saves its gold.
  const gold = res.gold ?? 0;
  if (gold) {
    const g = goldDp(gold, plan);
    add(`${res.gold} gold`, g.dp, g.note);
  }
  if (rec?.gold?.perRound) {
    const g = goldDp(rec.gold.perRound * f.rounds, plan);
    add(`${rec.gold.perRound} gold a round`, g.dp, `${f.rounds} rounds left; ${g.note}`);
  }
  if (rec?.gold?.perStage) {
    const g = goldDp(rec.gold.perStage * f.stages, plan);
    add(`${rec.gold.perStage} gold a stage`, g.dp, `${f.stages} stages left; ${g.note}`);
  }
  const xp = (res.xp ?? 0) + (rec?.xp?.perStage ?? 0) * f.stages + (res.xp && rec?.xp?.rounds ? res.xp * rec.xp.rounds : 0);
  if (xp) {
    const g = goldDp(xp * xpShare, plan);
    add(`${fmt(xp)} XP`, g.dp, xpShare < 1 ? `XP is worth ${Math.round(xpShare * 100)}% at level ${board.level}` : g.note);
  }
  const rolls = (res.rerolls ?? 0) + (rec?.rerolls?.perRound ? rec.rerolls.perRound * f.rounds * 0.5 : 0) + (rec?.rerolls?.perStage ? rec.rerolls.perStage * f.stages : 0);
  if (rolls) {
    const g = goldDp(rolls * SHOP.rerollGold, plan);
    add(`${fmt(rolls)} rerolls`, g.dp, `${(plan.rollIntent * 100).toFixed(0)}% of its gold is used rolling: ${g.note}`, true);
  }

  // Items: components count half, completed items one, artifacts and radiants a bit more.
  const itemsOf = (n: number, kind: string) => n * (A.itemEquivalents[kind] ?? 1);
  let items = itemsOf(res.components ?? 0, "component") + itemsOf(res.completed ?? 0, "completed") + itemsOf(res.artifacts ?? 0, "artifact") + itemsOf(res.emblems ?? 0, "emblem") + itemsOf(res.radiants ?? 0, "radiant");
  for (const n of res.named ?? []) items += itemsOf(n.n, n.kind);
  if (rec?.components?.rounds && res.components) items += itemsOf(res.components * rec.components.rounds, "component");
  if (items) {
    const r = itemsDp(items, board, data);
    const kinds = [res.components && `${fmt(res.components)} components`, res.completed && `${fmt(res.completed)} completed items`, res.artifacts && `${fmt(res.artifacts)} artifacts`, res.emblems && `${res.emblems} emblems`, res.radiants && `${fmt(res.radiants)} radiant items`, ...(res.named ?? []).map((n) => `${n.n} × ${n.name}`)].filter(Boolean);
    add(kinds.join(", "), r.dp, `about ${fmt(items)} items' worth${r.note ? `; ${r.note}` : ""}`);
  }

  // Champions: a copy the board wants is worth its share of a star-up; any other champion sells for its gold.
  const evUnit = (cost: number, star: number, name?: string, extraCopies = 0): { dp: number; note: string } => {
    const nCopies = 3 ** (star - 1) + extraCopies;
    const same = plan.targets.filter((t) => t.cost === cost);
    let hit = hitChance(cost, plan);
    let wanted = same.length ? same.reduce((a, t) => a + t.remaining * t.weight, 0) / same.length : 0;
    let per = same.length ? same.reduce((a, t) => a + copyGain(t.cost, t.star) * t.weight, 0) / same.length : 0;
    if (name) {
      const t = same.find((x) => nameOf(x.id) === name);
      hit = t ? 1 : 0;
      wanted = t ? t.remaining * t.weight : 0;
      per = t ? copyGain(t.cost, t.star) * t.weight : 0;
    }
    const useful = Math.min(nCopies, wanted);
    const sell = A.idleGold * cost;
    const gotHit = useful * per + (nCopies - useful) * sell;
    const miss = nCopies * sell;
    return {
      dp: hit * gotHit + (1 - hit) * miss,
      note: hit > 0 ? `${Math.round(hit * 100)}% a copy the board wants (it needs ~${wanted.toFixed(0)} more)` : "no unit of that cost on the board still wants copies",
    };
  };
  for (const u of res.units ?? []) {
    const ev = evUnit(u.cost, u.star, u.name);
    add(`${u.name ? u.name : `${u.n} × ${u.cost}-cost`}${u.star > 1 ? ` (${u.star}★)` : ""}`, u.n * ev.dp, ev.note, true);
  }
  if (res.allOfCost) {
    const n = plan.unitsOfCost[res.allOfCost] ?? 0;
    const wantedHere = plan.targets.filter((t) => t.cost === res.allOfCost);
    const dp = wantedHere.reduce((a, t) => a + copyGain(t.cost, t.star) * t.weight, 0) + Math.max(0, n - wantedHere.length) * A.idleGold * res.allOfCost;
    add(`a copy of each ${res.allOfCost}-cost (${n})`, dp, `${wantedHere.length} of them are units the board wants`, true);
  }
  if (rec?.copies?.perRound && rec.copies.cost) {
    const extra = rec.copies.perRound * f.rounds;
    const ev = evUnit(rec.copies.cost, 1, undefined, extra);
    add(`another copy each round`, ev.dp - evUnit(rec.copies.cost, 1).dp, `${f.rounds} rounds left; ${ev.note}`, true);
  }
  // Duplicators copy one unit each: the copy the board wants most (best gain per copy).
  for (const d of res.duplicators ?? []) {
    const wanted = plan.targets
      .filter((t) => t.cost <= d.maxCost)
      .map((t) => ({ t, gain: copyGain(t.cost, t.star) * t.weight }))
      .filter((w) => w.gain > 0)
      .sort((a, b) => b.gain - a.gain);
    let left = d.n;
    let dp = 0;
    const used: string[] = [];
    for (const w of wanted) {
      const take = Math.min(left, w.t.remaining);
      dp += take * w.gain;
      if (take > 0) used.push(nameOf(w.t.id));
      left -= take;
      if (left <= 0) break;
    }
    dp += left * 2 * A.idleGold;
    add(`${d.n} duplicator${d.n > 1 ? "s" : ""} (units up to ${d.maxCost}-cost)`, dp, used.length ? `copies of ${[...new Set(used)].join(", ")}` : `no unit up to ${d.maxCost}-cost still wants copies`, true);
  }
  // Pandora's Bench: the rightmost bench slots become random champions of the same cost every round.
  if (res.benchTransform && plan.targets.length) {
    const valuable = plan.targets.filter((t) => t.weight >= 1);
    const pool = valuable.length ? valuable : plan.targets;
    const cheapest = Math.min(...pool.map((t) => t.cost));
    const hit = hitChance(cheapest, plan);
    const perRound = res.benchTransform.slots * hit;
    const rounds = f.rounds * A.benchUseShare;
    const per = pool.filter((t) => t.cost === cheapest).reduce((a, t) => a + copyGain(t.cost, t.star) * t.weight, 0) / Math.max(1, pool.filter((t) => t.cost === cheapest).length);
    const wantedCopies = pool.filter((t) => t.cost === cheapest).reduce((a, t) => a + t.remaining, 0);
    add(`bench slots re-rolled every round`, Math.min(perRound * rounds, wantedCopies) * per, `${perRound.toFixed(2)} copies a round of your ${cheapest}-cost targets over ~${rounds.toFixed(0)} rounds`, true);
  }
  // Early arrivals do more than the final board shows (see tempoPerStage), and copies most of all on a board of cheap carries.
  const copyTempo = 1 + A.rerollTempo * plan.lowCostShare;
  for (const p of parts) p.dp *= f.tempo * (p.copies ? copyTempo : 1);
  return parts;
}

// -------------------------------------------------------------------- score

const LEVELS: Confidence[] = ["none", "low", "medium", "high"];
/** One notch less confident, but never "none": the augment is still scored. */
const lower = (c: Confidence): Confidence => LEVELS[Math.max(1, LEVELS.indexOf(c) - 1)];

/** Placement per unit of team strength lift: what one extra item does to the strength, set equal to what an item is worth. */
function strengthToPlacement(board: Board, data: SetData): number {
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  const states = board.units.filter((u) => defs.has(u.id)).map((u) => unitState(u, defs.get(u.id)!));
  if (states.length === 0) return 0;
  const before = teamPower(states);
  // The item goes on the unit that gains most from it.
  let best = 0;
  for (let i = 0; i < states.length; i++) {
    const alt = states.map((s) => ({ ...s }));
    alt[i].hp *= 1 + ASSUMPTIONS.itemHealth;
    alt[i].ad *= 1 + ASSUMPTIONS.itemDamage;
    alt[i].apBonus += ASSUMPTIONS.itemAbility;
    best = Math.max(best, teamPower(alt) / before - 1);
  }
  return best > 0.001 ? ITEM_GAIN / best : 0;
}

/**
 * Scores one augment for the board, as if picked at `stage` ("2-1", "3-2" or
 * "4-2"). `effects` is overridable for tests.
 */
export function scoreAugment(
  aug: Pick<SetAugment, "apiName" | "name" | "tier">,
  board: Board,
  data: SetData,
  stage: string,
  effects: Record<string, AugmentEffects> = EFFECTS,
): AugmentScore {
  const base: AugmentScore = { apiName: aug.apiName, name: aug.name, tier: aug.tier, dp: null, impact: null, statLift: 0, confidence: "none", parts: [], caveats: [] };
  const fx = effects[aug.apiName];
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  const placed = board.units.filter((u) => defs.has(u.id));
  if (!fx || fx.confidence === "none" || placed.length === 0) {
    base.caveats.push(placed.length === 0 ? "put units on the board first" : "this augment's effect isn't modelled");
    if (fx?.unparsed?.length) base.caveats.push(...fx.unparsed.map((t) => `not read: ${t}`));
    return base;
  }

  let confidence: Confidence = fx.confidence;
  const caveats: string[] = [];
  const parts: Part[] = [];
  const cal = ASSUMPTIONS.calibration;

  // Stat effects: recompute strength with every effect applied to the units it covers.
  let statLift = 0;
  if (fx.effects?.length) {
    const states = placed.map((u) => unitState(u, defs.get(u.id)!));
    const before = teamPower(states);
    const after = states.map((s) => ({ ...s }));
    let assumedScope = false;
    for (const e of fx.effects) {
      const t = timing(e);
      let covered = 0;
      for (const s of after) {
        if (!appliesTo(e, s)) continue;
        covered++;
        applyStat(s, e, perCount(e, after, board, data), t, board.level);
      }
      if (e.scope === "unknown") assumedScope = true;
      if (covered === 0) caveats.push(`no unit on this board is covered by "${describeEffect(e)}"`);
    }
    statLift = before > 0 ? teamPower(after) / before - 1 : 0;
    if (assumedScope) {
      statLift *= ASSUMPTIONS.unknownScopeCoverage;
      caveats.push("who receives part of the bonus isn't clear from the text; counted at half");
      confidence = lower(confidence);
    }
    if (fx.effects.some((e) => e.per === "uniqueOther" || e.per === "trait")) caveats.push("scales per ally or unit; an average is assumed");
    if (fx.effects.some((e) => e.everySeconds || e.ramp || e.delay)) caveats.push(`timed bonus, averaged over a ${ASSUMPTIONS.fightSeconds}s fight`);
    if (Math.abs(statLift) > 1e-6) {
      const k = strengthToPlacement(board, data);
      parts.push({ label: fx.effects.map(describeEffect).join(", "), dp: statLift * k, lift: statLift, note: `${(statLift * 100).toFixed(1)}% team strength` });
    }
  }

  if (fx.resources || fx.recurring) parts.push(...resourceParts(fx.resources ?? {}, fx.recurring, stage, buildPlan(board, data), board, data));
  if (fx.unparsed?.length) caveats.push(...fx.unparsed.map((t) => `not counted: ${t}`));
  if (fx.notes?.length) caveats.push(...fx.notes.map((t) => `not counted again: ${t}`));
  if (fx.conditional?.length) caveats.push(...fx.conditional.map((t) => `counted at half, it depends on something happening: ${t}`));

  // Built around a trait this board doesn't play: only what sells or still applies counts.
  if (fx.traits?.length) {
    const counts = new Map(computeTraits(board, data).map((t) => [t.name, t.count]));
    if (!fx.traits.some((t) => (counts.get(t) ?? 0) >= ASSUMPTIONS.playedTraitUnits)) {
      const gate = ASSUMPTIONS.offTraitShare;
      caveats.push(`built around ${fx.traits.join("/")}, which this board doesn't play: counted at ${gate * 100}%`);
      confidence = lower(confidence);
      for (const p of parts) {
        p.dp *= gate;
        if (p.lift !== undefined) p.lift *= gate;
      }
      statLift *= gate;
    }
  }
  for (const p of parts) p.dp *= cal;
  // Diminishing returns: keep the order, squeeze the very large totals (parts shrink with the total so the breakdown still adds up).
  const raw = parts.reduce((a, p) => a + p.dp, 0);
  const cap = ASSUMPTIONS.softCap;
  const dp = raw > 0 ? cap * Math.tanh(raw / cap) : raw;
  if (raw > 0 && dp < raw) {
    for (const p of parts) p.dp *= dp / raw;
    if (dp < 0.9 * raw) caveats.push("a very large total is squeezed (diminishing returns)");
  }
  return { ...base, dp, impact: dp, statLift, confidence: parts.length === 0 ? "none" : confidence, parts, caveats };
}

function describeEffect(e: StatEffect): string {
  const v = e.pct !== undefined ? `${+(e.pct * 100).toFixed(1)}%` : `${e.flat}${e.plusPerLevel ? ` (+${e.plusPerLevel} per level)` : ""}`;
  const stat = { health: "health", armor: "armor", mr: "magic resist", ad: "attack damage", ap: "ability power", as: "attack speed", crit: "crit chance", critDmg: "crit damage", damageAmp: "damage amp", durability: "durability", omnivamp: "omnivamp" }[e.stat];
  const who = { team: "team", holders: "item holders", unheld: "unequipped units", front: "front row", back: "back row", cost: `${e.cost}-cost units`, trait: `${e.trait} units`, unknown: "some units" }[e.scope];
  return `+${v} ${stat} (${who}${e.per ? `, per ${e.per}` : ""})`;
}

/** Scores the augments, best first; those that couldn't be scored come last. */
export function rankAugments(augments: SetAugment[], board: Board, data: SetData, stage: string, effects: Record<string, AugmentEffects> = EFFECTS): AugmentScore[] {
  return augments
    .map((a) => scoreAugment(a, board, data, stage, effects))
    .sort((a, b) => (b.impact ?? -Infinity) - (a.impact ?? -Infinity) || a.name.localeCompare(b.name));
}
