import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import {
  COLS,
  ROWS,
  addItem,
  addUnit,
  boardCost,
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
  augments: [{ apiName: "A_One", name: "One", tier: 1 }],
} as unknown as SetData;

describe("URL round trip", () => {
  it("keeps units, items, augments, level and title", () => {
    let b = emptyBoard(18);
    b = { ...b, title: "My comp", level: 9 };
    b = addUnit(b, "U_A", 3);
    b = addItem(addItem(b, 3, "I_IE"), 3, "I_EmblemSage");
    b = addUnit(b, "U_B", 20);
    b = setAugment(b, 0, "A_One");
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
      "set=18&u=U_A.2.5&u=U_B.9.6.I1,I2,I3,I4,I5&u=bad id.1.1&u=U_C.1.28&u=U_C.1.-1&u=U_C.x.1&u=U_A.1.5&u=U_C..2&lv=99&a=A1&a=A1&a=A2&a=A3&a=A4",
    );
    expect(b.units.map((u) => [u.id, u.star, u.pos])).toEqual([
      ["U_A", 2, 5],
      ["U_B", 3, 6], // star clamped to 3
      ["U_C", 1, 1], // non-numeric star falls back to 1 (pos 28 and -1 were rejected)
      ["U_C", 1, 2], // empty star too; the duplicate unit is fine at another cell
    ]); // "bad id" is rejected, and the second unit at pos 5 is ignored
    expect(b.units[1].items).toHaveLength(3);
    expect(b.level).toBe(8);
    expect(b.augments).toEqual(["A1", "A2", "A3"]);
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
    const b = decodeBoard(new URLSearchParams("u=U_A.1.0.I_IE,I_nope&u=U_gone.1.1&a=A_One&a=A_gone"), 18);
    const { board, dropped } = sanitize(b, data);
    expect(board.units.map((u) => u.id)).toEqual(["U_A"]);
    expect(board.units[0].items).toEqual(["I_IE"]);
    expect(board.augments).toEqual(["A_One"]);
    expect(dropped).toEqual({ units: 1, items: 1, augments: 1 });
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

  it("holds at most three items and three augments", () => {
    let b = addUnit(emptyBoard(18), "U_A", 0);
    for (let i = 0; i < 5; i++) b = addItem(b, 0, "I_IE");
    expect(b.units[0].items).toHaveLength(3);
    for (const a of ["1", "2", "3", "4"]) b = setAugment(b, b.augments.length, a);
    expect(b.augments).toEqual(["1", "2", "3"]);
    expect(setAugment(b, 0, null).augments).toEqual(["2", "3"]);
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
