import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { BOARD_VALUE_FIT, ITEM_GAIN, copyGain, copiesToNextStar, itemGain, predictedShift, starStepGain } from "./boardValue";
import { Board } from "./board";

const unit = (apiName: string, cost: number) => ({ apiName, name: apiName, cost, traits: [], stats: {}, ability: { name: "", desc: "", values: {} } });
const data = { units: [unit("A", 2), unit("B", 4)], traits: [], augments: [], items: [], wisps: [] } as unknown as SetData;
const board = (units: Board["units"], level = 8): Board => ({ set: 18, title: "", level, units, augments: [null, null, null] });
const u = (id: string, star: 1 | 2 | 3, items: string[] = []) => ({ id, star, pos: 0, items, extra: [] as string[] });

describe("board value fit", () => {
  it("still beats the simple baselines on held-out lobbies", () => {
    const h = BOARD_VALUE_FIT.heldOut;
    expect(h.spearman).toBeGreaterThan(h.goldCostSpearman);
    expect(h.goldCostSpearman).toBeGreaterThan(h.levelOnlySpearman);
    expect(h.spearman).toBeGreaterThan(0.75);
    expect(BOARD_VALUE_FIT.fittedOn.lobbies).toBeGreaterThan(1000);
  });

  it("prices a star-up positively and more for expensive units, per copy", () => {
    for (const cost of [1, 2, 3, 4, 5]) {
      expect(starStepGain(cost, 1)).toBeGreaterThan(0);
      expect(starStepGain(cost, 2)).toBeGreaterThan(0);
    }
    expect(starStepGain(4, 2)).toBeGreaterThan(starStepGain(1, 2));
    expect(copiesToNextStar(1)).toBe(2);
    expect(copiesToNextStar(2)).toBe(6);
    expect(copiesToNextStar(3)).toBe(0);
    expect(copyGain(3, 3)).toBe(0); // nothing past 3 stars
    expect(copyGain(4, 2)).toBeCloseTo(starStepGain(4, 2) / 6, 9);
  });

  it("prices an item at a sane share of a placement, higher on a 3-star holder than a 1-star one, within bounds", () => {
    expect(ITEM_GAIN).toBeGreaterThan(0.05);
    expect(ITEM_GAIN).toBeLessThan(0.6);
    for (const cost of [1, 2, 3, 4, 5]) for (const star of [1, 2, 3]) {
      expect(itemGain(cost, star)).toBeGreaterThanOrEqual(0.06);
      expect(itemGain(cost, star)).toBeLessThanOrEqual(0.9);
    }
    expect(itemGain(4, 3)).toBeGreaterThan(itemGain(4, 1));
  });

  it("predicts a lower (better) placement shift for a stronger board", () => {
    const weak = board([u("A", 1), u("B", 1)]);
    const strong = board([u("A", 2, ["x"]), u("B", 3, ["x", "y"])], 9);
    expect(predictedShift(strong, data)).toBeLessThan(predictedShift(weak, data));
  });
});
