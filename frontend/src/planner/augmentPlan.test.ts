import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { SHOP, buildPlan, copyPrice, hitChance } from "./augmentPlan";
import { AugmentEffects, EFFECTS, scoreAugment } from "./augmentScore";
import { Board } from "./board";

const unit = (apiName: string, cost: number) => ({
  apiName,
  name: apiName,
  cost,
  traits: [],
  stats: { hp: 700, damage: 50, armor: 30, magicResist: 30, attackSpeed: 0.7, critChance: 0.25, critMultiplier: 1.4 },
  ability: { name: "", desc: "", values: {} },
});
// 13 one-costs, 13 two-costs, 13 three-costs, 12 four-costs, 9 five-costs.
const units = [
  ...Array.from({ length: 13 }, (_, i) => unit(`C1_${i}`, 1)),
  ...Array.from({ length: 13 }, (_, i) => unit(`C2_${i}`, 2)),
  ...Array.from({ length: 13 }, (_, i) => unit(`C3_${i}`, 3)),
  ...Array.from({ length: 12 }, (_, i) => unit(`C4_${i}`, 4)),
  ...Array.from({ length: 9 }, (_, i) => unit(`C5_${i}`, 5)),
];
const data = { setNumber: 18, version: "t", units, traits: [], augments: [], items: [], wisps: [] } as unknown as SetData;
const p = (id: string, pos: number, items: string[] = [], star: 1 | 2 | 3 = 2) => ({ id, star, pos, items, extra: [] as string[] });
const board = (us: Board["units"], level: number): Board => ({ set: 18, title: "", level, units: us, augments: [null, null, null] });

// Cheap units at 2 stars, the carries (holding items) among them.
const reroll = board([p("C2_0", 0), p("C2_1", 21, ["A", "B"]), p("C3_0", 22, ["A", "B", "C"]), p("C1_0", 1), p("C3_1", 23)], 7);
// Expensive carries (holding items), a few cheap filler units.
const expensive = board([p("C1_0", 0), p("C2_0", 1), p("C4_0", 21, ["A", "B", "C"]), p("C4_1", 22, ["A", "B"]), p("C5_0", 23, [], 1), p("C3_0", 2)], 9);

const aug = (apiName: string) => ({ apiName, name: apiName, tier: 2 });
const only = (e: Partial<AugmentEffects>): Record<string, AugmentEffects> => ({ X: { confidence: "high", ...e } as AugmentEffects });

describe("shop price of a copy", () => {
  it("rises with the number of units to pick from and falls with the odds", () => {
    const plan = (level: number) => buildPlan(board([p("C1_0", 0)], level), data);
    expect(copyPrice(1, plan(4))).toBeLessThan(copyPrice(3, plan(4))); // a 3-cost is rarer at level 4
    expect(copyPrice(3, plan(7))).toBeLessThan(copyPrice(3, plan(4))); // more likely at level 7
    expect(copyPrice(5, plan(5))).toBe(SHOP.copyPriceCap); // not in the shop at level 5
  });

  it("matches the shop odds: a 2-cost at level 6 is a 3.1% slot", () => {
    const plan = buildPlan(board([p("C2_0", 0)], 6), data);
    const perSlot = 0.4 / 13;
    const perReroll = 1 - (1 - perSlot) ** 5;
    expect(copyPrice(2, plan)).toBeCloseTo(2 + 2 / perReroll, 6);
  });
});

describe("board plan", () => {
  it("tells a board of cheap carries from one of expensive carries", () => {
    expect(buildPlan(reroll, data).lowCostShare).toBeGreaterThan(0.9);
    expect(buildPlan(expensive, data).lowCostShare).toBeLessThan(0.1);
    expect(buildPlan(reroll, data).rollIntent).toBeGreaterThan(buildPlan(expensive, data).rollIntent);
  });

  it("lists the units that still want copies; a 3-star or a played-out unit doesn't", () => {
    const plan = buildPlan(board([p("C1_0", 0, [], 3), p("C2_0", 1, [], 2), p("C4_0", 2, [], 2)], 8), data);
    expect(plan.targets.map((t) => t.id)).toEqual(["C2_0"]); // C1_0 is 3-star, C4_0 is at its goal (2 stars)
    expect(plan.targets[0].remaining).toBe(6);
  });

  it("counts filler units' copies as worth less than a carry's", () => {
    const plan = buildPlan(expensive, data);
    const filler = plan.targets.find((t) => t.id === "C1_0")!;
    expect(filler.weight).toBe(SHOP.fillerWeight);
    expect(hitChance(1, plan)).toBeLessThan(1 / 13);
  });
});

describe("copy-giving augments depend on the plan", () => {
  const duplicators = only({ resources: { duplicators: [{ n: 2, maxCost: 3 }] } });
  const bench = only({ resources: { benchTransform: { slots: 3 } } });
  const rerolls = only({ resources: { rerolls: 10 } });
  const impact = (e: Record<string, AugmentEffects>, b: Board) => scoreAugment(aug("X"), b, data, "3-2", e).impact!;

  it("values duplicators far more on a reroll board than on a board of expensive carries", () => {
    expect(impact(duplicators, reroll)).toBeGreaterThan(1.5 * impact(duplicators, expensive));
    const r = scoreAugment(aug("X"), reroll, data, "3-2", duplicators);
    expect(r.parts[0].note).toMatch(/copies of/);
  });

  it("only copies units up to the duplicator's cost", () => {
    // The only unit that wants copies costs 4: a "3-cost or less" duplicator has nothing to copy.
    const fourCost = board([p("C4_0", 21, ["A", "B", "C"], 1), p("C4_1", 22, ["A"], 2)], 8);
    const r = scoreAugment(aug("X"), fourCost, data, "3-2", duplicators);
    expect(r.parts[0].note).toMatch(/no unit up to 3-cost/);
  });

  it("values Pandora's Bench on a reroll board and nothing when nothing wants copies", () => {
    expect(impact(bench, reroll)).toBeGreaterThan(impact(bench, expensive));
    const done = board([p("C2_0", 0, [], 3), p("C3_0", 1, [], 3)], 7);
    const r = scoreAugment(aug("X"), done, data, "3-2", bench);
    expect(r.parts.length).toBe(0); // nothing to add, so nothing scored
  });

  it("values free rerolls by how much the board rolls", () => {
    expect(impact(rerolls, reroll)).toBeGreaterThan(impact(rerolls, expensive));
  });

  it("values a random champion by the chance it's a copy the board wants", () => {
    const random = only({ resources: { units: [{ n: 1, cost: 2, star: 1 }] } });
    const none = board([p("C4_0", 21, ["A", "B"], 2), p("C4_1", 22, ["A"], 2)], 8); // no 2-cost wanted
    expect(impact(random, reroll)).toBeGreaterThan(impact(random, none));
  });
});

describe("extracted copy effects (Set 18)", () => {
  it("reads duplicators as copies, Pandora's Bench as a bench transform and Worth the Wait as copies each round", () => {
    expect(EFFECTS.DA_HeroicGrabBag.resources!.duplicators).toEqual([{ n: 2, maxCost: 3 }]);
    expect(EFFECTS.DA_PandorasBench.resources!.benchTransform).toEqual({ slots: 3 });
    expect(EFFECTS.DA_WorththeWait.recurring!.copies).toMatchObject({ perRound: 1, cost: 1 });
    expect(EFFECTS.DA_MissedConnections.resources!.allOfCost).toBe(1);
  });
});
