import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { AugmentEffects } from "./augmentScore";
import { archetypeOf, needsOf, scoreByNeeds } from "./augmentNeeds";
import { Board, COLS } from "./board";

// The rules strong players pick augments by (see augmentNeeds.ts), as checks the model has to pass.

const stats = (hp: number, damage: number) => ({ hp, damage, armor: 40, magicResist: 40, attackSpeed: 0.7, critChance: 0.25, critMultiplier: 1.4 });
const unit = (apiName: string, cost: number, hp = 800, damage = 60) => ({ apiName, name: apiName, cost, traits: [], stats: stats(hp, damage), ability: { name: "", desc: "", values: {} } });
const data = {
  setNumber: 18,
  version: "test",
  units: [
    unit("T1", 1, 900, 40), unit("T2", 2, 1000, 40), unit("C3a", 3), unit("C3b", 3), unit("C3c", 3),
    unit("F4a", 4, 900, 80), unit("F4b", 4, 800, 90), unit("F4c", 4, 1200, 50), unit("L5a", 5, 1000, 100), unit("L5b", 5, 1100, 90),
  ],
  traits: [],
  augments: [],
  items: [],
  wisps: [],
} as unknown as SetData;

const placed = (id: string, star: 1 | 2 | 3, pos: number, items: string[] = []) => ({ id, star, pos, items, extra: [] as string[] });
const board = (units: Board["units"], level: number): Board => ({ set: 18, title: "", level, units, augments: [null, null, null] });
const I = ["I1", "I2", "I3"];
// Fast 8: two 2-star 4-cost carries with items, tanks in front.
const fast8 = board([placed("T1", 2, 0), placed("T2", 2, 1), placed("F4c", 2, 2, ["I1"]), placed("C3a", 2, 3), placed("F4a", 2, 3 * COLS, I), placed("F4b", 2, 3 * COLS + 1, I), placed("C3b", 2, 3 * COLS + 2), placed("C3c", 2, 3 * COLS + 3)], 8);
// Fast 9: 5-cost carries.
const fast9 = board([placed("T1", 2, 0), placed("T2", 2, 1), placed("F4c", 2, 2, ["I1"]), placed("C3a", 2, 3), placed("L5a", 2, 3 * COLS, I), placed("L5b", 2, 3 * COLS + 1, I), placed("F4a", 1, 3 * COLS + 2), placed("F4b", 1, 3 * COLS + 3), placed("C3b", 2, 3 * COLS + 4)], 9);
// 3-cost reroll: three 3-star 3-costs.
const reroll3 = board([placed("T1", 2, 0), placed("T2", 2, 1), placed("C3a", 3, 2, ["I1"]), placed("F4c", 1, 3), placed("C3b", 3, 3 * COLS, I), placed("C3c", 3, 3 * COLS + 1, I), placed("F4a", 1, 3 * COLS + 2), placed("F4b", 1, 3 * COLS + 3)], 8);

const fx: Record<string, AugmentEffects> = {
  Econ: { confidence: "high", resources: { gold: 30 } },
  Econ2: { confidence: "high", resources: { gold: 30 } },
  Items: { confidence: "high", resources: { completed: 2 } },
  Combat: { confidence: "high", effects: [{ kind: "stat", scope: "team", stat: "damageAmp", pct: 0.1 }] },
  Ticket: { confidence: "high", resources: { rerollRefund: 0.45 } },
  Copies: { confidence: "high", resources: { duplicators: [{ n: 2, maxCost: 3 }] } },
};
const aug = (apiName: string) => ({ apiName, name: apiName, tier: 2 });
const v = (b: Board, id: string, stage: string, given: string[] = []) => scoreByNeeds(aug(id), b, data, stage, given, fx).impact!;

describe("augment needs: reading the board", () => {
  it("tells the archetypes apart", () => {
    expect(archetypeOf(fast8, data)).toBe("fast8");
    expect(archetypeOf(fast9, data)).toBe("fast9");
    expect(archetypeOf(reroll3, data)).toBe("reroll3");
  });
  it("a fast 9 board is shorter of gold than a fast 8 board", () => {
    expect(needsOf(fast9, data).shortfall).toBeGreaterThan(needsOf(fast8, data).shortfall);
  });
});

describe("augment needs: the rules", () => {
  it("econ is worth most at 2-1 and least at 4-2 (interest compounds)", () => {
    expect(v(fast8, "Econ", "2-1")).toBeGreaterThan(v(fast8, "Econ", "3-2"));
    expect(v(fast8, "Econ", "3-2")).toBeGreaterThan(v(fast8, "Econ", "4-2"));
  });
  it("combat is weakest at 2-1 and strongest at 4-2", () => {
    expect(v(fast8, "Combat", "4-2")).toBeGreaterThan(v(fast8, "Combat", "2-1") * 1.8);
  });
  it("at 2-1 econ beats combat; at 4-2 combat closes in", () => {
    expect(v(fast8, "Econ", "2-1")).toBeGreaterThan(v(fast8, "Combat", "2-1"));
    expect(v(fast8, "Combat", "4-2") / v(fast8, "Econ", "4-2")).toBeGreaterThan(v(fast8, "Combat", "2-1") / v(fast8, "Econ", "2-1"));
  });
  it("fast 9 wants econ more than fast 8 does", () => {
    expect(v(fast9, "Econ", "2-1") / v(fast9, "Items", "2-1")).toBeGreaterThan(v(fast8, "Econ", "2-1") / v(fast8, "Items", "2-1"));
  });
  it("a second econ augment adds less; an item augment after econ doesn't lose value", () => {
    expect(v(fast8, "Econ2", "3-2", ["Econ"])).toBeLessThan(v(fast8, "Econ2", "3-2"));
    expect(v(fast8, "Items", "3-2", ["Econ"])).toBeCloseTo(v(fast8, "Items", "3-2"), 6);
  });
  it("after econ at 2-1, a fast 8 board wants items at 3-2", () => {
    expect(v(fast8, "Items", "3-2", ["Econ"])).toBeGreaterThan(v(fast8, "Econ2", "3-2", ["Econ"]));
  });
  it("a reroll board with Prismatic Ticket wants items, not more copies", () => {
    expect(v(reroll3, "Copies", "3-2", ["Ticket"])).toBeLessThan(v(reroll3, "Copies", "3-2"));
    expect(v(reroll3, "Items", "3-2", ["Ticket"])).toBeGreaterThan(v(reroll3, "Copies", "3-2", ["Ticket"]));
  });
  it("items past what the carries and tank can hold count as spares", () => {
    const lots: Record<string, AugmentEffects> = { Many: { confidence: "high", resources: { completed: 12 } } };
    const twelve = scoreByNeeds(aug("Many"), fast8, data, "3-2", [], lots).impact!;
    expect(twelve).toBeLessThan(6 * v(fast8, "Items", "3-2"));
  });
});
