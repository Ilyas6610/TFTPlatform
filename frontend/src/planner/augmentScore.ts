// Augment impact estimate (prototype).
//
// Question: given the board in the planner, how much would each augment add?
// Neither Set 16, 17 nor 18's match data records augments, so nothing here is
// learned from results. It is arithmetic on the set data, with every
// assumption written down in ASSUMPTIONS, and it reports a breakdown instead of
// a bare score.
//
// How it works:
//  1. Board power. Each unit gets effective HP (HP scaled by resists) and
//     damage per second (auto-attacks plus an ability share) from its base
//     stats, star level and a flat bonus per held item. Team strength is the
//     geometric mean of total effective HP and total DPS.
//  2. Stat augments change the units they apply to (team, item holders, front
//     row...) and power is recomputed; the lift is the gain.
//  3. Everything else (gold, XP, rerolls, components, champions...) is priced
//     in gold-equivalents (GE) with the table below.
//  4. Both are put on one scale: GE, and "% of the board's value", where the
//     board's value is its units' gold cost plus its items. A stat lift of 10%
//     is worth 10% of the board.
//
// Effects come from augmentEffects.data.json, extracted from the augments'
// descriptions by scripts/extract-augment-effects.mjs. An augment it couldn't
// read is "not scored", and one it only partly read has lower confidence.

import { SetAugment, SetData, SetUnit } from "../api/client";
import raw from "./augmentEffects.data.json";
import { Board, COLS, PlacedUnit, ROWS, computeTraits } from "./board";

export type Confidence = "high" | "medium" | "low" | "none";

export interface StatEffect {
  kind: "stat";
  scope: "team" | "holders" | "unheld" | "front" | "back" | "cost" | "unknown";
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
  units?: { n: number; cost: number; star: number; fixed?: boolean; name?: string }[];
  named?: { name: string; n: number; kind: string }[];
}

export interface Recurring {
  gold?: { perRound?: number; perStage?: number };
  xp?: { perStage?: number; rounds?: number };
  rerolls?: { perRound?: number; perStage?: number };
  components?: { rounds?: number };
}

export interface AugmentEffects {
  effects?: StatEffect[];
  resources?: Resources;
  recurring?: Recurring;
  confidence: Confidence;
  unparsed?: string[];
  /** Rewards that depend on something happening; counted at half. */
  conditional?: string[];
}

export const EFFECTS = raw.augments as unknown as Record<string, AugmentEffects>;

/** Every number the estimate rests on. Change one and the ranking moves. */
export const ASSUMPTIONS = {
  /** TFT star scaling for health and attack damage. */
  starMultiplier: [1, 1.8, 3.24],
  /** Each held item adds this much to health, to attack damage and to ability power (a generic item). */
  itemHealth: 0.12,
  itemDamage: 0.15,
  itemAbility: 0.15,
  /** Share of a unit's damage that comes from its ability (the rest is auto-attacks). */
  abilityShare: 0.5,
  /** How much of attack speed's gain reaches ability damage (faster mana generation). */
  abilityFromAttackSpeed: 0.5,
  /** Length of a fight that bonuses are averaged over. */
  fightSeconds: 30,
  /** Healing from omnivamp, as a share of damage dealt that counts as extra health. */
  omnivampValue: 0.8,
  /** What an unclear "who gets this" bonus is assumed to cover. */
  unknownScopeCoverage: 0.5,
  /** Items on the one champion an augment grants, when it scales per item on that champion. */
  itemsOnOneUnit: 2,
  /** Allies sharing a trait with a unit, when an augment scales per such ally (average guess). */
  sharedTraitAllies: 2,
  /** Gold-equivalent (GE) prices. A unit costs its cost x 1, 3 or 9 (copies for a 1, 2 or 3 star). */
  ge: { gold: 1, xp: 1, reroll: 1.6, component: 3.5, completed: 8, artifact: 10, emblem: 7, itemOnBoard: 8 },
  /** How much of a resource turns into board value: spent well, wasted, benched... */
  usefulness: { gold: 0.75, xp: 0.75, reroll: 0.75, item: 0.9, unit: 0.6 },
  /** Stages left after the stage an augment is picked at, and rounds per stage, for recurring gains. */
  stagesLeft: { "2-1": 5, "3-2": 4, "4-2": 3 } as Record<string, number>,
  roundsPerStage: 6,
  /**
   * How much more value a board can take. A level-L board holds about L units
   * worth this much each (a 2-star 4-cost is 12); resources beyond the room
   * left up to that, and the item slots still free, are worth less and less.
   */
  targetValuePerSlot: 13,
  /** Impact is a share of the board's value, but of at least this share of a full board's, so a one-unit board doesn't read as +700%. */
  impactFloor: 0.6,
  roomFloor: 8,
  /** Carries that can use items, and items each can hold. */
  itemCarries: 3,
  itemsPerCarry: 3,
  /** XP matters less the higher the level (level 9 and 10 have little left to buy). */
  xpUsefulByLevel: { 6: 1, 7: 1, 8: 0.6, 9: 0.2, 10: 0 } as Record<number, number>,
  /** Gold early is worth more (interest, earlier levels): +this per stage left. */
  earlyBonusPerStage: 0.06,
};

export interface Part {
  label: string;
  /** Which of the board's limits applies to it: units/economy, items, or none (a stat bonus). */
  side?: "units" | "items";
  /** Gold-equivalents this part adds. */
  ge: number;
  /** For stat parts: the lift in team power, as a fraction. */
  lift?: number;
  note?: string;
}

export interface AugmentScore {
  apiName: string;
  name: string;
  tier: number;
  /** Total gold-equivalents; null when the augment couldn't be read. */
  ge: number | null;
  /** ge as a fraction of the board's value; null when not scored. */
  impact: number | null;
  /** Team power lift from the stat effects alone. */
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

const STAGE_LABELS = ["2-1", "3-2", "4-2"];

function stageFactors(stage: string) {
  const A = ASSUMPTIONS;
  const stages = A.stagesLeft[stage] ?? 4;
  return { stages, rounds: stages * A.roundsPerStage, early: 1 + A.earlyBonusPerStage * stages };
}

function itemGE(kind: string): number {
  const { ge } = ASSUMPTIONS;
  switch (kind) {
    case "component":
      return ge.component;
    case "emblem":
    case "radiant":
      return ge.emblem;
    case "artifact":
      return ge.artifact;
    default:
      return ge.completed;
  }
}

export interface Room {
  /** Gold-equivalents of units/upgrades the board can still take at its level. */
  units: number;
  /** Gold-equivalents of items the carries can still hold. */
  items: number;
  /** Share of XP that is still worth something at this level. */
  xp: number;
}

/** What the board can still use: the same gold is worth more to a board with room for it. */
export function boardRoom(board: Board, data: SetData): Room {
  const A = ASSUMPTIONS;
  const value = boardValue(board, data);
  const target = board.level * A.targetValuePerSlot;
  const held = board.units.reduce((n, u) => n + u.items.length, 0);
  const slots = Math.max(0, A.itemCarries * A.itemsPerCarry - held);
  const xpLevel = Math.max(6, Math.min(10, board.level));
  return {
    units: Math.max(A.roomFloor, target - value),
    items: Math.max(0, slots) * A.ge.completed,
    xp: A.xpUsefulByLevel[xpLevel] ?? 1,
  };
}

/** Value of `ge` to something that can only take `room` more: close to ge when there is plenty of room, capped by the room when there isn't. */
const saturate = (ge: number, room: number) => (ge <= 0 ? 0 : room <= 0 ? 0 : room * (1 - Math.exp(-ge / room)));

function resourceParts(res: Resources, rec: Recurring | undefined, stage: string, room: Room): Part[] {
  const { ge, usefulness: use } = ASSUMPTIONS;
  const f = stageFactors(stage);
  const parts: Part[] = [];
  const add = (label: string, value: number, note?: string, side: Part["side"] = "units") => {
    if (value !== 0) parts.push({ label, ge: value, note, side });
  };

  if (res.gold) add(`${res.gold} gold`, res.gold * ge.gold * use.gold * f.early, "early gold compounds (interest, earlier levels)");
  if (rec?.gold?.perRound) add(`${rec.gold.perRound} gold a round`, rec.gold.perRound * f.rounds * use.gold, `${f.rounds} rounds left`);
  if (rec?.gold?.perStage) add(`${rec.gold.perStage} gold a stage`, rec.gold.perStage * f.stages * use.gold, `${f.stages} stages left`);
  if (res.xp) add(`${res.xp} XP`, res.xp * ge.xp * use.xp * f.early * room.xp, room.xp < 1 ? "XP is worth less at this level" : undefined);
  if (rec?.xp?.perStage) add(`${rec.xp.perStage} XP a stage`, rec.xp.perStage * f.stages * use.xp * room.xp);
  if (rec?.xp?.rounds && res.xp) add(`${res.xp} XP for ${rec.xp.rounds} more rounds`, res.xp * rec.xp.rounds * use.xp * room.xp);
  if (res.rerolls) add(`${res.rerolls} rerolls`, res.rerolls * ge.reroll * use.reroll);
  if (rec?.rerolls?.perRound) add(`${rec.rerolls.perRound} reroll a round`, rec.rerolls.perRound * f.rounds * ge.reroll * use.reroll * 0.5, "half of the rounds you'd use them");
  if (rec?.rerolls?.perStage) add(`${rec.rerolls.perStage} rerolls a stage`, rec.rerolls.perStage * f.stages * ge.reroll * use.reroll);
  if (res.components) add(`${fmt(res.components)} components`, res.components * ge.component * use.item, undefined, "items");
  if (rec?.components?.rounds && res.components) add(`components for ${rec.components.rounds} more rounds`, res.components * rec.components.rounds * ge.component * use.item, undefined, "items");
  if (res.completed) add(`${fmt(res.completed)} completed items`, res.completed * ge.completed * use.item, undefined, "items");
  if (res.artifacts) add(`${fmt(res.artifacts)} artifacts`, res.artifacts * ge.artifact * use.item, undefined, "items");
  if (res.emblems) add(`${res.emblems} emblems`, res.emblems * ge.emblem * use.item, undefined, "items");
  for (const n of res.named ?? []) add(`${n.n} × ${n.name}`, n.n * itemGE(n.kind) * use.item, undefined, "items");
  for (const u of res.units ?? []) {
    const label = u.name ? u.name : `${u.n} × ${u.cost}-cost`;
    add(`${label}${u.star > 1 ? ` (${u.star}★)` : ""}`, u.n * u.cost * 3 ** (u.star - 1) * use.unit);
  }
  return parts;
}

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));

// -------------------------------------------------------------------- score

const LEVELS: Confidence[] = ["none", "low", "medium", "high"];
/** One notch less confident, but never "none": the augment is still scored. */
const lower = (c: Confidence): Confidence => LEVELS[Math.max(1, LEVELS.indexOf(c) - 1)];

/** A board's value in gold-equivalents: its units' gold cost plus its items. */
export function boardValue(board: Board, data: SetData): number {
  const cost = new Map(data.units.map((u) => [u.apiName, u.cost]));
  const units = board.units.reduce((sum, u) => sum + (cost.get(u.id) ?? 0) * 3 ** (u.star - 1), 0);
  const items = board.units.reduce((sum, u) => sum + u.items.length, 0);
  return units + items * ASSUMPTIONS.ge.itemOnBoard;
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
  const base: AugmentScore = {
    apiName: aug.apiName,
    name: aug.name,
    tier: aug.tier,
    ge: null,
    impact: null,
    statLift: 0,
    confidence: "none",
    parts: [],
    caveats: [],
  };
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
  const value = boardValue(board, data);

  // Stat effects: recompute power with every effect applied to the units it covers.
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
    // Unclear scope: take only part of the effect (re-run with the coverage as a damping on the lift).
    const full = teamPower(after);
    statLift = before > 0 ? full / before - 1 : 0;
    if (assumedScope) {
      statLift *= ASSUMPTIONS.unknownScopeCoverage;
      caveats.push("who receives part of the bonus isn't clear from the text; counted at half");
      confidence = lower(confidence);
    }
    if (fx.effects.some((e) => e.per === "uniqueOther" || e.per === "trait")) caveats.push("scales per ally or unit; an average is assumed");
    if (fx.effects.some((e) => e.everySeconds || e.ramp || e.delay)) caveats.push(`timed bonus, averaged over a ${ASSUMPTIONS.fightSeconds}s fight`);
    if (Math.abs(statLift) > 1e-6) {
      parts.push({ label: fx.effects.map(describeEffect).join(", "), ge: statLift * value, lift: statLift, note: `${(statLift * 100).toFixed(1)}% team power` });
    }
  }

  if (fx.resources || fx.recurring) {
    const room = boardRoom(board, data);
    const rp = resourceParts(fx.resources ?? {}, fx.recurring, stage, room);
    parts.push(...rp);
    // A board can only use so much: limit each side to the room it has left and show the difference.
    for (const [side, roomGE] of [["units", room.units], ["items", room.items]] as const) {
      const sum = rp.filter((p) => p.side === side).reduce((a, p) => a + p.ge, 0);
      if (sum <= 0) continue;
      const usable = saturate(sum, roomGE);
      if (sum - usable > 0.05 * sum) {
        parts.push({
          label: side === "items" ? "less: your carries have few free item slots" : "less: this board has little room left to use it",
          ge: usable - sum,
          note: `usable ${usable.toFixed(1)} of ${sum.toFixed(1)}`,
        });
        caveats.push(side === "items" ? "limited by the item slots still free on the board" : "limited by how much more value this board can take at its level");
      }
    }
  }

  if (fx.unparsed?.length) {
    caveats.push(...fx.unparsed.map((t) => `not counted: ${t}`));
  }
  if (fx.conditional?.length) caveats.push(...fx.conditional.map((t) => `counted at half, it depends on something happening: ${t}`));
  const ge = parts.reduce((a, p) => a + p.ge, 0);
  return {
    ...base,
    ge,
    impact: value > 0 ? ge / Math.max(value, ASSUMPTIONS.impactFloor * board.level * ASSUMPTIONS.targetValuePerSlot) : null,
    statLift,
    confidence: parts.length === 0 ? "none" : confidence,
    parts,
    caveats,
  };
}

function describeEffect(e: StatEffect): string {
  const v = e.pct !== undefined ? `${+(e.pct * 100).toFixed(1)}%` : `${e.flat}${e.plusPerLevel ? ` (+${e.plusPerLevel} per level)` : ""}`;
  const stat = { health: "health", armor: "armor", mr: "magic resist", ad: "attack damage", ap: "ability power", as: "attack speed", crit: "crit chance", critDmg: "crit damage", damageAmp: "damage amp", durability: "durability", omnivamp: "omnivamp" }[e.stat];
  const who = { team: "team", holders: "item holders", unheld: "unequipped units", front: "front row", back: "back row", cost: `${e.cost}-cost units`, unknown: "some units" }[e.scope];
  return `+${v} ${stat} (${who}${e.per ? `, per ${e.per}` : ""})`;
}

/** Scores the augments, best first; those that couldn't be scored come last. */
export function rankAugments(augments: SetAugment[], board: Board, data: SetData, stage: string, effects: Record<string, AugmentEffects> = EFFECTS): AugmentScore[] {
  return augments
    .map((a) => scoreAugment(a, board, data, stage, effects))
    .sort((a, b) => (b.impact ?? -Infinity) - (a.impact ?? -Infinity) || a.name.localeCompare(b.name));
}

export { STAGE_LABELS };
