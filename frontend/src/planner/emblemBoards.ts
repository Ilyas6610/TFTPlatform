// Boards built around the emblems a player holds (several at once after The Trait Tree, Branching Out or emblem
// drops): for a level, which units make the most of them. Stored games rarely hold the same two or three emblems at
// once, so the boards are constructed rather than looked up: a beam search adds one unit at a time, scoring a board by
// its active traits (an emblem adds its trait to a unit that lacks it) and by how strong its units are at that level.

import { SetData, SetTrait } from "../api/client";
import { Board, COLS, TraitCount, buildFormIndex, buildSlotRules, computeTraits } from "./board";

export const EMBLEM_BOARD = {
  /** Value of an active trait tier by its style (1 bronze, 3 silver, 5 gold, 6 prismatic; 4 is a one-unit trait). */
  styleValue: { 1: 0.6, 3: 2, 4: 0.3, 5: 4, 6: 6 } as Record<number, number>,
  /** The traits the emblems add to are the point of the board: their tiers count this many times over. */
  emblemTraitFactor: 2.5,
  /** An emblem whose trait ends up inactive (or that finds no holder) is wasted. */
  emblemWasted: 1.5,
  /** Value per cost of a unit the shop offers often enough at the level (see unitValue). */
  unitPerCost: 0.35,
  /** A cost the shop offers at least this often at the level counts fully; rarer ones in proportion. */
  fullOdds: 15,
  /**
   * Lux comes in one copy per trait (Lux (Fae), Lux (Elderwood)...), so the Lux with the trait a board wants is far
   * rarer than another 5-cost: the shop offers that one form about 1/forms as often. (Once one is held, the shop's
   * other Avatars copy her trait, so the rest comes easier.) Planning on one form costs this much × (1 − 1/forms); the
   * bare Lux, whatever trait she brings, has no such cost.
   */
  formLuckPenalty: 4,
  /** While searching: share of the next tier's value credited for progress towards it. */
  progressShare: 0.5,
  beam: 40,
  /** Boards returned per level, and how much two may overlap (Jaccard of their units). */
  boards: 4,
  maxOverlap: 0.6,
};

/** Standard shop odds (percent) by player level, index = cost - 1. */
const SHOP_ODDS: Record<number, number[]> = {
  3: [75, 25, 0, 0, 0],
  4: [55, 30, 15, 0, 0],
  5: [45, 33, 20, 2, 0],
  6: [30, 40, 25, 5, 0],
  7: [19, 30, 35, 15, 1],
  8: [16, 20, 35, 25, 4],
  9: [9, 15, 30, 30, 16],
  10: [5, 10, 20, 40, 25],
};
const odds = (level: number) => SHOP_ODDS[Math.max(3, Math.min(10, level))];

interface Candidate {
  id: string;
  cost: number;
  /** Base name: Lux's forms share one, so a board fields one Lux. */
  base: string;
  traits: { name: string; weight: number }[];
  slots: number;
  value: number;
}

export interface EmblemBoard {
  board: Board;
  /** Which unit holds each emblem (an emblem without a holder is left out). */
  holders: { emblem: string; unit: string }[];
  traits: TraitCount[];
  value: number;
}

function candidates(data: SetData, level: number): Candidate[] {
  const forms = buildFormIndex(data.units);
  const slotRules = buildSlotRules(data);
  const o = odds(level);
  const out: Candidate[] = [];
  for (const u of data.units) {
    if (u.cost < 1 || u.cost > 5 || u.traits.length === 0) continue;
    // Lux: each trait form is a separate choice, with the luck of rolling that trait; the bare unit counts as a 5-cost
    // whose trait isn't planned on.
    const chosen = forms.chosen.get(u.apiName);
    const base = forms.baseOf.get(u.apiName);
    const formCount = base ? (forms.forms.get(base)?.length ?? 1) : 0;
    const rule = slotRules.get(u.apiName);
    const avail = Math.min(1, (o[u.cost - 1] ?? 0) / EMBLEM_BOARD.fullOdds);
    out.push({
      id: u.apiName,
      cost: u.cost,
      base: forms.baseOf.get(u.apiName) ?? u.apiName,
      traits: u.traits.map((t) => ({ name: t, weight: t === chosen ? 2 : t === rule?.trait ? (rule.traitCount ?? 1) : 1 })),
      slots: rule?.slots ?? 1,
      value:
        EMBLEM_BOARD.unitPerCost * u.cost * avail -
        (formCount > 1 ? EMBLEM_BOARD.formLuckPenalty * (1 - 1 / formCount) : 0),
    });
  }
  return out;
}

/** Breakpoints (unit counts) and their styles, per trait name. */
function breakpoints(data: SetData): Map<string, { at: number[]; style: number[] }> {
  const m = new Map<string, { at: number[]; style: number[] }>();
  for (const t of data.traits as SetTrait[]) {
    const bps = t.breakpoints.filter((b) => b.minUnits > 0);
    m.set(t.name, { at: bps.map((b) => b.minUnits), style: bps.map((b) => b.style ?? 1) });
  }
  return m;
}

/**
 * Emblem holders for a set of units: the strongest unit without the trait. A unit may hold emblems of different
 * traits, but copies of one emblem need different holders (a unit counts once for a trait).
 */
function holdersFor(units: Candidate[], emblemTraits: string[]): (Candidate | null)[] {
  const byStrength = [...units].sort((a, b) => b.cost - a.cost);
  const used = new Map<string, Set<string>>(); // trait -> holders so far
  return emblemTraits.map((t) => {
    const taken = used.get(t) ?? new Set<string>();
    const h = byStrength.find((u) => !taken.has(u.id) && !u.traits.some((x) => x.name === t)) ?? null;
    if (h) used.set(t, taken.add(h.id));
    return h;
  });
}

function score(units: Candidate[], emblemTraits: string[], bp: ReturnType<typeof breakpoints>, searching: boolean): number {
  const counts = new Map<string, number>();
  for (const u of units) for (const t of u.traits) counts.set(t.name, (counts.get(t.name) ?? 0) + t.weight);
  let value = units.reduce((a, u) => a + u.value, 0);
  const hs = holdersFor(units, emblemTraits);
  hs.forEach((h, i) => {
    if (h) counts.set(emblemTraits[i], (counts.get(emblemTraits[i]) ?? 0) + 1);
  });
  const emblemSet = new Set(emblemTraits);
  for (const [name, n] of counts) {
    const b = bp.get(name);
    if (!b) continue;
    let tier = -1;
    b.at.forEach((at, i) => n >= at && (tier = i));
    const factor = emblemSet.has(name) ? EMBLEM_BOARD.emblemTraitFactor : 1;
    if (tier >= 0) value += factor * (EMBLEM_BOARD.styleValue[b.style[tier]] ?? 1);
    // While the board is still being built, partial progress towards the next tier counts a little, so the search
    // doesn't just take the most expensive units first.
    if (searching && tier + 1 < b.at.length) {
      const next = b.at[tier + 1];
      const from = tier >= 0 ? b.at[tier] : 0;
      value += factor * EMBLEM_BOARD.progressShare * ((n - from) / (next - from)) * (EMBLEM_BOARD.styleValue[b.style[tier + 1]] ?? 1) * (tier >= 0 ? 0.5 : 1);
    }
  }
  // A finished board pays for every emblem left without a holder or on an inactive trait.
  if (!searching) {
    emblemTraits.forEach((t, i) => {
      const b = bp.get(t);
      const active = !!b && (counts.get(t) ?? 0) >= (b.at[0] ?? Infinity);
      if (!hs[i] || !active) value -= EMBLEM_BOARD.emblemWasted;
    });
  }
  return value;
}

/** A planner board: melee units in front, the rest behind; each emblem on its holder. */
function toBoard(units: Candidate[], emblems: string[], emblemTraits: string[], level: number, data: SetData): { board: Board; holders: EmblemBoard["holders"] } {
  const defs = new Map(data.units.map((u) => [u.apiName, u]));
  const hs = holdersFor(units, emblemTraits);
  let front = 0;
  let back = 3 * COLS;
  let mid = 2 * COLS;
  const placed = units.map((u) => {
    const melee = (defs.get(u.id)?.stats.range ?? 1) <= 1;
    const pos = melee && front < COLS ? front++ : back < 4 * COLS ? back++ : mid++;
    const items = emblems.filter((_, i) => hs[i]?.id === u.id).slice(0, 3);
    return { id: u.id, star: (u.cost >= 4 ? 1 : 2) as 1 | 2 | 3, pos, items, extra: [] as string[] };
  });
  return {
    board: { set: data.setNumber, title: "", level, augments: [null, null, null], units: placed },
    holders: emblems.flatMap((e, i) => (hs[i] ? [{ emblem: e, unit: hs[i]!.id }] : [])),
  };
}

/**
 * The best boards of `level` slots for the held `emblems` (apiNames, repeats allowed: two of one emblem count twice),
 * most valuable first, each different enough from the ones before it.
 */
export function suggestEmblemBoards(emblems: string[], level: number, data: SetData): EmblemBoard[] {
  const itemByApi = new Map(data.items.map((i) => [i.apiName, i]));
  const emblemTraits = emblems.map((e) => itemByApi.get(e)?.name.replace(/ Emblem$/, "") ?? "");
  const bp = breakpoints(data);
  const pool = candidates(data, level);
  type State = { units: Candidate[]; slots: number; key: string; v: number };
  let beam: State[] = [{ units: [], slots: 0, key: "", v: 0 }];
  const done: State[] = [];
  for (let step = 0; step < level && beam.length; step++) {
    const next = new Map<string, State>();
    for (const s of beam) {
      const bases = new Set(s.units.map((u) => u.base));
      for (const c of pool) {
        if (bases.has(c.base) || s.slots + c.slots > level) continue;
        const units = [...s.units, c].sort((a, b) => (a.id < b.id ? -1 : 1));
        const key = units.map((u) => u.id).join(",");
        if (next.has(key)) continue;
        const st = { units, slots: s.slots + c.slots, key, v: score(units, emblemTraits, bp, true) };
        next.set(key, st);
      }
    }
    const ranked = [...next.values()].sort((a, b) => b.v - a.v);
    beam = [];
    for (const s of ranked) {
      if (s.slots === level) done.push(s);
      else if (beam.length < EMBLEM_BOARD.beam) beam.push(s);
    }
  }
  // Final ranking without the search's progress credit; then keep boards different enough from those before.
  const finals = [...new Map(done.map((s) => [s.key, s])).values()]
    .map((s) => ({ ...s, v: score(s.units, emblemTraits, bp, false) }))
    .sort((a, b) => b.v - a.v);
  const out: EmblemBoard[] = [];
  const picked: Set<string>[] = [];
  for (const s of finals) {
    const ids = new Set(s.units.map((u) => u.id));
    if (picked.some((p) => [...ids].filter((x) => p.has(x)).length / new Set([...ids, ...p]).size > EMBLEM_BOARD.maxOverlap)) continue;
    picked.push(ids);
    const { board, holders } = toBoard(s.units, emblems, emblemTraits, level, data);
    out.push({ board, holders, traits: computeTraits(board, data).filter((t) => t.tier >= 0), value: s.v });
    if (out.length === EMBLEM_BOARD.boards) break;
  }
  return out;
}
