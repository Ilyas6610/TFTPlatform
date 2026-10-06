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
/** The most evolved traits any unit can hold (Kha'Zix's four choices). */
export const MAX_EXTRA_TRAITS = 4;

export interface PlacedUnit {
  id: string;
  star: 1 | 2 | 3;
  /** 0 .. ROWS*COLS-1, row-major; row 0 is the front line. */
  pos: number;
  items: string[];
  /** Trait apiNames the unit gained by evolving (Kha'Zix); see buildTraitChoices. */
  extra: string[];
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
 *   u=<unitId>.<star>.<pos>[.<item>,<item>,<item>[.<trait>,<trait>]] (repeated;
 *     the item field stays empty when only evolved traits are given)
 * Positions and ids use only characters that need no URL escaping.
 */
export function encodeBoard(b: Board): URLSearchParams {
  const q = new URLSearchParams();
  q.set("set", String(b.set));
  if (b.title) q.set("t", b.title);
  if (b.level !== DEFAULT_LEVEL) q.set("lv", String(b.level));
  b.augments.forEach((a, i) => a && q.set(`a${i + 1}`, a));
  for (const u of [...b.units].sort((x, y) => x.pos - y.pos))
    q.append(
      "u",
      `${u.id}.${u.star}.${u.pos}` +
        (u.items.length || u.extra.length ? "." + u.items.join(",") : "") +
        (u.extra.length ? "." + u.extra.join(",") : ""),
    );
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
    const [id, star, pos, items = "", extra = ""] = raw.split(".");
    const p = Number(pos);
    if (!id || !ID.test(id) || !Number.isInteger(p) || p < 0 || p >= ROWS * COLS || taken.has(p)) continue;
    const list = items
      .split(",")
      .filter((i) => ID.test(i))
      .slice(0, MAX_ITEMS);
    const traits = [...new Set(extra.split(",").filter((t) => ID.test(t)))].slice(0, MAX_EXTRA_TRAITS);
    taken.add(p);
    b.units.push({ id, star: clampStar(Number(star)), pos: p, items: list, extra: traits });
  }
  return b;
}

/** "1 unit and 2 augments": only the non-zero parts, singular or plural. */
export function droppedSummary(d: Dropped): string {
  const parts = (
    [
      [d.units, "unit", "units"],
      [d.items, "item", "items"],
      [d.augments, "augment", "augments"],
      [d.traits, "trait", "traits"],
    ] as const
  )
    .filter(([n]) => n > 0)
    .map(([n, one, many]) => `${n} ${n === 1 ? one : many}`);
  if (parts.length <= 1) return parts[0] ?? "";
  return `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

/** What `sanitize` removed, for a notice. */
export interface Dropped {
  units: number;
  items: number;
  augments: number;
  /** Evolved traits the unit can't have. */
  traits: number;
}

/** Removes units, items and augments the set data doesn't know. */
export function sanitize(b: Board, data: SetData): { board: Board; dropped: Dropped } {
  const units = new Set(data.units.map((u) => u.apiName));
  // Only what the planner offers: components, consumables and the like can't be held.
  const items = new Set(data.items.filter((i) => HOLDABLE_KINDS.has(i.kind)).map((i) => i.apiName));
  const augmentById = new Map(data.augments.map((a) => [a.apiName, a]));
  const dropped: Dropped = { units: 0, items: 0, augments: 0, traits: 0 };
  const choices = buildTraitChoices(data);
  const board: Board = {
    ...b,
    units: b.units.flatMap((u) => {
      if (!units.has(u.id)) {
        dropped.units++;
        return [];
      }
      const kept = u.items.filter((i) => items.has(i));
      dropped.items += u.items.length - kept.length;
      const allowed = new Set(choices.get(u.id)?.traits.map((t) => t.id));
      const extra = u.extra.filter((t) => allowed.has(t));
      dropped.traits += u.extra.length - extra.length;
      return [{ ...u, items: kept, extra }];
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
  const formed = normalizeForms(board, buildFormIndex(data.units));
  const clean = dropRedundantEmblems(formed, data);
  dropped.items += clean.dropped;
  return { board: clean.board, dropped };
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
  return { ...b, units: [...b.units, { id, star: 1, pos: at, items: [], extra: [] }] };
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

/** What a click on a board cell does, given the cell that is currently selected. */
export interface CellClick {
  /** The selection after the click. */
  selected: number | null;
  /** Set when the selected unit should move to an empty cell. */
  move?: { from: number; to: number };
}

/**
 * Clicking a unit selects it (clicking the selected one again deselects it), so
 * other units stay choosable while one is selected. Clicking an empty cell
 * moves the selected unit there. Swapping two units is a drag, never a click.
 */
export function clickCell(b: Board, selected: number | null, pos: number): CellClick {
  const occupied = b.units.some((u) => u.pos === pos);
  if (occupied) return { selected: selected === pos ? null : pos };
  const hasSelection = selected !== null && b.units.some((u) => u.pos === selected);
  if (hasSelection) return { selected: pos, move: { from: selected as number, to: pos } };
  return { selected: null };
}

// ---- Units that take more than one team slot (Elder Dragon) --------------------

export interface SlotRule {
  /** Team slots the unit takes (the unit limit counts these, not units). */
  slots: number;
  /** A trait the unit counts extra for, and how many units it counts as there. */
  trait?: string;
  traitCount?: number;
}

/**
 * Reads, from trait text like "Elder Dragon takes up 2 team slots and grants
 * +2 to the Riftbeast trait.", which units take extra slots and what they
 * count for in a trait. Units are matched by name; others take one slot.
 */
export function buildSlotRules(data: Pick<SetData, "units" | "traits">): Map<string, SlotRule> {
  const unitByName = new Map(data.units.map((u) => [u.name, u]));
  const traitNames = new Set(data.traits.map((t) => t.name));
  const out = new Map<string, SlotRule>();
  const re = /([^.]+?) takes up (\d+) team slots?(?: and grants \+?(\d+) to the ([^.]+?) trait)?\./gi;
  for (const t of data.traits) {
    for (const text of [t.desc, ...t.breakpoints.map((bp) => bp.text ?? "")]) {
      for (const m of (text ?? "").matchAll(re)) {
        const unit = unitByName.get(m[1].trim());
        const slots = Number(m[2]);
        if (!unit || slots < 1) continue;
        const rule: SlotRule = { slots };
        if (m[3] && traitNames.has(m[4].trim())) {
          rule.trait = m[4].trim();
          rule.traitCount = Number(m[3]);
        }
        out.set(unit.apiName, rule);
      }
    }
  }
  return out;
}

/** Team slots the board uses: one per unit, more for units like Elder Dragon. */
export function slotsUsed(b: Board, data: SetData): { used: number; extra: { unit: string; slots: number }[] } {
  const rules = buildSlotRules(data);
  const name = new Map(data.units.map((u) => [u.apiName, u.name]));
  let used = 0;
  const extra: { unit: string; slots: number }[] = [];
  for (const u of b.units) {
    const slots = rules.get(u.id)?.slots ?? 1;
    used += slots;
    if (slots > 1) extra.push({ unit: name.get(u.id) ?? u.id, slots });
  }
  return { used, extra };
}

// ---- Units with a chosen trait (Lux) ----------------------------------------

/** One form of a unit: the apiName to field and the trait it was given. */
export interface UnitForm {
  id: string;
  trait: string;
}

export interface FormIndex {
  /** Base unit apiName -> its forms. */
  forms: Map<string, UnitForm[]>;
  /** Form apiName -> its base unit's apiName. */
  baseOf: Map<string, string>;
  /** Form apiName -> the trait it chose. */
  chosen: Map<string, string>;
}

/**
 * Some units come in one version per trait the player picks: Set 18's Lux is
 * "Lux" plus "Lux (Coven)", "Lux (Fae)"... Set data lists each as its own
 * unit; they are recognised by the name pattern "<base name> (<trait>)" where
 * the base exists, and the trait is one the form really has. The planner shows
 * only the base and offers the trait as a menu on it.
 */
export function buildFormIndex(units: SetUnit[]): FormIndex {
  const byName = new Map(units.map((u) => [u.name, u]));
  const idx: FormIndex = { forms: new Map(), baseOf: new Map(), chosen: new Map() };
  for (const u of units) {
    const m = /^(.+) \((.+)\)$/.exec(u.name);
    const base = m && byName.get(m[1]);
    if (!m || !base || base.apiName === u.apiName || !u.traits.includes(m[2])) continue;
    idx.baseOf.set(u.apiName, base.apiName);
    idx.chosen.set(u.apiName, m[2]);
    idx.forms.set(base.apiName, [...(idx.forms.get(base.apiName) ?? []), { id: u.apiName, trait: m[2] }]);
  }
  for (const list of idx.forms.values()) list.sort((a, b) => a.trait.localeCompare(b.trait));
  return idx;
}

/**
 * Sets the trait of a unit with forms (Lux) to form `id` (or its base for
 * "none"). Every copy of that unit on the board takes the same form: all
 * Avatars share the trait that was chosen.
 */
export function setForm(b: Board, pos: number, id: string, idx: FormIndex): Board {
  const at = b.units.find((u) => u.pos === pos);
  if (!at) return b;
  const base = idx.baseOf.get(at.id) ?? at.id;
  return { ...b, units: b.units.map((u) => ((idx.baseOf.get(u.id) ?? u.id) === base ? { ...u, id } : u)) };
}

/**
 * Makes all copies of a unit with forms agree: the first copy (front line
 * first) that has a trait chosen decides, so a Lux added to a board whose Lux
 * is Coven comes in as Coven too.
 */
export function normalizeForms(b: Board, idx: FormIndex): Board {
  if (idx.forms.size === 0) return b;
  const decided = new Map<string, string>(); // base -> form id
  for (const u of [...b.units].sort((x, y) => x.pos - y.pos)) {
    const base = idx.baseOf.get(u.id);
    if (base && !decided.has(base)) decided.set(base, u.id);
  }
  let changed = false;
  const units = b.units.map((u) => {
    const base = idx.baseOf.get(u.id) ?? (idx.forms.has(u.id) ? u.id : undefined);
    const want = base && decided.get(base);
    if (!want || want === u.id) return u;
    changed = true;
    return { ...u, id: want };
  });
  return changed ? { ...b, units } : b;
}

// ---- Units that gain traits by evolving (Kha'Zix) ---------------------------

export interface TraitChoice {
  /** Trait apiName, as stored in links. */
  id: string;
  name: string;
}

export interface TraitChoices {
  /** Most traits the unit can end up with from evolving. */
  max: number;
  traits: TraitChoice[];
}

/**
 * Reads, from trait text like "Takedowns evolve Kha'Zix, permanently granting
 * him your choice of Executioner, Rapidfire, Ravager, or Spellweaver.", which
 * unit picks up which extra traits. Only traits that exist in the set and that
 * the unit doesn't already have are kept.
 */
export function buildTraitChoices(data: Pick<SetData, "units" | "traits">): Map<string, TraitChoices> {
  const unitByName = new Map(data.units.map((u) => [u.name, u]));
  const traitByName = new Map(data.traits.map((t) => [t.name, t]));
  const out = new Map<string, TraitChoices>();
  const re = /evolve ([^,.]+?), permanently granting \w+ your choice of ([^.]+)\./g;
  for (const t of data.traits) {
    for (const text of [t.desc, ...t.breakpoints.map((bp) => bp.text ?? "")]) {
      for (const m of (text ?? "").matchAll(re)) {
        const unit = unitByName.get(m[1].trim());
        if (!unit) continue;
        const traits: TraitChoice[] = m[2]
          .split(/,\s*(?:or\s+)?|\s+or\s+/)
          .map((n) => traitByName.get(n.trim()))
          .filter((tr): tr is SetTrait => !!tr && !unit.traits.includes(tr.name))
          .map((tr) => ({ id: tr.apiName, name: tr.name }));
        if (traits.length > 0) out.set(unit.apiName, { max: Math.min(traits.length, MAX_EXTRA_TRAITS), traits });
      }
    }
  }
  return out;
}

/** Adds or removes an evolved trait on the unit at `pos`, up to the unit's maximum. */
export function toggleExtraTrait(b: Board, pos: number, trait: string, choices: TraitChoices): Board {
  const unit = b.units.find((u) => u.pos === pos);
  if (!unit || !choices.traits.some((t) => t.id === trait)) return b;
  const on = unit.extra.includes(trait);
  if (!on && unit.extra.length >= choices.max) return b;
  const extra = on ? unit.extra.filter((t) => t !== trait) : [...unit.extra, trait];
  return { ...b, units: b.units.map((u) => (u.pos === pos ? { ...u, extra } : u)) };
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
// ---- Item rules ---------------------------------------------------------------

/** The trait an emblem grants (its name minus " Emblem"), or null for any other item. */
export function emblemTrait(item: SetItem | undefined): string | null {
  return item?.kind === "emblem" ? item.name.replace(/ Emblem$/, "") : null;
}

/**
 * Traits a placed unit already has: its own, any it gained by evolving, and
 * those of the emblems it holds (index `except` skipped, for re-checking one).
 */
function heldTraitNames(u: PlacedUnit, data: SetData, except = -1): Set<string> {
  const unit = data.units.find((x) => x.apiName === u.id);
  const traitName = new Map(data.traits.map((t) => [t.apiName, t.name]));
  const have = new Set<string>(unit?.traits ?? []);
  for (const t of u.extra) if (traitName.has(t)) have.add(traitName.get(t)!);
  u.items.forEach((id, i) => {
    const t = i === except ? null : emblemTrait(data.items.find((x) => x.apiName === id));
    if (t) have.add(t);
  });
  return have;
}

export type CanHold = { ok: true } | { ok: false; reason: string };

/**
 * Whether the unit at `pos` can take the item: it needs a free slot, and an
 * emblem can't go on a unit that already has that trait (its own, an evolved
 * one, or from another emblem it holds), as in the game.
 */
export function canHoldItem(b: Board, pos: number, itemId: string, data: SetData): CanHold {
  const u = b.units.find((x) => x.pos === pos);
  if (!u) return { ok: false, reason: "No unit there" };
  if (u.items.length >= MAX_ITEMS) return { ok: false, reason: "This unit holds 3 items" };
  const trait = emblemTrait(data.items.find((x) => x.apiName === itemId));
  if (trait && heldTraitNames(u, data).has(trait)) {
    const name = data.units.find((x) => x.apiName === u.id)?.name ?? "This unit";
    return { ok: false, reason: `${name} already has ${trait}` };
  }
  return { ok: true };
}

/** addItem that follows canHoldItem; an item that can't be held leaves the board as it was. */
export function addItemChecked(b: Board, pos: number, itemId: string, data: SetData): Board {
  return canHoldItem(b, pos, itemId, data).ok ? addItem(b, pos, itemId) : b;
}

/**
 * Drops emblems whose trait the unit has by other means: it had the trait
 * already, or gained it after the emblem was put on (a Lux's trait menu, an
 * evolved trait). Earlier items win, so of two same-trait emblems the first stays.
 */
export function dropRedundantEmblems(b: Board, data: SetData): { board: Board; dropped: number } {
  let dropped = 0;
  const units = b.units.map((u) => {
    const kept: string[] = [];
    for (const id of u.items) {
      const t = emblemTrait(data.items.find((x) => x.apiName === id));
      if (t && heldTraitNames({ ...u, items: kept }, data).has(t)) {
        dropped++;
        continue;
      }
      kept.push(id);
    }
    return kept.length === u.items.length ? u : { ...u, items: kept };
  });
  return { board: dropped ? { ...b, units } : b, dropped };
}

/** Extra team size from items that say so ("Your team gains +1 maximum team size": the Tactician's items). */
export function teamSizeBonus(b: Board, data: SetData): { bonus: number; sources: string[] } {
  let bonus = 0;
  const sources: string[] = [];
  for (const u of b.units) {
    for (const id of u.items) {
      const item = data.items.find((x) => x.apiName === id);
      const n = Number(/\+(\d+) maximum team size/i.exec(item?.desc ?? "")?.[1] ?? 0);
      if (n > 0 && item) {
        bonus += n;
        sources.push(item.name);
      }
    }
  }
  return { bonus, sources };
}

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
 * the unit already has that trait; a form unit's chosen trait (Lux) counts twice, and so does Elder Dragon's Riftbeast.
 */
export function computeTraits(b: Board, data: SetData): TraitCount[] {
  const unitById = new Map<string, SetUnit>(data.units.map((u) => [u.apiName, u]));
  const traitByName = new Map<string, SetTrait>(data.traits.map((t) => [t.name, t]));
  const itemById = new Map<string, SetItem>(data.items.map((i) => [i.apiName, i]));

  const forms = buildFormIndex(data.units);
  const slotRules = buildSlotRules(data);
  const traitNameByApi = new Map(data.traits.map((t) => [t.apiName, t.name]));
  // trait name -> distinct unit ids (+ emblem holders) and what each counts for
  const members = new Map<string, Map<string, number>>();
  const add = (trait: string, unit: string, weight = 1) => {
    if (!members.has(trait)) members.set(trait, new Map());
    const m = members.get(trait)!;
    m.set(unit, Math.max(m.get(unit) ?? 0, weight));
  };
  for (const placed of b.units) {
    const unit = unitById.get(placed.id);
    if (!unit) continue;
    // A form's chosen trait (Lux's) is counted twice.
    const chosen = forms.chosen.get(placed.id);
    const rule = slotRules.get(placed.id);
    for (const t of unit.traits) add(t, placed.id, t === chosen ? 2 : t === rule?.trait ? (rule.traitCount ?? 1) : 1);
    // Traits gained by evolving count once each (never again for one it has).
    for (const tid of placed.extra) {
      const name = traitNameByApi.get(tid);
      if (name && !unit.traits.includes(name)) add(name, placed.id);
    }
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
  for (const [name, byUnit] of members) {
    const total = [...byUnit.values()].reduce((a, w) => a + w, 0);
    const trait = traitByName.get(name);
    const points = (trait?.breakpoints ?? []).map((bp) => bp.minUnits).filter((n) => n > 0);
    let tier = -1;
    points.forEach((n, i) => {
      if (total >= n) tier = i;
    });
    out.push({
      name,
      apiName: trait?.apiName,
      count: total,
      tier,
      next: points.find((n) => n > total),
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
