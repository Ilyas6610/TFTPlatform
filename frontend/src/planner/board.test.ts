import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { STAGE_DATA, StageEntry, augmentTier, availableAt, stagesOf } from "./augmentStages";
import {
  COLS,
  ROWS,
  addItem,
  addUnit,
  boardCost,
  buildTraitChoices,
  toggleExtraTrait,
  normalizeForms,
  buildFormIndex,
  setForm,
  clickCell,
  computeTraits,
  decodeBoard,
  emptyBoard,
  encodeBoard,
  firstFreeCell,
  moveUnit,
  sanitize,
  setAugment,
} from "./board";

const data = {
  units: [
    { apiName: "U_A", name: "A", cost: 1, traits: ["Blade", "Sage"] },
    { apiName: "U_B", name: "B", cost: 3, traits: ["Blade"] },
    { apiName: "U_C", name: "C", cost: 5, traits: ["Sage"] },
  ],
  traits: [
    { apiName: "T_Blade", name: "Blade", breakpoints: [{ minUnits: 2, style: 1 }, { minUnits: 4, style: 2 }] },
    { apiName: "T_Sage", name: "Sage", breakpoints: [{ minUnits: 2, style: 1 }] },
  ],
  items: [
    { apiName: "I_IE", name: "Infinity Edge", kind: "completed" },
    { apiName: "I_EmblemSage", name: "Sage Emblem", kind: "emblem" },
    { apiName: "I_EmblemBlade", name: "Blade Emblem", kind: "emblem" },
  ],
  augments: [
    { apiName: "A_One", name: "One", tier: 1 },
    { apiName: "A_Two", name: "Two", tier: 3 },
  ],
} as unknown as SetData;

describe("URL round trip", () => {
  it("keeps units, items, augments, level and title", () => {
    let b = emptyBoard(18);
    b = { ...b, title: "My comp", level: 9 };
    b = addUnit(b, "U_A", 3);
    b = addItem(addItem(b, 3, "I_IE"), 3, "I_EmblemSage");
    b = addUnit(b, "U_B", 20);
    b = setAugment(b, 1, "A_One");
    const back = decodeBoard(new URLSearchParams(encodeBoard(b).toString()), 18);
    expect(back).toEqual({ ...b, units: [...b.units].sort((x, y) => x.pos - y.pos) });
  });

  it("omits defaults so links stay short", () => {
    expect(encodeBoard(emptyBoard(18)).toString()).toBe("set=18");
  });
});

describe("decoding untrusted input", () => {
  const dec = (s: string) => decodeBoard(new URLSearchParams(s), 18);

  it("drops malformed units and bounds everything", () => {
    const b = dec(
      "set=18&u=U_A.2.5&u=U_B.9.6.I1,I2,I3,I4,I5&u=bad id.1.1&u=U_C.1.28&u=U_C.1.-1&u=U_C.x.1&u=U_A.1.5&u=U_C..2&lv=99&a=A1&a=A1&a=A2&a=A3&a=A4&a3=A9&a2=A9",
    );
    expect(b.units.map((u) => [u.id, u.star, u.pos])).toEqual([
      ["U_A", 2, 5],
      ["U_B", 3, 6], // star clamped to 3
      ["U_C", 1, 1], // non-numeric star falls back to 1 (pos 28 and -1 were rejected)
      ["U_C", 1, 2], // empty star too; the duplicate unit is fine at another cell
    ]); // "bad id" is rejected, and the second unit at pos 5 is ignored
    expect(b.units[1].items).toHaveLength(3);
    expect(b.level).toBe(8);
    // a2 and a3 take their own slots (A9 once); plain `a` values fill what is left in order.
    expect(b.augments).toEqual(["A1", "A9", "A2"]);
  });

  it("cleans the title and rejects odd sets", () => {
    const b = dec("set=999999&t=%00%1b%20%20hello%0A%20%20world" + "x".repeat(100));
    expect(b.set).toBe(18);
    expect(b.title.startsWith("hello world")).toBe(true);
    expect(b.title.length).toBeLessThanOrEqual(40);
  });

  it("never lets a payload reach a unit position outside the board", () => {
    const b = dec("u=U_A.1.27&u=U_B.1.28&u=U_C.1.1e3");
    expect(b.units.map((u) => u.pos)).toEqual([27]);
  });
});

describe("sanitize", () => {
  it("drops what the set data doesn't know", () => {
    const b = decodeBoard(new URLSearchParams("u=U_A.1.0.I_IE,I_nope&u=U_gone.1.1&a1=A_One&a3=A_gone"), 18);
    const { board, dropped } = sanitize(b, data);
    expect(board.units.map((u) => u.id)).toEqual(["U_A"]);
    expect(board.units[0].items).toEqual(["I_IE"]);
    expect(board.augments).toEqual(["A_One", null, null]);
    expect(dropped).toEqual({ units: 1, items: 1, augments: 1, traits: 0 });
  });
});

describe("edits", () => {
  it("fills from the back line and stops when full", () => {
    let b = emptyBoard(18);
    expect(firstFreeCell(b)).toBe((ROWS - 1) * COLS);
    for (let i = 0; i < ROWS * COLS; i++) b = addUnit(b, "U_A");
    expect(b.units).toHaveLength(ROWS * COLS);
    expect(firstFreeCell(b)).toBeNull();
    expect(addUnit(b, "U_A")).toBe(b);
  });

  it("moves into an empty cell and swaps with an occupied one", () => {
    let b = addUnit(addUnit(emptyBoard(18), "U_A", 0), "U_B", 1);
    b = moveUnit(b, 0, 5);
    expect(b.units.find((u) => u.id === "U_A")!.pos).toBe(5);
    b = moveUnit(b, 5, 1);
    expect(b.units.find((u) => u.id === "U_A")!.pos).toBe(1);
    expect(b.units.find((u) => u.id === "U_B")!.pos).toBe(5);
  });

  it("holds at most three items", () => {
    let b = addUnit(emptyBoard(18), "U_A", 0);
    for (let i = 0; i < 5; i++) b = addItem(b, 0, "I_IE");
    expect(b.units[0].items).toHaveLength(3);
  });

  it("puts each augment in one slot, and slots can be filled in any order", () => {
    let b = setAugment(emptyBoard(18), 2, "A_One");
    b = setAugment(b, 0, "A_Two");
    expect(b.augments).toEqual(["A_Two", null, "A_One"]);
    b = setAugment(b, 1, "A_One"); // moving it empties the old slot
    expect(b.augments).toEqual(["A_Two", "A_One", null]);
    expect(setAugment(b, 5, "A_One")).toBe(b);
    expect(setAugment(b, 0, null).augments).toEqual([null, "A_One", null]);
  });
});

describe("traits and cost", () => {
  it("counts each distinct unit once and finds the tier", () => {
    let b = emptyBoard(18);
    b = addUnit(b, "U_A", 0);
    b = addUnit(b, "U_A", 1); // a second copy doesn't add
    b = addUnit(b, "U_B", 2);
    const t = Object.fromEntries(computeTraits(b, data).map((x) => [x.name, x]));
    expect(t.Blade.count).toBe(2);
    expect(t.Blade.tier).toBe(0);
    expect(t.Blade.next).toBe(4);
    expect(t.Sage.count).toBe(1);
    expect(t.Sage.tier).toBe(-1);
  });

  it("an emblem adds a trait the holder lacks, not one it has", () => {
    let b = addUnit(emptyBoard(18), "U_B", 0); // Blade only
    b = addUnit(b, "U_A", 1); // Blade + Sage
    b = addItem(b, 0, "I_EmblemSage"); // B gains Sage
    b = addItem(b, 1, "I_EmblemBlade"); // A already Blade: no change
    const t = Object.fromEntries(computeTraits(b, data).map((x) => [x.name, x]));
    expect(t.Sage.count).toBe(2);
    expect(t.Sage.tier).toBe(0);
    expect(t.Blade.count).toBe(2);
  });

  it("lists active traits first", () => {
    let b = addUnit(emptyBoard(18), "U_C", 0);
    b = addUnit(b, "U_A", 1);
    b = addUnit(b, "U_B", 2);
    expect(computeTraits(b, data).map((x) => x.name)).toEqual(["Blade", "Sage"]);
  });

  it("prices stars as copies", () => {
    let b = addUnit(addUnit(emptyBoard(18), "U_A", 0), "U_C", 1);
    b = { ...b, units: b.units.map((u) => (u.id === "U_A" ? { ...u, star: 3 } : { ...u, star: 2 })) };
    expect(boardCost(b, data)).toBe(1 * 9 + 5 * 3);
  });
});

describe("augment stages", () => {
  const table: Record<string, StageEntry> = {
    A_First: { tier: "silver", stages: [1, 0, 0] },
    A_Late: { tier: "prismatic", stages: [0, 1, 1] },
    A_Never: { tier: "gold", stages: [0, 0, 0] },
  };

  it("offers an augment only where the data says it can appear", () => {
    expect([0, 1, 2].map((s) => availableAt({ apiName: "A_First" }, s, table))).toEqual([true, false, false]);
    expect([0, 1, 2].map((s) => availableAt({ apiName: "A_Late" }, s, table))).toEqual([false, true, true]);
    expect([0, 1, 2].map((s) => availableAt({ apiName: "A_Never" }, s, table))).toEqual([false, false, false]);
    expect(stagesOf({ apiName: "A_Late" }, table)).toEqual(["3-2", "4-2"]);
  });

  it("treats an augment the data doesn't know as offerable in the three slots only", () => {
    expect([0, 1, 2].map((s) => availableAt({ apiName: "A_New" }, s, table))).toEqual([true, true, true]);
    expect(availableAt({ apiName: "A_New" }, 3, table)).toBe(false);
    expect(stagesOf({ apiName: "A_New" }, table)).toBeNull();
  });

  it("falls back to the stage data's tier when ours is unknown", () => {
    expect(augmentTier({ apiName: "A_Late", tier: 0 }, table)).toBe(3);
    expect(augmentTier({ apiName: "A_Late", tier: 2 }, table)).toBe(2);
    expect(augmentTier({ apiName: "A_New", tier: 0 }, table)).toBe(0);
  });

  it("the shipped data is complete: three flags each, known tiers, mostly limited stages", () => {
    const entries = Object.values(STAGE_DATA);
    expect(entries.length).toBeGreaterThan(200);
    expect(entries.every((e) => e.stages.length === 3 && e.stages.every((f) => f === 0 || f === 1))).toBe(true);
    expect(entries.every((e) => ["silver", "gold", "prismatic"].includes(e.tier))).toBe(true);
    expect(entries.filter((e) => e.stages.includes(0)).length).toBeGreaterThan(100);
  });

  it("an augment is dropped from a slot its stage can't offer when a link is loaded", () => {
    const aug = (id: string) => ({ apiName: id, name: id, tier: 1 });
    const d = { ...data, augments: [aug("A_First"), aug("A_Late")] } as unknown as SetData;
    // Uses the shipped table, which doesn't know these ids: both stay.
    const loose = sanitize(decodeBoard(new URLSearchParams("a1=A_Late&a3=A_First"), 18), d);
    expect(loose.board.augments).toEqual(["A_Late", null, "A_First"]);
    // A real restricted augment from the shipped data.
    const [id, entry] = Object.entries(STAGE_DATA).find(([, e]) => e.stages.join() === "1,0,0")!;
    const real = { ...data, augments: [aug(id)] } as unknown as SetData;
    const ok = sanitize(decodeBoard(new URLSearchParams(`a1=${id}`), 18), real);
    expect(ok.board.augments).toEqual([id, null, null]);
    const bad = sanitize(decodeBoard(new URLSearchParams(`a3=${id}`), 18), real);
    expect(bad.board.augments).toEqual([null, null, null]);
    expect(bad.dropped.augments).toBe(1);
    expect(entry.stages).toEqual([1, 0, 0]);
  });
});

describe("clickCell", () => {
  const board = addUnit(addUnit(emptyBoard(18), "U_A", 3), "U_B", 10);

  it("clicking a unit selects it, and clicking another unit switches the selection", () => {
    expect(clickCell(board, null, 3)).toEqual({ selected: 3 });
    expect(clickCell(board, 3, 10)).toEqual({ selected: 10 }); // no swap, no move
  });

  it("clicking the selected unit again deselects it", () => {
    expect(clickCell(board, 3, 3)).toEqual({ selected: null });
  });

  it("clicking an empty cell moves the selected unit there and keeps it selected", () => {
    expect(clickCell(board, 3, 20)).toEqual({ selected: 20, move: { from: 3, to: 20 } });
  });

  it("clicking an empty cell with nothing selected does nothing", () => {
    expect(clickCell(board, null, 20)).toEqual({ selected: null });
    expect(clickCell(board, 99, 20)).toEqual({ selected: null }); // a stale selection
  });
});

describe("units with a chosen trait (Lux)", () => {
  const unit = (apiName: string, name: string, traits: string[], cost = 5) => ({ apiName, name, cost, traits });
  const luxData = {
    units: [
      unit("U_Lux", "Lux", ["Avatar"]),
      unit("U_Lux_Blade", "Lux (Blade)", ["Blade", "Avatar"]),
      unit("U_Lux_Moon", "Lux (Sage)", ["Sage", "Avatar"]),
      unit("U_A", "A", ["Blade"], 1),
      unit("U_B", "B", ["Blade"], 2),
      unit("U_Odd", "Odd (Unknown)", ["Blade"]), // base "Odd" doesn't exist
      unit("U_Odd2", "Odd2", ["Blade"]),
      unit("U_Odd2_X", "Odd2 (Nope)", ["Blade"]), // "Nope" isn't one of its traits
    ],
    traits: [
      { apiName: "T_Blade", name: "Blade", breakpoints: [{ minUnits: 2, style: 1 }, { minUnits: 4, style: 2 }] },
      { apiName: "T_Sage", name: "Sage", breakpoints: [{ minUnits: 2, style: 1 }] },
      { apiName: "T_Avatar", name: "Avatar", breakpoints: [{ minUnits: 1, style: 4 }] },
    ],
    items: [],
    augments: [],
  } as unknown as SetData;

  const idx = buildFormIndex(luxData.units);

  it("recognises forms by name, base and trait", () => {
    expect(idx.forms.get("U_Lux")).toEqual([
      { id: "U_Lux_Blade", trait: "Blade" },
      { id: "U_Lux_Moon", trait: "Sage" },
    ]);
    expect(idx.baseOf.get("U_Lux_Moon")).toBe("U_Lux");
    expect(idx.chosen.get("U_Lux_Blade")).toBe("Blade");
    expect(idx.baseOf.has("U_Odd")).toBe(false);
    expect(idx.baseOf.has("U_Odd2_X")).toBe(false);
    expect(idx.forms.size).toBe(1);
  });

  it("changing the trait swaps the unit for that form and keeps its cell, stars and items", () => {
    let b = addUnit(emptyBoard(18), "U_Lux", 9);
    b = { ...b, units: [{ ...b.units[0], star: 2, items: ["I_X"] }] };
    const back = setForm(setForm(b, 9, "U_Lux_Blade", idx), 9, "U_Lux", idx);
    expect(setForm(b, 9, "U_Lux_Blade", idx).units).toEqual([{ id: "U_Lux_Blade", star: 2, pos: 9, items: ["I_X"], extra: [] }]);
    expect(back.units).toEqual(b.units);
  });

  it("the chosen trait counts twice, the base Lux adds none", () => {
    const traits = (b: ReturnType<typeof emptyBoard>) => Object.fromEntries(computeTraits(b, luxData).map((t) => [t.name, t]));
    let b = addUnit(addUnit(emptyBoard(18), "U_Lux_Blade", 0), "U_A", 1);
    let t = traits(b);
    expect(t.Blade.count).toBe(3); // Lux counts 2, A counts 1
    expect(t.Blade.tier).toBe(0);
    expect(t.Avatar.count).toBe(1);
    b = setForm(b, 0, "U_Lux", idx); // no trait chosen
    t = traits(b);
    expect(t.Blade.count).toBe(1);
    expect(t.Avatar.count).toBe(1);
    b = addUnit(addUnit(setForm(b, 0, "U_Lux_Blade", idx), "U_B", 2), "U_Odd2", 3);
    expect(traits(b).Blade.count).toBe(5); // 2 + A + B + Odd2
    expect(traits(b).Blade.tier).toBe(1);
  });

  it("every copy of the unit takes the trait that is chosen", () => {
    let b = addUnit(addUnit(addUnit(emptyBoard(18), "U_Lux", 0), "U_A", 1), "U_Lux", 5);
    b = setForm(b, 5, "U_Lux_Blade", idx);
    expect(b.units.filter((u) => u.id.startsWith("U_Lux")).map((u) => u.id)).toEqual(["U_Lux_Blade", "U_Lux_Blade"]);
    b = setForm(b, 0, "U_Lux_Moon", idx);
    expect(b.units.filter((u) => u.id.startsWith("U_Lux")).map((u) => u.id)).toEqual(["U_Lux_Moon", "U_Lux_Moon"]);
    b = setForm(b, 5, "U_Lux", idx); // back to none, for both
    expect(b.units.filter((u) => u.id.startsWith("U_Lux")).map((u) => u.id)).toEqual(["U_Lux", "U_Lux"]);
    expect(b.units.find((u) => u.id === "U_A")).toBeTruthy(); // others untouched
  });

  it("a Lux added later joins the trait already chosen, and links with mixed forms are made to agree", () => {
    let b = setForm(addUnit(emptyBoard(18), "U_Lux", 9), 9, "U_Lux_Blade", idx);
    b = normalizeForms(addUnit(b, "U_Lux", 2), idx); // new base Lux at the front
    expect(b.units.map((u) => u.id)).toEqual(["U_Lux_Blade", "U_Lux_Blade"]);
    const mixed = decodeBoard(new URLSearchParams("u=U_Lux_Moon.1.3&u=U_Lux_Blade.1.8&u=U_Lux.1.20"), 18);
    const clean = sanitize(mixed, luxData).board;
    expect(clean.units.map((u) => u.id)).toEqual(["U_Lux_Moon", "U_Lux_Moon", "U_Lux_Moon"]); // front-most chosen form wins
  });

  it("two Luxes are one unit for trait counts: the chosen trait is still just 2", () => {
    const b = setForm(addUnit(addUnit(emptyBoard(18), "U_Lux", 0), "U_Lux", 1), 0, "U_Lux_Blade", idx);
    const t = Object.fromEntries(computeTraits(b, luxData).map((x) => [x.name, x]));
    expect(t.Blade.count).toBe(2);
  });
});

describe("traits gained by evolving (Kha'Zix)", () => {
  const kz = {
    units: [
      { apiName: "U_Kha", name: "Kha'Zix", cost: 3, traits: ["Rival"] },
      { apiName: "U_Rengar", name: "Rengar", cost: 4, traits: ["Rival"] },
      { apiName: "U_X", name: "X", cost: 1, traits: ["Slayer"] },
    ],
    traits: [
      { apiName: "T_Rival", name: "Rival", desc: "", breakpoints: [{ minUnits: 1, style: 1, text: "(1) Takedowns evolve Kha'Zix, permanently granting him your choice of Executioner, Rapidfire, Ravager, or Spellweaver.\n\nRengar grants gold." }] },
      { apiName: "T_Exec", name: "Executioner", breakpoints: [{ minUnits: 2, style: 1 }, { minUnits: 4, style: 2 }] },
      { apiName: "T_Rapid", name: "Rapidfire", breakpoints: [{ minUnits: 2, style: 1 }] },
      { apiName: "T_Rav", name: "Ravager", breakpoints: [{ minUnits: 2, style: 1 }] },
      { apiName: "T_Spell", name: "Spellweaver", breakpoints: [{ minUnits: 2, style: 1 }] },
      { apiName: "T_Slayer", name: "Slayer", breakpoints: [{ minUnits: 2, style: 1 }] },
    ],
    items: [],
    augments: [],
  } as unknown as SetData;
  const choices = buildTraitChoices(kz).get("U_Kha")!;

  it("reads the choices from the Rival trait's text", () => {
    expect(choices.traits.map((t) => t.name)).toEqual(["Executioner", "Rapidfire", "Ravager", "Spellweaver"]);
    expect(choices.max).toBe(4);
    expect(buildTraitChoices(kz).has("U_Rengar")).toBe(false);
  });

  it("a unit can hold each choice once, up to the maximum", () => {
    let b = addUnit(emptyBoard(18), "U_Kha", 4);
    for (const t of ["T_Exec", "T_Rapid", "T_Rav", "T_Spell"]) b = toggleExtraTrait(b, 4, t, choices);
    expect(b.units[0].extra).toEqual(["T_Exec", "T_Rapid", "T_Rav", "T_Spell"]);
    b = toggleExtraTrait(b, 4, "T_Rav", choices); // off again
    expect(b.units[0].extra).toEqual(["T_Exec", "T_Rapid", "T_Spell"]);
    expect(toggleExtraTrait(b, 4, "T_Slayer", choices)).toBe(b); // not one of its choices
    const capped = { ...choices, max: 2 };
    expect(toggleExtraTrait(addUnit(emptyBoard(18), "U_Kha", 4), 4, "T_Exec", capped).units[0].extra).toEqual(["T_Exec"]);
  });

  it("each evolved trait counts for the unit, and the unit's own trait is unchanged", () => {
    let b = addUnit(addUnit(emptyBoard(18), "U_Kha", 0), "U_X", 1);
    b = toggleExtraTrait(toggleExtraTrait(b, 0, "T_Exec", choices), 0, "T_Rapid", choices);
    const t = Object.fromEntries(computeTraits(b, kz).map((x) => [x.name, x]));
    expect(t.Executioner.count).toBe(1);
    expect(t.Rapidfire.count).toBe(1);
    expect(t.Rival.count).toBe(1);
    const two = toggleExtraTrait(addUnit(b, "U_Rengar", 2), 0, "T_Rav", choices);
    expect(Object.fromEntries(computeTraits(two, kz).map((x) => [x.name, x.count])).Rival).toBe(2);
  });

  it("round-trips through the link, with and without items", () => {
    let b = addUnit(addUnit(emptyBoard(18), "U_Kha", 6), "U_X", 7);
    b = toggleExtraTrait(toggleExtraTrait(b, 6, "T_Exec", choices), 6, "T_Spell", choices);
    b = addItem(b, 6, "I_IE");
    b = addItem(addUnit(b, "U_Rengar", 9), 9, "I_IE");
    const q = encodeBoard(b);
    expect(q.getAll("u")).toEqual(["U_Kha.1.6.I_IE.T_Exec,T_Spell", "U_X.1.7", "U_Rengar.1.9.I_IE"]);
    expect(decodeBoard(new URLSearchParams(q.toString()), 18).units.map((u) => u.extra)).toEqual([["T_Exec", "T_Spell"], [], []]);
    const onlyTraits = toggleExtraTrait(addUnit(emptyBoard(18), "U_Kha", 3), 3, "T_Rav", choices);
    expect(encodeBoard(onlyTraits).get("u")).toBe("U_Kha.1.3..T_Rav");
    expect(decodeBoard(encodeBoard(onlyTraits), 18).units[0]).toEqual({ id: "U_Kha", star: 1, pos: 3, items: [], extra: ["T_Rav"] });
  });

  it("a loaded link keeps only the traits the unit can have, at most four", () => {
    const b = decodeBoard(
      new URLSearchParams("u=U_Kha.1.0..T_Exec,T_Slayer,T_Nope,T_Exec,T_Rav&u=U_X.1.1..T_Exec&u=U_Kha.1.2..a,b,c,d,e,f"),
      18,
    );
    expect(b.units[2].extra).toHaveLength(4); // capped while decoding
    const { board, dropped } = sanitize(b, kz);
    expect(board.units[0].extra).toEqual(["T_Exec", "T_Rav"]);
    expect(board.units[1].extra).toEqual([]); // X can't evolve
    expect(board.units[2].extra).toEqual([]);
    expect(dropped.traits).toBe(2 + 1 + 4); // Slayer + Nope, X's Exec, and the four unknown ones
  });
});
