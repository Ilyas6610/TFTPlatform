// The board planner's model: what's on a board, how it is written into (and
// read back from) a page URL so a link shares the board, and the numbers
// derived from it (active traits, gold cost). No React in here.
//
// A link is untrusted input, so decoding validates everything: ids must look
// like Riot ids, positions and counts are bounded, and (once set data is
// known) unknown units, items and augments are dropped by `sanitize`.

import { SetAugment, SetData, SetItem, SetTrait, SetUnit } from "../api/client";
import { AUGMENT_SLOTS, availableAt } from "./augmentStages";

export const ROWS = 4;
export const COLS = 7;
export const MAX_ITEMS = 3;
export const AUGMENT_COUNT = AUGMENT_SLOTS.length;
export const MAX_LEVEL = 10;
export const DEFAULT_LEVEL = 8;
export const MAX_TITLE = 40;

export interface PlacedUnit {
  id: string;
  star: 1 | 2 | 3;
  /** 0 .. ROWS*COLS-1, row-major; row 0 is the front line. */
  pos: number;
  items: string[];
}

export interface Board {
  set: number;
  title: string;
  level: number;
  units: PlacedUnit[];
  /** One entry per augment slot (first, second, third), null when not chosen. */
  augments: (string | null)[];
}

export const emptyBoard = (set: number): Board => ({ set, title: "", level: DEFAULT_LEVEL, units: [], augments: Array(AUGMENT_COUNT).fill(null) });

export const cellRow = (pos: number) => Math.floor(pos / COLS);
export const cellCol = (pos: number) => pos % COLS;

const ID = /^[A-Za-z0-9_]{1,80}$/;

/** Control characters out, whitespace collapsed, length capped. */
export function cleanTitle(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/[\u0000-\u001f\u007f]/g, "").replace(/\s+/g, " ").trimStart().slice(0, MAX_TITLE);
}

const clampStar = (n: number): 1 | 2 | 3 => (n >= 3 ? 3 : n === 2 ? 2 : 1);

/**
 * The board as URL parameters:
 *   set=18  t=<title>  lv=8  a1= a2= a3= <augment> (first, second, third slot)
 *   u=<unitId>.<star>.<pos>[.<item>,<item>,<item>] (repeated)
 * Positions and ids use only characters that need no URL escaping.
 */
export function encodeBoard(b: Board): URLSearchParams {
  const q = new URLSearchParams();
  q.set("set", String(b.set));
  if (b.title) q.set("t", b.title);
  if (b.level !== DEFAULT_LEVEL) q.set("lv", String(b.level));
  b.augments.forEach((a, i) => a && q.set(`a${i + 1}`, a));
  for (const u of [...b.units].sort((x, y) => x.pos - y.pos))
    q.append("u", `${u.id}.${u.star}.${u.pos}${u.items.length ? "." + u.items.join(",") : ""}`);
  return q;
}

/** Reads a board from URL parameters, dropping anything malformed. */
export function decodeBoard(q: URLSearchParams, defaultSet: number): Board {
  const set = Number(q.get("set"));
  const b = emptyBoard(Number.isInteger(set) && set > 0 && set <= 100 ? set : defaultSet);
  b.title = cleanTitle(q.get("t") ?? "");
  const lv = Number(q.get("lv"));
  if (Number.isInteger(lv) && lv >= 1 && lv <= MAX_LEVEL) b.level = lv;

  // a1..a3 are the slots; plain repeated `a` (older links) fill the slots in order.
  const chosen = new Set<string>();
  const pick = (slot: number, a: string | null) => {
    if (a && ID.test(a) && !chosen.has(a) && b.augments[slot] === null) {
      b.augments[slot] = a;
      chosen.add(a);
    }
  };
  for (let i = 0; i < AUGMENT_COUNT; i++) pick(i, q.get(`a${i + 1}`));
  for (const a of q.getAll("a")) {
    const free = b.augments.indexOf(null);
    if (free >= 0) pick(free, a);
  }
  const taken = new Set<number>();
  for (const raw of q.getAll("u")) {
    const [id, star, pos, items = ""] = raw.split(".");
    const p = Number(pos);
    if (!id || !ID.test(id) || !Number.isInteger(p) || p < 0 || p >= ROWS * COLS || taken.has(p)) continue;
    const list = items
      .split(",")
      .filter((i) => ID.test(i))
      .slice(0, MAX_ITEMS);
    taken.add(p);
    b.units.push({ id, star: clampStar(Number(star)), pos: p, items: list });
  }
  return b;
}

/** What `sanitize` removed, for a notice. */
export interface Dropped {
  units: number;
  items: number;
  augments: number;
}

/** Removes units, items and augments the set data doesn't know. */
export function sanitize(b: Board, data: SetData): { board: Board; dropped: Dropped } {
  const units = new Set(data.units.map((u) => u.apiName));
  const items = new Set(data.items.map((i) => i.apiName));
  const augmentById = new Map(data.augments.map((a) => [a.apiName, a]));
  const dropped: Dropped = { units: 0, items: 0, augments: 0 };
  const board: Board = {
    ...b,
    units: b.units.flatMap((u) => {
      if (!units.has(u.id)) {
        dropped.units++;
        return [];
      }
      const kept = u.items.filter((i) => items.has(i));
      dropped.items += u.items.length - kept.length;
      return [{ ...u, items: kept }];
    }),
    // Unknown augments go, and so do ones the slot's stage can't offer.
    augments: b.augments.map((a, slot) => {
      if (a === null) return null;
      const aug = augmentById.get(a);
      if (aug && availableAt(aug, slot)) return a;
      dropped.augments++;
      return null;
    }),
  };
  return { board, dropped };
}

// ---- Edits (pure: each returns a new board) --------------------------------

/** The first empty cell, filling from the back line forward (new units tend to be carries). */
export function firstFreeCell(b: Board): number | null {
  const taken = new Set(b.units.map((u) => u.pos));
  for (let row = ROWS - 1; row >= 0; row--)
    for (let col = 0; col < COLS; col++) if (!taken.has(row * COLS + col)) return row * COLS + col;
  return null;
}

export function addUnit(b: Board, id: string, pos?: number): Board {
  const at = pos ?? firstFreeCell(b);
  if (at === null || at < 0 || at >= ROWS * COLS || b.units.some((u) => u.pos === at)) return b;
  return { ...b, units: [...b.units, { id, star: 1, pos: at, items: [] }] };
}

export const removeUnit = (b: Board, pos: number): Board => ({ ...b, units: b.units.filter((u) => u.pos !== pos) });

/** Moves the unit at `from` to `to`; swaps with whoever is there. */
export function moveUnit(b: Board, from: number, to: number): Board {
  if (from === to || to < 0 || to >= ROWS * COLS) return b;
  if (!b.units.some((u) => u.pos === from)) return b;
  return {
    ...b,
    units: b.units.map((u) => (u.pos === from ? { ...u, pos: to } : u.pos === to ? { ...u, pos: from } : u)),
  };
}

export const setStar = (b: Board, pos: number, star: 1 | 2 | 3): Board => ({
  ...b,
  units: b.units.map((u) => (u.pos === pos ? { ...u, star } : u)),
});

export function addItem(b: Board, pos: number, item: string): Board {
  return {
    ...b,
    units: b.units.map((u) => (u.pos === pos && u.items.length < MAX_ITEMS ? { ...u, items: [...u.items, item] } : u)),
  };
}

export const removeItem = (b: Board, pos: number, index: number): Board => ({
  ...b,
  units: b.units.map((u) => (u.pos === pos ? { ...u, items: u.items.filter((_, i) => i !== index) } : u)),
});

/** Puts an augment in a slot (null empties it). An augment sits in one slot only. */
export function setAugment(b: Board, slot: number, id: string | null): Board {
  if (slot < 0 || slot >= AUGMENT_COUNT) return b;
  const list = b.augments.map((a) => (id !== null && a === id ? null : a));
  list[slot] = id;
  return { ...b, augments: list };
}

// ---- Derived numbers --------------------------------------------------------

export interface TraitCount {
  /** Display name, as units list it. */
  name: string;
  apiName?: string;
  count: number;
  /** Index into the trait's breakpoints of the tier reached, -1 if none. */
  tier: number;
  /** Units needed for the next breakpoint, if any. */
  next?: number;
  style: number;
  trait?: SetTrait;
}

/**
 * Traits active on the board. Each distinct unit counts once however many
 * copies are fielded; an emblem adds its trait to the unit holding it, unless
 * the unit already has that trait.
 */
export function computeTraits(b: Board, data: SetData): TraitCount[] {
  const unitById = new Map<string, SetUnit>(data.units.map((u) => [u.apiName, u]));
  const traitByName = new Map<string, SetTrait>(data.traits.map((t) => [t.name, t]));
  const itemById = new Map<string, SetItem>(data.items.map((i) => [i.apiName, i]));

  const members = new Map<string, Set<string>>(); // trait name -> distinct unit ids (+ emblem holders)
  const add = (trait: string, unit: string) => {
    if (!members.has(trait)) members.set(trait, new Set());
    members.get(trait)!.add(unit);
  };
  for (const placed of b.units) {
    const unit = unitById.get(placed.id);
    if (!unit) continue;
    for (const t of unit.traits) add(t, placed.id);
    for (const it of placed.items) {
      const item = itemById.get(it);
      if (item?.kind !== "emblem") continue;
      const trait = item.name.replace(/ Emblem$/, "");
      // A unit holding two copies of the same emblem still counts once, and
      // not at all if it already has the trait.
      if (traitByName.has(trait) && !unit.traits.includes(trait)) add(trait, placed.id);
    }
  }

  const out: TraitCount[] = [];
  for (const [name, units] of members) {
    const trait = traitByName.get(name);
    const points = (trait?.breakpoints ?? []).map((bp) => bp.minUnits).filter((n) => n > 0);
    let tier = -1;
    points.forEach((n, i) => {
      if (units.size >= n) tier = i;
    });
    out.push({
      name,
      apiName: trait?.apiName,
      count: units.size,
      tier,
      next: points.find((n) => n > units.size),
      style: tier >= 0 ? (trait?.breakpoints.filter((bp) => bp.minUnits > 0)[tier]?.style ?? 1) : 0,
      trait,
    });
  }
  // Active traits first (higher style, then count), inactive after.
  return out.sort((a, b2) => (b2.tier >= 0 ? 1 : 0) - (a.tier >= 0 ? 1 : 0) || b2.style - a.style || b2.count - a.count || a.name.localeCompare(b2.name));
}

/** Gold to buy every unit: a 2★ is 3 copies, a 3★ is 9. */
export function boardCost(b: Board, data: SetData): number {
  const cost = new Map(data.units.map((u) => [u.apiName, u.cost]));
  return b.units.reduce((sum, u) => sum + (cost.get(u.id) ?? 0) * 3 ** (u.star - 1), 0);
}

/** Items a unit can hold in the planner (components are left out: they are only recipe parts). */
export const HOLDABLE_KINDS = new Set(["completed", "emblem", "artifact", "radiant"]);

export function holdableItems(data: SetData): SetItem[] {
  const order: Record<string, number> = { completed: 0, radiant: 1, artifact: 2, emblem: 3 };
  return data.items
    .filter((i) => HOLDABLE_KINDS.has(i.kind))
    .sort((a, b) => (order[a.kind] ?? 9) - (order[b.kind] ?? 9) || a.name.localeCompare(b.name));
}

export function augmentsByTier(data: SetData): SetAugment[] {
  return [...data.augments].sort((a, b) => a.tier - b.tier || a.name.localeCompare(b.name));
}
