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
import { Plan, SHOP, SHOP_ODDS, buildPlan, hitChance } from "./augmentPlan";
import { Board, COLS, PlacedUnit, ROWS, computeTraits } from "./board";
import { ITEM_GAIN, copyGain, itemGain } from "./boardValue";
import { boardAtStage } from "./goalProgress";

export type Confidence = "high" | "medium" | "low" | "none";

export interface StatEffect {
  kind: "stat";
  scope: "team" | "holders" | "unheld" | "front" | "back" | "cost" | "trait" | "unknown";
  /** For scope "trait": the units that have it. */
  trait?: string;
  stat: "health" | "armor" | "mr" | "ad" | "ap" | "as" | "crit" | "critDmg" | "damageAmp" | "durability" | "omnivamp" | "abilityCrit" | "manaRegen";
  flat?: number;
  /** The flat bonus grows by this much per player level. */
  plusPerLevel?: number;
  pct?: number;
  /** The bonus repeats per ... (see perCount). */
  per?: "trait" | "activeTrait" | "bronzeTrait" | "frontRowUnit" | "level" | "item" | "oneUnitItem" | "emblem" | "uniqueOther" | "combat";
  /** For scope "holders": only holders of this kind of item ("Champions holding Spatula or Frying Pan items"). */
  heldKind?: "emblem";
  rows?: number;
  /** At most this many of the covered units get it (the holders of the items the augment gave), the most itemized first. */
  maxUnits?: number;
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
  /** The reward comes on reaching this level ("When you reach Level 9, gain 27 gold"): only for a board that gets there, late. */
  atLevel?: number;
  /** Chance that a reroll gives a free one back ("each time your shop is rerolled, 45% chance to gain a free reroll"). */
  rerollRefund?: number;
  components?: number;
  completed?: number;
  artifacts?: number;
  emblems?: number;
  radiants?: number;
  units?: { n: number; cost: number; star: number; fixed?: boolean; name?: string }[];
  /** Named items; `from` marks random completed items that build from that component ("2 items that build from B.F. Sword"). */
  named?: { name: string; n: number; kind: string; from?: string }[];
  /** Champion Duplicators: each copies a unit of up to maxCost. */
  duplicators?: { n: number; maxCost: number }[];
  /** The rightmost bench slots turn into random champions of the same cost every round. */
  benchTransform?: { slots: number };
  /** A copy of every champion of this cost. */
  allOfCost?: number;
}

export interface Recurring {
  gold?: { perRound?: number; perStage?: number };
  xp?: { perStage?: number; rounds?: number; perCombat?: number };
  rerolls?: { perRound?: number; perStage?: number };
  components?: { rounds?: number };
  /** An artifact anvil again after every N player combats. */
  artifacts?: { everyCombats: number };
  /** Another copy of the champion(s) gained, every round. */
  copies?: { perRound?: number; perLevel?: number; cost?: number; n?: number };
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
  /** Mana a unit gains a second without regen: 10 per attack at its attack speed plus this much from damage taken. */
  manaFromDamage: 2,
  /** How much of attack speed's gain reaches ability damage (faster mana generation). */
  abilityFromAttackSpeed: 0.5,
  /** Length of a fight that bonuses are averaged over. */
  fightSeconds: 30,
  /**
   * How much a back-row unit's effective HP counts towards team strength next to a front-row unit's (rows in between
   * linearly): the front line takes most of a fight's damage, so back-row health is worth less. Domain knowledge, not
   * fitted (final boards don't record who took damage).
   */
  backRowEhpWeight: 0.5,
  /** Healing from omnivamp, as a share of damage dealt that counts as extra health. */
  omnivampValue: 0.8,
  /** What a trait augment is worth to a board that doesn't play its trait (its grants still sell, its bonuses do nothing). */
  offTraitShare: 0.1,
  /** Units of a trait a board needs for the trait to count as played. */
  playedTraitUnits: 2,
  /** What a random emblem whose trait the board doesn't play is still worth, as a share of a fitting one (held for a pivot, reforged). */
  randomEmblemFloor: 0.2,
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
  /**
   * What an item off the board's build is worth next to one in it (the board's items say what it builds: which completed
   * items, made of which components). A completed item the board doesn't hold counts this much, more when its components
   * are ones the board builds from; a component the board's recipes don't use (a Rod for an attack-damage board) counts
   * offBuildComponentShare. Random items count at their expected fit. Unmeasured; a board without items counts every item fully.
   */
  offBuildItemShare: 0.5,
  offBuildComponentShare: 0.5,
  /** Share of an item beyond the free slots that still counts. */
  itemOverflowShare: 0.3,
  /**
   * Placement per gold the board doesn't spend on copies it wants: it buys levels, interest and safety. The designers
   * price gold against items within a tier at about 15 gold an item (Gold: 21 gold beside 1-2.25 items; Prismatic: 22
   * gold beside ~2; Silver: 8-12 gold beside 0.5-1), but that is what an augment gives, not what it does: spare gold
   * only pays through what it buys later. Taken at 40 gold an item (at 15-25, plain gold augments outranked the copies
   * and items TFT Academy's guides pick on both halves of its comps; the old 0.003 was ~100 gold an item).
   */
  idleGold: ITEM_GAIN / 40,
  /** Spare gold saturates: x gold counts as spareGoldScale × (1 − e^(−x/spareGoldScale)) (20 gold ≈ 85%, 60 ≈ 63%, 126 ≈ 41% of face value). */
  spareGoldScale: 60,
  /**
   * Gold, copies and items picked early do more than the final board shows: they arrive before the fights they decide,
   * save rolling later and compound. Their value grows by this per stage left (the fit sees only final boards, so it
   * can't measure this: it is an assumption, and the one that decides how economy compares with stats).
   */
  tempoPerStage: 0.1,
  /** Stages left after the stage an augment is picked at, and rounds per stage, for recurring gains. */
  stagesLeft: { "2-1": 5, "3-2": 4, "4-2": 3 } as Record<string, number>,
  roundsPerStage: 6,
  /** The player's level at each augment stage (as in goalProgress's STAGE_PROGRESS). */
  stageLevel: { "2-1": 4, "3-2": 6, "4-2": 7 } as Record<string, number>,
  /** Player combats a stage (the rest are a carousel and a monster round). */
  combatsPerStage: 5,
  /** XP matters less the higher the level (level 9 and 10 have little left to buy). */
  xpUsefulByLevel: { 6: 1, 7: 1, 8: 0.6, 9: 0.2, 10: 0 } as Record<number, number>,
  /**
   * A board of cheap carries (a reroll board) lives on hitting 3 stars early: its copies decide fights the final board
   * can't show (star-ups at level 5-6 beat the lobby), so their value is multiplied by 1 + this x the carries' low-cost
   * share. Domain knowledge, not measured: the fit sees only final boards.
   */
  rerollTempo: 2,
  /** No augment moves a placement by more than about this: totals are squeezed towards it (tanh), keeping their order. */
  softCap: 0.8,
  /**
   * With earlier picks, the cap holds for all the augments together and grows as (number of augments)^this: 1 lets two
   * augments move placement by up to ~1.6 and three by ~2.4. An augment is worth what it adds to the squeezed total
   * (both totals under the same cap), so a pick after a strong one has less room left.
   */
  combinedCapGrowth: 1,
  /**
   * Gold the player rolls with from that stage on, before the board's roll intent (a reroll board uses all of it, one
   * of expensive carries about a third). The copies it buys come first: an augment's copies count only for what the
   * player's own rolling wouldn't find, and a reroll refund multiplies this gold. A typical game's rolling, not measured.
   */
  ownRollGold: { "2-1": 100, "3-2": 80, "4-2": 50 } as Record<string, number>,
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
  /** Share of the part that arrives over the rest of the game (per round or stage) rather than now: it gets the tempo of halfway through. */
  later?: number;
  /** Copies of units: worth more on a board of cheap carries (see rerollTempo); a number is the share of the part that is copies (gold buys some, the rest sits idle). */
  copies?: boolean | number;
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
  /** With earlier picks given: what the augment would be worth without them (dp is then its value on top of them). */
  alone?: number;
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
  /** Ability damage can crit too (Precision). */
  abilityCrit: boolean;
  apBonus: number; // ability power as a fraction added to ability damage
  damageAmp: number;
  durability: number;
  omnivamp: number;
  /** Mana regenerated a second. */
  manaRegen: number;
  items: number;
  /** Emblems (Spatula and Frying Pan items) it holds. */
  emblems: number;
  row: number;
  traits: string[];
  cost: number;
}

function unitState(u: PlacedUnit, def: SetUnit, emblemIds?: Set<string>): UnitState {
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
    abilityCrit: false,
    apBonus: A.itemAbility * items,
    damageAmp: 0,
    durability: 0,
    omnivamp: 0,
    manaRegen: 0,
    items,
    emblems: emblemIds ? u.items.filter((i) => emblemIds.has(i)).length : 0,
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
  // Mana regen casts the ability more often: its share of the mana the unit gains a second.
  const income = 10 * s.attackSpeed + A.manaFromDamage;
  const regen = 1 + s.manaRegen / income;
  const ability = s.abilityBase * (1 + s.apBonus) * (1 + A.abilityFromAttackSpeed * Math.max(0, asGain)) * (1 + (s.abilityCrit ? crit : 0)) * regen;
  return (auto + ability) * (1 + s.damageAmp);
}

/**
 * Team strength: the geometric mean of total effective HP and total DPS (the
 * square root of their product, as in Lanchester's square law), so +10% of
 * both makes a team 10% stronger, not 21%.
 */
export function teamPower(states: UnitState[]): number {
  if (states.length === 0) return 0;
  const w = (row: number) => 1 - (1 - ASSUMPTIONS.backRowEhpWeight) * (row / (ROWS - 1));
  const hp = states.reduce((a, s) => a + ehp(s) * w(s.row), 0);
  const dmg = states.reduce((a, s) => a + dps(s), 0);
  return Math.sqrt(hp * dmg);
}

/**
 * How useful a random emblem is to the board: the share of the set's emblem traits it plays (playedTraitUnits or
 * more units), never below randomEmblemFloor (an emblem can still be held for a later pivot, or reforged).
 */
export function randomEmblemUse(board: Board, data: SetData): number {
  const emblemTraits = new Set(
    data.items.filter((i) => i.kind === "emblem" && / Emblem$/i.test(i.name)).map((i) => i.name.replace(/ Emblem$/i, "").toLowerCase()),
  );
  if (emblemTraits.size === 0) return 1;
  const played = computeTraits(board, data).filter((t) => t.count >= ASSUMPTIONS.playedTraitUnits && emblemTraits.has(t.name.toLowerCase())).length;
  return Math.max(ASSUMPTIONS.randomEmblemFloor, played / emblemTraits.size);
}

// -------------------------------------------------------------- stat effects

/** How many times a "per ..." bonus applies to a unit. */
function perCount(e: StatEffect, states: UnitState[], board: Board, data: SetData, combats = 0): number {
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
    case "combat":
      // Stacks after every player combat: a fight sees half of what the rest of the game builds, on average.
      return combats / 2;
  }
}

function appliesTo(e: StatEffect, s: UnitState): boolean {
  switch (e.scope) {
    case "team":
    case "unknown":
      return true;
    case "holders":
      return e.heldKind === "emblem" ? s.emblems > 0 : s.items > 0;
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
    case "abilityCrit":
      s.abilityCrit = true;
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
    case "manaRegen":
      s.manaRegen += flat;
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
function itemsDp(n: number, board: Board, data: SetData, goal?: Board): { dp: number; note?: string } {
  const A = ASSUMPTIONS;
  const carried = itemSlots(board, data, goal);
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

/** The item slots an augment's items can fill, best first (at most itemSlotsConsidered, padded with spare slots). */
function itemSlots(board: Board, data: SetData, goal?: Board): { gain: number; who: string }[] {
  const A = ASSUMPTIONS;
  const cost = new Map(data.units.map((u) => [u.apiName, u.cost]));
  const slots: { gain: number; who: string }[] = [];
  // Towards a goal that holds items, the free slots are the goal's: each unit up to its goal items, valued at its goal
  // star. Without items in the goal (or without a goal), every unit up to a full carry.
  const goalItems = goal?.units.some((u) => u.items.length > 0);
  board.units.forEach((u, i) => {
    const c = cost.get(u.id);
    if (!c) return;
    const target = goal ? goal.units[i] : u;
    const capacity = goalItems ? target.items.length : A.itemsPerCarry;
    const g = itemGain(c, target.star);
    for (let k = u.items.length; k < capacity; k++) slots.push({ gain: g, who: `${c}-cost ${target.star}★` });
  });
  slots.sort((a, b) => b.gain - a.gain);
  const carried = slots.slice(0, A.itemSlotsConsidered);
  while (carried.length < A.itemSlotFloor) carried.push({ gain: ITEM_GAIN * A.spareSlotShare, who: "spare" });
  return carried;
}

/**
 * Placement gained by `gold` spent rolling for the copies the board wants. One roll shows every wanted unit at once
 * (a board waiting on five 3-costs hits one of them far more often than any one of them), so gold buys rolls, each
 * finding the expected copies of every unit still short of its goal (the standard odds, 5 slots) and paying for them.
 * Only `rollIntent` of the gold is used that way (a board of expensive carries rolls little); the rest is idle.
 * What it buys comes off the plan's counts (the caller's own copy).
 */
/** Placement from spare gold (levels, interest, safety), on the plan's shared ledger: a board can only use so much. */
function spareDp(gold: number, plan: Plan): number {
  const A = ASSUMPTIONS;
  const s0 = plan.spareGold ?? 0;
  const F = (x: number) => A.spareGoldScale * (1 - Math.exp(-x / A.spareGoldScale));
  plan.spareGold = s0 + gold;
  return (F(s0 + gold) - F(s0)) * A.idleGold;
}

function goldDp(
  gold: number,
  plan: Plan,
  opts: { rolls?: "only" | "none"; savedCap?: number; own?: boolean } = {},
): { dp: number; copyShare: number; note: string } {
  // Rerolls can only roll (what they don't find copies with saves the player's own rolling gold, up to savedCap);
  // XP can't roll at all; gold rolls with the board's roll intent.
  const spendable = opts.rolls === "only" ? gold : opts.rolls === "none" ? 0 : gold * plan.rollIntent;
  const odds = SHOP_ODDS[Math.max(1, Math.min(10, plan.level))] ?? SHOP_ODDS[10];
  const perRoll = (cost: number) => (SHOP.slots * (odds[cost - 1] ?? 0)) / 100 / Math.max(1, plan.unitsOfCost[cost] ?? 1);
  let left = spendable;
  let dp = 0;
  let copies = 0;
  for (let i = 0; i < 400 && left > 1e-6; i++) {
    const open = plan.targets.filter((t) => t.remaining > 1e-6 && perRoll(t.cost) > 0);
    if (open.length === 0) break;
    // One roll: its price, and what it finds (never more than a unit still wants).
    const hits = open.map((t) => Math.min(t.remaining, perRoll(t.cost)));
    const price = SHOP.rerollGold + open.reduce((a, t, k) => a + hits[k] * t.cost, 0);
    const share = Math.min(1, left / price);
    open.forEach((t, k) => {
      const n = hits[k] * share;
      dp += n * copyGain(t.cost, t.star) * t.weight;
      copies += n;
      t.remaining -= n;
    });
    left -= price * share;
  }
  const idle = opts.rolls === "only" ? Math.min(left, opts.savedCap ?? 0) : left + (gold - spendable);
  const copyDp = dp;
  // Spare gold buys levels, interest and safety, with diminishing returns over all the picks' gold together (the
  // plan's ledger): a board can only use so much. The player's own gold isn't counted (it's the baseline).
  if (!opts.own) dp += spareDp(idle, plan);
  const note = copies > 0.05 ? `finds about ${copies.toFixed(1)} copies the board wants` : opts.rolls === "none" ? "spare: levels and interest" : "nothing the board still needs to buy: spare gold";
  return { dp, copyShare: dp > 0 ? copyDp / dp : 0, note };
}

/** What a board builds: how many of its items use each component, and which completed items it holds (by name). */
interface Build {
  components: Map<string, number>;
  items: Map<string, number>;
}
const buildCache = new WeakMap<Board, Build | null>();
function buildOf(board: Board, data: SetData): Build | null {
  if (buildCache.has(board)) return buildCache.get(board)!;
  const byId = new Map(data.items.map((i) => [i.apiName, i]));
  const components = new Map<string, number>();
  const items = new Map<string, number>();
  let any = false;
  for (const u of board.units)
    for (const id of u.items) {
      const it = byId.get(id);
      if (!it || it.kind !== "completed") continue;
      any = true;
      items.set(it.name, (items.get(it.name) ?? 0) + 1);
      for (const c of it.composition ?? []) {
        const name = byId.get(c)?.name;
        if (name) components.set(name, (components.get(name) ?? 0) + 1);
      }
    }
  const b = any ? { components, items } : null;
  buildCache.set(board, b);
  return b;
}

/** How well items fit the board's build, each 0..1 of a fitting item (see offBuildItemShare). */
function itemFit(build: Build | null, data: SetData) {
  const A = ASSUMPTIONS;
  const byId = new Map(data.items.map((i) => [i.apiName, i]));
  const compName = (id: string) => byId.get(id)?.name ?? id;
  const completed = data.items.filter((i) => i.kind === "completed" && (i.composition?.length ?? 0) === 2);
  const usesComp = (name: string) => (build?.components.get(name) ?? 0) > 0;
  // A Spatula or Frying Pan makes an emblem with anything, so it fits whatever the board builds (its value is the emblem's).
  const emblemMaker = (name: string) => {
    const id = data.items.find((i) => i.name === name)?.apiName;
    return !!id && data.items.some((i) => i.kind === "emblem" && i.composition?.includes(id));
  };
  const completedFit = (it: { name: string; composition?: string[] }) => {
    if (!build || build.items.has(it.name)) return 1;
    const share = (it.composition ?? []).filter((c) => usesComp(compName(c))).length / Math.max(1, it.composition?.length ?? 1);
    return A.offBuildItemShare + (1 - A.offBuildItemShare) * 0.5 * share;
  };
  const mean = (xs: number[]) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 1);
  return {
    /** n of the named component: fully when the board's recipes use that kind (copies pair into its items too). */
    component: (name: string, n: number) => (!build || usesComp(name) || emblemMaker(name) ? n : n * A.offBuildComponentShare),
    // Random components go into whatever the board builds (two of them make an item, any pair): no discount. A pile of
    // one named component is different (4 Rods on an attack-damage board).
    randomComponent: 1,
    completed: (name: string) => {
      const it = data.items.find((i) => i.name === name);
      return it ? completedFit(it) : build ? A.offBuildItemShare : 1;
    },
    randomCompleted: build ? mean(completed.map(completedFit)) : 1,
    completedFrom: (comp: string) => {
      const id = data.items.find((i) => i.name === comp)?.apiName;
      return build ? mean(completed.filter((i) => id && i.composition?.includes(id)).map(completedFit)) : 1;
    },
  };
}

/**
 * Items' worth of what an augment gives: components count half, completed items one, artifacts and radiants a bit
 * more; components and completed items scaled by how well they fit what the board builds (`build`: the goal's items in
 * goal mode, else the board's own).
 */
function itemEquivalents(res: Resources, rec: Recurring | undefined, board: Board, data: SetData, build: Board = board): number {
  const fit = itemFit(buildOf(build, data), data);
  const itemsOf = (n: number, kind: string) => n * (ASSUMPTIONS.itemEquivalents[kind] ?? 1);
  // Emblems here are random ones ("Gain 3 random Emblems"; a named emblem is under named): one is only as good as an
  // item if its trait is one the board plays, so it counts at the share of the set's emblem traits this board plays.
  const emblemUse = res.emblems ? randomEmblemUse(board, data) : 1;
  const comps = (res.components ?? 0) + (rec?.components?.rounds && res.components ? res.components * rec.components.rounds : 0);
  let items =
    itemsOf(comps, "component") * fit.randomComponent +
    itemsOf(res.completed ?? 0, "completed") * fit.randomCompleted +
    itemsOf(res.artifacts ?? 0, "artifact") +
    itemsOf(res.emblems ?? 0, "emblem") * emblemUse +
    itemsOf(res.radiants ?? 0, "radiant");
  for (const n of res.named ?? []) {
    if (n.kind === "component") items += itemsOf(fit.component(n.name, n.n), "component");
    else if (n.kind === "completed") items += itemsOf(n.n, "completed") * (n.from ? fit.completedFrom(n.from) : fit.completed(n.name));
    else items += itemsOf(n.n, n.kind);
  }
  return items;
}

function resourceParts(res: Resources, rec: Recurring | undefined, stage: string, sharedPlan: Plan, board: Board, data: SetData, goal?: Board, out?: { copiesLeft: number; copiesWanted: number }): Part[] {
  const A = ASSUMPTIONS;
  // One count of the copies each unit still needs, shared by every source below (and, through mergeEffects, by
  // several augments): copies one source covers aren't wanted again from the next. Sources that give specific copies
  // go first, gold last (it can buy whatever is still missing).
  const plan: Plan = { ...sharedPlan, targets: sharedPlan.targets.map((t) => ({ ...t })) };
  const take = (t: Plan["targets"][number], n: number) => (t.remaining = Math.max(0, t.remaining - n));
  // The player's own rolling finds copies too: those aren't wanted from augments.
  const ownGold = A.ownRollGold[stage] ?? 0;
  goldDp(ownGold, plan, { own: true });
  plan.spareGold = 0;
  plan.rollBudget = ownGold * plan.rollIntent;
  if (out) out.copiesWanted = plan.targets.reduce((a, t) => a + t.remaining, 0);
  const f = stageFactors(stage);
  const parts: Part[] = [];
  const add = (label: string, dp: number, note?: string, copies: boolean | number = false, later = 0) => {
    if (Math.abs(dp) > 1e-9) parts.push({ label, dp, note, copies, later });
  };
  const xpShare = A.xpUsefulByLevel[Math.max(6, Math.min(10, board.level))] ?? 1;
  const nameOf = (id: string) => (data.units.find((u) => u.apiName === id)?.name ?? id).replace(/\s*\(.*\)$/, "");

  const emblemUse = res.emblems ? randomEmblemUse(board, data) : 1;
  // Anvils again every N player combats: as many as the stages left hold, arriving later in the game.
  const moreAnvils = rec?.artifacts?.everyCombats ? Math.floor((f.stages * A.combatsPerStage) / rec.artifacts.everyCombats) : 0;
  if (moreAnvils) res = { ...res, artifacts: (res.artifacts ?? 0) + moreAnvils };
  const items = itemEquivalents(res, rec, board, data, goal ?? board);
  const laterItems = moreAnvils * (A.itemEquivalents.artifact ?? 1) + (rec?.components?.rounds && res.components ? res.components * rec.components.rounds * (A.itemEquivalents.component ?? 0.5) : 0);
  if (items) {
    const r = itemsDp(items, board, data, goal);
    const kinds = [res.components && `${fmt(res.components)} components`, res.completed && `${fmt(res.completed)} completed items`, res.artifacts && `${fmt(res.artifacts)} artifacts`, res.emblems && `${res.emblems} random emblems${emblemUse < 1 ? ` (${Math.round(emblemUse * 100)}% likely to fit)` : ""}`, res.radiants && `${fmt(res.radiants)} radiant items`, ...(res.named ?? []).map((n) => `${n.n} × ${n.name}`)].filter(Boolean);
    add(kinds.join(", "), r.dp, `about ${fmt(items)} items' worth${r.note ? `; ${r.note}` : ""}`, false, Math.min(1, laterItems / items));
  }

  // Champions: a copy the board wants is worth its share of a star-up; any other champion sells for its gold.
  // use: copies that complete a star-up the board wants (what a reroll board's tempo multiplies); sell: the rest,
  // worth only their gold.
  const evUnit = (cost: number, star: number, name?: string, extraCopies = 0): { use: number; sell: number; note: string } => {
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
    const got = hit * useful;
    if (name) {
      const t = same.find((x) => nameOf(x.id) === name);
      if (t) take(t, got);
    } else for (const t of same) take(t, got / same.length);
    return {
      use: hit * useful * per,
      /** Gold the unwanted copies sell for. */
      sell: (hit * (nCopies - useful) + (1 - hit) * nCopies) * cost,
      note: hit > 0 ? `${Math.round(hit * 100)}% a copy the board wants (it needs ~${wanted.toFixed(0)} more)` : "no unit of that cost on the board still wants copies",
    };
  };
  const addUnits = (label: string, use: number, sellGold: number, note: string, later = 0) => {
    const sell = sellGold > 0 ? spareDp(sellGold, plan) : 0;
    if (use > 0 || sell === 0) add(label, use, note, true, later);
    if (sell > 0) add(use > 0 ? `${label}: the rest sold` : `${label} (sold)`, sell, use > 0 ? "copies the board doesn't need sell for their gold" : note, false, later);
  };
  for (const u of res.units ?? []) {
    const ev = evUnit(u.cost, u.star, u.name);
    addUnits(`${u.name ? u.name : `${u.n} × ${u.cost}-cost`}${u.star > 1 ? ` (${u.star}★)` : ""}`, u.n * ev.use, u.n * ev.sell, ev.note);
  }
  if (res.allOfCost) {
    const n = plan.unitsOfCost[res.allOfCost] ?? 0;
    const wantedHere = plan.targets.filter((t) => t.cost === res.allOfCost);
    const still = wantedHere.filter((t) => t.remaining >= 1);
    const dp = still.reduce((a, t) => a + copyGain(t.cost, t.star) * t.weight, 0) + spareDp(Math.max(0, n - still.length) * res.allOfCost, plan);
    for (const t of still) take(t, 1);
    add(`a copy of each ${res.allOfCost}-cost (${n})`, dp, `${wantedHere.length} of them are units the board wants`, true);
  }
  if (rec?.copies?.perRound && rec.copies.cost) {
    const extra = rec.copies.perRound * f.rounds;
    const saved = plan.targets.map((t) => t.remaining);
    const one = evUnit(rec.copies.cost, 1);
    plan.targets.forEach((t, i) => (t.remaining = saved[i]));
    const ev = evUnit(rec.copies.cost, 1, undefined, extra);
    addUnits(`another copy each round`, ev.use - one.use, ev.sell - one.sell, `${f.rounds} rounds left; ${ev.note}`, 1);
  }
  // "The same one again every time you level up": a copy of the same random champion per level gained by the goal.
  if (rec?.copies?.perLevel && rec.copies.cost) {
    const levels = Math.max(0, (goal ?? board).level - (A.stageLevel[stage] ?? board.level)) * rec.copies.perLevel;
    if (levels > 0) {
      const saved = plan.targets.map((t) => t.remaining);
      const one = evUnit(rec.copies.cost, 1);
      plan.targets.forEach((t, i) => (t.remaining = saved[i]));
      const ev = evUnit(rec.copies.cost, 1, undefined, levels);
      addUnits(`another copy each level (${levels})`, ev.use - one.use, ev.sell - one.sell, ev.note, 1);
    }
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
      const n = Math.min(left, w.t.remaining);
      dp += n * w.gain;
      if (n > 0) used.push(nameOf(w.t.id));
      take(w.t, n);
      left -= n;
      if (left <= 0) break;
    }
    dp += spareDp(left * 2, plan);
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
    const cheap = pool.filter((t) => t.cost === cheapest);
    const wantedCopies = cheap.reduce((a, t) => a + t.remaining, 0);
    const got = Math.min(perRound * rounds, wantedCopies);
    for (const t of cheap) take(t, wantedCopies > 0 ? (got * t.remaining) / wantedCopies : 0);
    add(`bench slots re-rolled every round`, got * per, `${perRound.toFixed(2)} copies a round of your ${cheapest}-cost targets over ~${rounds.toFixed(0)} rounds`, true, 1);
  }
  // A reroll refund: each paid reroll gives another with chance p, so the player's rolling gold goes 1/(1-p) times as far.
  if (res.rerollRefund) {
    const p = Math.min(0.9, res.rerollRefund);
    const rolled = ownGold * plan.rollIntent;
    const g = goldDp((rolled * p) / (1 - p), plan, { rolls: "only", savedCap: rolled * p });
    add(`free rerolls (${Math.round(p * 100)}% a reroll)`, g.dp, `your ~${Math.round(ownGold * plan.rollIntent)} gold of rolling goes ${(1 / (1 - p)).toFixed(1)}× as far; ${g.note}`, g.copyShare);
  }
  // Gold: now, per round, per stage; XP counts as gold while the level still matters; a reroll saves its gold.
  const gold = res.gold ?? 0;
  if (gold) {
    const g = goldDp(gold, plan);
    add(`${res.gold} gold`, g.dp, g.note, g.copyShare);
  }
  if (rec?.gold?.perRound) {
    const g = goldDp(rec.gold.perRound * f.rounds, plan);
    add(`${rec.gold.perRound} gold a round`, g.dp, `${f.rounds} rounds left; ${g.note}`, g.copyShare, 1);
  }
  if (rec?.gold?.perStage) {
    const g = goldDp(rec.gold.perStage * f.stages, plan);
    add(`${rec.gold.perStage} gold a stage`, g.dp, `${f.stages} stages left; ${g.note}`, g.copyShare, 1);
  }
  const laterXp = (rec?.xp?.perStage ?? 0) * f.stages + (rec?.xp?.perCombat ?? 0) * f.stages * A.combatsPerStage;
  const xp = (res.xp ?? 0) + laterXp + (res.xp && rec?.xp?.rounds ? res.xp * rec.xp.rounds : 0);
  if (xp) {
    const g = goldDp(xp * xpShare, plan, { rolls: "none" });
    add(`${fmt(xp)} XP`, g.dp, xpShare < 1 ? `XP is worth ${Math.round(xpShare * 100)}% at level ${board.level}` : g.note, false, laterXp / xp);
  }
  const laterRolls = (rec?.rerolls?.perRound ? rec.rerolls.perRound * f.rounds * 0.5 : 0) + (rec?.rerolls?.perStage ? rec.rerolls.perStage * f.stages : 0);
  const rolls = (res.rerolls ?? 0) + laterRolls;
  if (rolls) {
    // Free rerolls replace rolls the player would pay for, up to their own rolling budget (shared by the picks).
    const budget = plan.rollBudget ?? 0;
    const g = goldDp(rolls * SHOP.rerollGold, plan, { rolls: "only", savedCap: budget });
    plan.rollBudget = Math.max(0, budget - rolls * SHOP.rerollGold);
    add(`${fmt(rolls)} rerolls`, g.dp, `${(plan.rollIntent * 100).toFixed(0)}% of its gold is used rolling: ${g.note}`, g.copyShare, laterRolls / rolls);
  }

  // Early arrivals do more than the final board shows (see tempoPerStage), and copies most of all on a board of cheap carries.
  if (out) out.copiesLeft = plan.targets.reduce((a, t) => a + t.remaining, 0);
  if (res.atLevel) {
    // Only a board that reaches the level gets it, and only then (late).
    const reaches = (goal ?? board).level >= res.atLevel;
    for (const p of parts) {
      if (!reaches) p.dp = 0;
      p.later = 1;
    }
    if (!reaches) parts.push({ label: `needs level ${res.atLevel}`, dp: 0, note: `this board stops at level ${(goal ?? board).level}` });
  }
  const copyTempo = 1 + A.rerollTempo * plan.lowCostShare;
  for (const p of parts) {
    const share = p.copies === true ? 1 : typeof p.copies === "number" ? p.copies : 0;
    const tempo = (1 - (p.later ?? 0)) * f.tempo + (p.later ?? 0) * (1 + (f.tempo - 1) / 2);
    p.dp *= tempo * (1 + (copyTempo - 1) * share);
  }
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

/** Before the soft cap: the calibrated parts of one set of effects, and what's needed to describe them. */
interface RawScore {
  raw: number;
  parts: Part[];
  statLift: number;
  confidence: Confidence;
  caveats: string[];
}

/** Whether a trait augment's trait is played on the board (augments without traits always pass). */
function traitPlayed(fx: AugmentEffects, board: Board, data: SetData): boolean {
  if (!fx.traits?.length) return true;
  const counts = new Map(computeTraits(board, data).map((t) => [t.name, t.count]));
  return fx.traits.some((t) => (counts.get(t) ?? 0) >= ASSUMPTIONS.playedTraitUnits);
}

/**
 * `board` is where the player is when the augment is picked (resources are priced from there); `goal`, in the
 * planner's goal mode, is the board they're building towards: stat bonuses are priced on it (it's the board that
 * has to win), and copies and items count towards its stars and item slots.
 */
function rawScore(fx: AugmentEffects, board: Board, data: SetData, stage: string, goal?: Board): RawScore {
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  const statBoard = goal ?? board;
  const placed = statBoard.units.filter((u) => defs.has(u.id));
  let confidence: Confidence = fx.confidence;
  const caveats: string[] = [];
  const parts: Part[] = [];
  const cal = ASSUMPTIONS.calibration;

  // Stat effects: recompute strength with every effect applied to the units it covers.
  let statLift = 0;
  if (fx.effects?.length) {
    const emblemIds = new Set(data.items.filter((i) => i.kind === "emblem").map((i) => i.apiName));
    const states = placed.map((u) => unitState(u, defs.get(u.id)!, emblemIds));
    const before = teamPower(states);
    const after = states.map((s) => ({ ...s }));
    let assumedScope = false;
    const combatsLeft = stageFactors(stage).stages * ASSUMPTIONS.combatsPerStage;
    for (const e of fx.effects) {
      const t = timing(e);
      let covered = 0;
      const order = e.maxUnits ? [...after].sort((a, b) => b.items - a.items) : after;
      for (const s of order) {
        if (!appliesTo(e, s)) continue;
        if (e.maxUnits && covered >= e.maxUnits) break;
        covered++;
        applyStat(s, e, perCount(e, after, statBoard, data, combatsLeft), t, statBoard.level);
      }
      // "Champions holding Spatula or Frying Pan items" on a board without any: the emblem the augment hands out makes one.
      if (covered === 0 && e.heldKind === "emblem" && order.length) {
        const s = [...after].sort((x, y) => y.items - x.items)[0];
        applyStat(s, e, perCount(e, after, statBoard, data, combatsLeft), t, statBoard.level);
        covered = 1;
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
      const k = strengthToPlacement(statBoard, data);
      // A bonus picked early also wins the fights before the final board, like the items and gold it competes with.
      const tempo = stageFactors(stage).tempo;
      parts.push({ label: fx.effects.map(describeEffect).join(", "), dp: statLift * k * tempo, lift: statLift, note: `${(statLift * 100).toFixed(1)}% team strength` });
    }
  }

  if (fx.resources || fx.recurring) parts.push(...resourceParts(fx.resources ?? {}, fx.recurring, stage, buildPlan(board, data, goal), board, data, goal));
  if (fx.unparsed?.length) caveats.push(...fx.unparsed.map((t) => `not counted: ${t}`));
  if (fx.notes?.length) caveats.push(...fx.notes.map((t) => `not counted again: ${t}`));
  if (fx.conditional?.length) caveats.push(...fx.conditional.map((t) => `counted at half, it depends on something happening: ${t}`));

  // Built around a trait this board doesn't play: only what sells or still applies counts.
  if (!traitPlayed(fx, statBoard, data)) {
    const gate = ASSUMPTIONS.offTraitShare;
    caveats.push(`built around ${fx.traits!.join("/")}, which this board doesn't play: counted at ${gate * 100}%`);
    confidence = lower(confidence);
    for (const p of parts) {
      p.dp *= gate;
      if (p.lift !== undefined) p.lift *= gate;
    }
    statLift *= gate;
  }
  for (const p of parts) p.dp *= cal;
  return { raw: parts.reduce((a, p) => a + p.dp, 0), parts, statLift, confidence: parts.length === 0 ? "none" : confidence, caveats };
}

/**
 * Several augments' effects as one, so the model can price them together: stat effects apply together (a second
 * damage bonus lands on an already stronger team), and resources add up before they're priced (a second item
 * augment fills the slots the first left, copies stop counting once the board has the ones it wants, gold runs out
 * of things to buy). Trait gating isn't carried: callers leave out augments whose trait the board doesn't play.
 */
export function mergeEffects(list: AugmentEffects[]): AugmentEffects {
  const out: AugmentEffects = { confidence: "high" };
  const res: Resources = {};
  const rec: Recurring = {};
  const num = <T extends object>(dst: T, src: T | undefined) => {
    for (const [k, v] of Object.entries(src ?? {})) if (typeof v === "number") (dst as any)[k] = ((dst as any)[k] ?? 0) + v;
  };
  for (const fx of list) {
    if (fx.effects?.length) out.effects = [...(out.effects ?? []), ...fx.effects];
    const c0 = fx.recurring;
    // "N components (or XP) now and again for R rounds" multiplies that augment's own amount; turned into totals
    // first, so the rounds can't multiply what another augment gives.
    const r = fx.resources && {
      ...fx.resources,
      ...(fx.resources.components && c0?.components?.rounds ? { components: fx.resources.components * (1 + c0.components.rounds) } : {}),
      ...(fx.resources.xp && c0?.xp?.rounds ? { xp: fx.resources.xp * (1 + c0.xp.rounds) } : {}),
    };
    if (r) {
      num(res, r);
      for (const k of ["units", "named", "duplicators"] as const) if (r[k]?.length) (res as any)[k] = [...((res as any)[k] ?? []), ...r[k]!];
      if (r.benchTransform) res.benchTransform = { slots: (res.benchTransform?.slots ?? 0) + r.benchTransform.slots };
      if (r.allOfCost && !res.allOfCost) res.allOfCost = r.allOfCost;
    }
    const c = fx.recurring;
    if (c) {
      for (const k of ["gold", "rerolls"] as const) if (c[k]) num(((rec as any)[k] ??= {}), c[k]);
      if (c.xp?.perStage) num((rec.xp ??= {}), { perStage: c.xp.perStage }); // xp.rounds was folded into the total above
      if (c.xp?.perCombat) num((rec.xp ??= {}), { perCombat: c.xp.perCombat });
      // Per-round copies of one champion: two such augments rarely give the same one, so the larger is kept.
      if (c.copies && (!rec.copies || (c.copies.perRound ?? 0) > (rec.copies.perRound ?? 0))) rec.copies = { ...c.copies };
    }
  }
  if (Object.keys(res).length) out.resources = res;
  if (Object.keys(rec).length) out.recurring = rec;
  return out;
}

// The picks' own score is the same for every candidate in a ranking: kept per board object, stage and picks.
const picksCache = new WeakMap<Board, Map<string, RawScore>>();
function picksAlone(pickFx: AugmentEffects[], picks: string[], board: Board, data: SetData, stage: string, mode: BoardMode): RawScore {
  const key = `${stage}|${mode}|${data.version}|${picks.join(",")}`;
  let m = picksCache.get(board);
  if (!m) picksCache.set(board, (m = new Map()));
  let r = m.get(key);
  if (!r) m.set(key, (r = scoreIn(mergeEffects(pickFx), board, data, stage, mode)));
  return r;
}

/**
 * How the planner's board is read: "now" is the board the player has (its stars and items are already there), "goal"
 * the board they're building towards (each stage is priced from the typical position by then, boardAtStage).
 */
export type BoardMode = "now" | "goal";

function scoreIn(fx: AugmentEffects, board: Board, data: SetData, stage: string, mode: BoardMode): RawScore {
  return mode === "goal" ? rawScore(fx, boardAtStage(board, stage, data), data, stage, board) : rawScore(fx, board, data, stage);
}

/**
 * How much of what the board still wants at `stage` the picks cover: copies (of the units that still need some) and
 * item slots (the best ones items count for). Lets the panel say why an earlier pick does or doesn't change the
 * ranking: a pick that leaves most of the need open leaves the next augment's value as it was.
 */
export function picksCoverage(given: string[], board: Board, data: SetData, stage: string, mode: BoardMode = "now", effects: Record<string, AugmentEffects> = EFFECTS) {
  const at = mode === "goal" ? boardAtStage(board, stage, data) : board;
  const goal = mode === "goal" ? board : undefined;
  const fx = mergeEffects(given.map((id) => effects[id]).filter((e) => e && e.confidence !== "none"));
  const plan = buildPlan(at, data, goal);
  const out = { copiesLeft: 0, copiesWanted: 0 };
  resourceParts(fx.resources ?? {}, fx.recurring, stage, plan, at, data, goal, out);
  return {
    /** Copies still wanted after the player's own rolling. */
    copiesWanted: out.copiesWanted,
    copiesCovered: out.copiesWanted - out.copiesLeft,
    itemSlots: itemSlots(at, data, goal).length,
    items: fx.resources ? itemEquivalents(fx.resources, fx.recurring, at, data, goal ?? at) : 0,
  };
}

/**
 * Scores one augment for the board, as if picked at `stage` ("2-1", "3-2" or "4-2"). `effects` is overridable for
 * tests. `given` are augments already picked (earlier stages): the score is then what the augment adds on top of
 * them, the model's value of the picks plus this one minus its value of the picks alone.
 */
export function scoreAugment(
  aug: Pick<SetAugment, "apiName" | "name" | "tier">,
  board: Board,
  data: SetData,
  stage: string,
  effects: Record<string, AugmentEffects> = EFFECTS,
  given: string[] = [],
  mode: BoardMode = "now",
): AugmentScore {
  const base: AugmentScore = { apiName: aug.apiName, name: aug.name, tier: aug.tier, dp: null, impact: null, statLift: 0, confidence: "none", parts: [], caveats: [] };
  const fx = effects[aug.apiName];
  const placed = board.units.filter((u) => data.units.some((d) => d.apiName === u.id));
  if (!fx || fx.confidence === "none" || placed.length === 0) {
    base.caveats.push(placed.length === 0 ? "put units on the board first" : "this augment's effect isn't modelled");
    if (fx?.unparsed?.length) base.caveats.push(...fx.unparsed.map((t) => `not read: ${t}`));
    return base;
  }

  const alone = scoreIn(fx, board, data, stage, mode);
  const { parts, caveats } = alone;
  // Diminishing returns: a board can only place so much better. Totals are squeezed towards a cap (tanh, so the order
  // is kept); with earlier picks the cap holds for all of them together, growing with their number, and an augment is
  // worth what it adds to the squeezed total: after a strong pick, the next one has less room left.
  const squeeze = (x: number, k: number) => {
    const c = ASSUMPTIONS.softCap * k ** ASSUMPTIONS.combinedCapGrowth;
    return x > 0 ? c * Math.tanh(x / c) : x;
  };
  let dp = squeeze(alone.raw, 1);
  let statLift = alone.statLift;
  let liftShare = 1;
  let aloneDp: number | undefined;
  if (alone.raw > 0 && dp < 0.9 * alone.raw) caveats.push("a very large total is squeezed (diminishing returns)");

  // On top of earlier picks: what they and this one are worth together, minus the picks alone. Picks built around a
  // trait the board doesn't play add almost nothing, so they're left out; so is a candidate like that (its value is
  // mostly what sells).
  const picks = given.filter((id) => id !== aug.apiName && effects[id] && effects[id].confidence !== "none" && traitPlayed(effects[id], board, data));
  if (picks.length && traitPlayed(fx, board, data)) {
    const pickFx = picks.map((id) => effects[id]);
    const without = picksAlone(pickFx, picks, board, data, stage, mode);
    const together = scoreIn(mergeEffects([...pickFx, fx]), board, data, stage, mode);
    aloneDp = dp;
    // The room left under the cap for all the picks, but never more than what it adds counts as a single augment (the
    // larger shared cap mustn't make an augment worth more after a weak pick than it is alone).
    const k = picks.length + 1;
    dp = Math.min(squeeze(together.raw, k) - squeeze(without.raw, k), squeeze(together.raw - without.raw, 1));
    statLift = together.statLift - without.statLift;
    if (Math.abs(alone.raw) > 1e-9) liftShare = (together.raw - without.raw) / alone.raw;
    if (Math.abs(aloneDp) > 1e-9 && Math.abs(dp / aloneDp - 1) >= 0.05) {
      const names = picks.map((id) => data.augments.find((a) => a.apiName === id)?.name ?? id).join(", ");
      caveats.unshift(`with ${picks.length > 1 ? "your earlier picks" : "your earlier pick"} (${names}): worth ${Math.round((dp / aloneDp) * 100)}% of what it adds alone`);
    }
  }
  // The breakdown scaled to the shown total, so it still adds up.
  const scale = Math.abs(alone.raw) > 1e-9 ? dp / alone.raw : 0;
  for (const p of parts) {
    p.dp *= scale;
    if (p.lift !== undefined) p.lift *= liftShare;
  }
  return { ...base, dp, impact: dp, statLift, confidence: alone.confidence, parts, caveats, ...(aloneDp !== undefined ? { alone: aloneDp } : {}) };
}

function describeEffect(e: StatEffect): string {
  const v = e.pct !== undefined ? `${+(e.pct * 100).toFixed(1)}%` : `${e.flat}${e.plusPerLevel ? ` (+${e.plusPerLevel} per level)` : ""}`;
  const stat = { health: "health", armor: "armor", mr: "magic resist", ad: "attack damage", ap: "ability power", as: "attack speed", crit: "crit chance", critDmg: "crit damage", damageAmp: "damage amp", durability: "durability", omnivamp: "omnivamp", abilityCrit: "ability damage can crit", manaRegen: "mana regen" }[e.stat];
  const who = { team: "team", holders: e.heldKind === "emblem" ? "emblem holders" : "item holders", unheld: "unequipped units", front: "front row", back: "back row", cost: `${e.cost}-cost units`, trait: `${e.trait} units`, unknown: "some units" }[e.scope];
  if (e.stat === "abilityCrit") return `${stat} (${who})`;
  return `+${v} ${stat} (${who}${e.per ? `, per ${e.per}` : ""})`;
}

/** Scores the augments, best first; those that couldn't be scored come last. */
export function rankAugments(
  augments: SetAugment[],
  board: Board,
  data: SetData,
  stage: string,
  effects: Record<string, AugmentEffects> = EFFECTS,
  given: string[] = [],
  mode: BoardMode = "now",
): AugmentScore[] {
  return augments
    .map((a) => scoreAugment(a, board, data, stage, effects, given, mode))
    .sort((a, b) => (b.impact ?? -Infinity) - (a.impact ?? -Infinity) || a.name.localeCompare(b.name));
}
