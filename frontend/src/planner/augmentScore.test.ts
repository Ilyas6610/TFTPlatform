import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { AugmentEffects, ASSUMPTIONS, EFFECTS, rankAugments, scoreAugment } from "./augmentScore";
import { ITEM_GAIN, itemGain } from "./boardValue";
import { Board, COLS } from "./board";

const stats = (hp: number, damage: number) => ({ hp, damage, armor: 40, magicResist: 40, attackSpeed: 0.7, critChance: 0.25, critMultiplier: 1.4 });
const unit = (apiName: string, cost: number, hp: number, damage: number) => ({
  apiName,
  name: apiName,
  cost,
  traits: [],
  stats: stats(hp, damage),
  ability: { name: "", desc: "", values: {} },
});
const data = {
  setNumber: 18,
  version: "test",
  units: [unit("Tank", 2, 1200, 40), unit("Carry", 4, 700, 80)],
  traits: [],
  augments: [],
  items: [],
  wisps: [],
} as unknown as SetData;

const placed = (id: string, pos: number, items: string[] = [], star: 1 | 2 | 3 = 2) => ({ id, star, pos, items, extra: [] as string[] });
const board = (units: Board["units"], level = 8): Board => ({ set: 18, title: "", level, units, augments: [null, null, null] });
// Tank in the front row, carry holding two items in the back row.
const basic = board([placed("Tank", 0), placed("Carry", 3 * COLS, ["IE", "JG"])]);

const aug = (apiName: string) => ({ apiName, name: apiName, tier: 2 });
const fx = (e: Partial<AugmentEffects> & { confidence?: AugmentEffects["confidence"] }): Record<string, AugmentEffects> => ({ X: { confidence: "high", ...e } as AugmentEffects });

describe("augment score: stats", () => {
  it("scores team stat bonuses by the strength they add, and more of a stat is worth more", () => {
    const small = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "health", pct: 0.1 }] }));
    const big = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "health", pct: 0.3 }] }));
    expect(small.statLift).toBeGreaterThan(0);
    expect(big.statLift).toBeGreaterThan(small.statLift);
    expect(big.impact!).toBeGreaterThan(small.impact!);
  });

  it("uses the geometric mean, so +10% of both HP and damage is about +10%, not +21%", () => {
    const both = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [
      { kind: "stat", scope: "team", stat: "health", pct: 0.1 },
      { kind: "stat", scope: "team", stat: "damageAmp", pct: 0.1 },
    ] }));
    // Health is +10% of health (resists don't change), damage amp +10% of damage.
    expect(both.statLift).toBeGreaterThan(0.095);
    expect(both.statLift).toBeLessThan(0.105);
  });

  it("splits a bonus between auto-attacks and ability: attack damage helps less than damage amp", () => {
    const ad = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "ad", pct: 0.2 }] }));
    const amp = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "damageAmp", pct: 0.2 }] }));
    expect(ad.statLift).toBeLessThan(amp.statLift);
  });

  it("applies a bonus only to the units it covers", () => {
    const holders = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "holders", stat: "ad", pct: 0.3 }] }));
    const unheld = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "unheld", stat: "ad", pct: 0.3 }] }));
    // The item-holding carry does most of the damage, so a bonus to holders beats one to unequipped units.
    expect(holders.statLift).toBeGreaterThan(unheld.statLift);

    const noItems = board([placed("Tank", 0), placed("Carry", 3 * COLS)]);
    const none = scoreAugment(aug("X"), noItems, data, "3-2", fx({ effects: [{ kind: "stat", scope: "holders", stat: "ad", pct: 0.3 }] }));
    expect(none.statLift).toBe(0);
    expect(none.caveats.join(" ")).toMatch(/no unit on this board/);
  });

  it("reads rows from the board: a front-row bonus needs a unit up front", () => {
    const front = fx({ effects: [{ kind: "stat", scope: "front", stat: "health", pct: 0.5 }] });
    const withTank = scoreAugment(aug("X"), basic, data, "3-2", front);
    const allBack = scoreAugment(aug("X"), board([placed("Tank", 3 * COLS), placed("Carry", 3 * COLS + 1)]), data, "3-2", front);
    expect(withTank.statLift).toBeGreaterThan(0);
    expect(allBack.statLift).toBe(0);
  });

  it("scales a per-level bonus with the player's level", () => {
    const perLevel = fx({ effects: [{ kind: "stat", scope: "team", stat: "armor", flat: 2, per: "level" }] });
    const l6 = scoreAugment(aug("X"), board(basic.units, 6), data, "3-2", perLevel);
    const l9 = scoreAugment(aug("X"), board(basic.units, 9), data, "3-2", perLevel);
    expect(l9.statLift).toBeGreaterThan(l6.statLift);
  });

  it("averages a timed bonus over the fight: a delayed bonus is worth less than one from the start", () => {
    const now = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "damageAmp", pct: 0.2 }] }));
    const late = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "damageAmp", pct: 0.2, delay: 12 }] }));
    expect(late.statLift).toBeLessThan(now.statLift);
    expect(late.statLift).toBeGreaterThan(0);
    expect(late.caveats.join(" ")).toMatch(/timed bonus/);
  });

  it("counts an unclear scope at half and says so", () => {
    const clear = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "health", pct: 0.2 }] }));
    const unclear = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "unknown", stat: "health", pct: 0.2 }] }));
    expect(unclear.statLift).toBeCloseTo(clear.statLift * ASSUMPTIONS.unknownScopeCoverage, 6);
    expect(unclear.confidence).toBe("medium"); // high -> medium
    expect(unclear.caveats.join(" ")).toMatch(/isn't clear/);
  });

  it("lets a trade-off come out negative (start combat at 80% health for damage)", () => {
    const glass = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [
      { kind: "stat", scope: "back", rows: 1, stat: "health", pct: -0.2 },
      { kind: "stat", scope: "back", rows: 1, stat: "damageAmp", pct: 0.05 },
    ] }));
    expect(glass.statLift).toBeLessThan(0);
  });
});

describe("augment score: resources", () => {
  const gold = (n: number) => fx({ resources: { gold: n } });
  // A board of cheap units short of 3 stars: it wants copies, so gold has something to buy.
  const wants = board([placed("Tank", 0, [], 2), placed("Carry", 3 * COLS, ["IE", "JG"], 2)], 7);

  it("values more gold more, and gold at 2-1 more than the same gold at 4-2", () => {
    const small = scoreAugment(aug("X"), wants, data, "3-2", gold(10));
    const big = scoreAugment(aug("X"), wants, data, "3-2", gold(40));
    expect(big.dp!).toBeGreaterThan(small.dp!);
    const stage = fx({ resources: { gold: 5 }, recurring: { gold: { perStage: 7 } } });
    expect(scoreAugment(aug("X"), wants, data, "2-1", stage).dp!).toBeGreaterThan(scoreAugment(aug("X"), wants, data, "4-2", stage).dp!);
  });

  it("buys copies the board wants, and says what's left over is idle", () => {
    const r = scoreAugment(aug("X"), wants, data, "3-2", gold(30));
    expect(r.parts[0].note).toMatch(/copies the board wants/);
    // A board with nothing left to buy can only bank the gold.
    const done = board([placed("Tank", 0, [], 3), placed("Carry", 1, [], 3)], 9);
    const idle = scoreAugment(aug("X"), done, data, "3-2", gold(30));
    expect(idle.parts[0].note).toMatch(/idle gold/);
    expect(idle.dp!).toBeLessThan(r.dp! / 3);
  });

  it("counts a component as half an item and a completed item as one", () => {
    const comp = scoreAugment(aug("X"), wants, data, "3-2", fx({ resources: { components: 2 } }));
    const done = scoreAugment(aug("X"), wants, data, "3-2", fx({ resources: { completed: 1 } }));
    expect(comp.dp!).toBeCloseTo(done.dp!, 6);
  });

  it("scales a fraction of an item smoothly (a lone component is half an item, not a whole one)", () => {
    const at = (n: number) => scoreAugment(aug("X"), wants, data, "3-2", fx({ resources: { completed: n } })).dp!;
    expect(at(0.5)).toBeCloseTo(at(1) / 2, 2); // (the soft cap bends it slightly)
    const series = [0.5, 1, 1.5, 2, 3, 5].map(at);
    for (let i = 1; i < series.length; i++) expect(series[i]).toBeGreaterThan(series[i - 1]);
  });

  it("values an item by who can hold it: a 3-star carry before a 1-star filler", () => {
    const carry = board([placed("Carry", 21, ["A", "B", "C"], 3), placed("Tank", 0, [], 3)], 8); // only the 3-star tank has room
    const filler = board([placed("Carry", 21, ["A", "B", "C"], 1), placed("Tank", 0, [], 1)], 8); // only the 1-star tank has room
    const item = fx({ resources: { completed: 1 } });
    const onStrong = scoreAugment(aug("X"), carry, data, "3-2", item).dp!;
    const onWeak = scoreAugment(aug("X"), filler, data, "3-2", item).dp!;
    expect(itemGain(2, 3)).toBeGreaterThan(itemGain(2, 1));
    expect(onStrong).toBeGreaterThan(onWeak);
  });

  it("an item is never worth nothing, even when every carrier is full", () => {
    const full = board([placed("Carry", 21, ["A", "B", "C"], 3), placed("Tank", 0, ["A", "B", "C"], 3)], 8);
    const r = scoreAugment(aug("X"), full, data, "3-2", fx({ resources: { completed: 1 } }));
    expect(r.dp!).toBeGreaterThan(0.1 * ITEM_GAIN * ASSUMPTIONS.calibration);
  });

  it("values XP less the higher the level", () => {
    const xp = fx({ resources: { xp: 10 } });
    const at = (level: number) => scoreAugment(aug("X"), board(wants.units, level), data, "3-2", xp).dp!;
    expect(at(6)).toBeGreaterThan(at(8));
    expect(at(8)).toBeGreaterThan(at(9));
    expect(at(10)).toBe(0);
  });

  it("combines stat and resource effects into one breakdown that adds up", () => {
    const both = scoreAugment(aug("X"), wants, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "health", pct: 0.1 }], resources: { gold: 10 } }));
    expect(both.parts.length).toBeGreaterThanOrEqual(2);
    expect(both.parts.some((p) => p.lift !== undefined)).toBe(true);
    expect(both.dp!).toBeCloseTo(both.parts.reduce((a, p) => a + p.dp, 0), 9);
  });

  it("squeezes very large totals: nothing is worth more than the cap, and the order holds", () => {
    const huge = scoreAugment(aug("X"), wants, data, "2-1", fx({ resources: { completed: 40 } }));
    const bigger = scoreAugment(aug("X"), wants, data, "2-1", fx({ resources: { completed: 80 } }));
    expect(huge.dp!).toBeLessThanOrEqual(ASSUMPTIONS.softCap);
    expect(bigger.dp!).toBeGreaterThanOrEqual(huge.dp!);
    expect(huge.caveats.join(" ")).toMatch(/diminishing returns/);
  });
});

describe("augment score: traits", () => {
  const tdata = {
    ...data,
    units: [...(data as unknown as { units: unknown[] }).units, { ...unit("Lunar1", 2, 800, 50), traits: ["Lunar"] }, { ...unit("Lunar2", 3, 800, 50), traits: ["Lunar"] }],
    traits: [{ apiName: "T_Lunar", name: "Lunar", desc: "", breakpoints: [{ minUnits: 2, maxUnits: 3, style: 1, values: {} }] }],
  } as unknown as SetData;
  const effects: Record<string, AugmentEffects> = {
    X: { confidence: "high", traits: ["Lunar"], effects: [{ kind: "stat", scope: "trait", trait: "Lunar", stat: "health", pct: 0.3 }], resources: { gold: 10 } },
  };
  it("is worth little to a board that doesn't play the trait, and full price to one that does", () => {
    const off = scoreAugment(aug("X"), board([placed("Tank", 0), placed("Carry", 21, ["A"])], 8), tdata, "3-2", effects);
    const on = scoreAugment(aug("X"), board([placed("Lunar1", 0), placed("Lunar2", 1), placed("Carry", 21, ["A"])], 8), tdata, "3-2", effects);
    expect(off.dp!).toBeLessThan(0.2 * on.dp!);
    expect(off.caveats.join(" ")).toMatch(/doesn't play/);
    expect(on.statLift).toBeGreaterThan(0);
  });
  it("a trait bonus only reaches that trait's units", () => {
    const only = scoreAugment(aug("X"), board([placed("Tank", 0), placed("Carry", 21)], 8), tdata, "3-2", { X: { confidence: "high", effects: [{ kind: "stat", scope: "trait", trait: "Lunar", stat: "health", pct: 0.5 }] } });
    expect(only.statLift).toBe(0);
  });
});

describe("augment score: not scored and ranking", () => {
  it("doesn't score an augment it can't read, or an empty board", () => {
    expect(scoreAugment(aug("Missing"), basic, data, "3-2", {}).impact).toBeNull();
    expect(scoreAugment(aug("X"), basic, data, "3-2", fx({ confidence: "none", unparsed: ["Flip a coin"] })).caveats.join(" ")).toMatch(/not read: Flip a coin/);
    const empty = scoreAugment(aug("X"), board([]), data, "3-2", fx({ resources: { gold: 5 } }));
    expect(empty.impact).toBeNull();
    expect(empty.caveats.join(" ")).toMatch(/units on the board/);
  });

  it("ranks the most valuable first and unscored ones last", () => {
    const effects: Record<string, AugmentEffects> = {
      Small: { confidence: "high", resources: { gold: 3 } },
      Big: { confidence: "high", resources: { completed: 3 } },
      Unknown: { confidence: "none" },
    };
    const ranked = rankAugments([aug("Unknown"), aug("Small"), aug("Big")].map((a) => ({ ...a, name: a.apiName, desc: "", values: {} })) as never, basic, data, "3-2", effects);
    expect(ranked.map((r) => r.apiName)).toEqual(["Big", "Small", "Unknown"]);
  });
});

// The generated file is part of the feature: check that the extraction read what it should.
describe("extracted effects (Set 18)", () => {
  const get = (name: string) => EFFECTS[`DA_${name}`];

  it("reads a stat bundle", () => {
    const e = get("TonsOfStatsII")!;
    const byStat = (s: string) => e.effects!.find((x) => x.stat === s)!;
    expect(byStat("health").flat).toBe(88);
    expect(byStat("ad").pct).toBeCloseTo(0.08);
    expect(byStat("as").pct).toBeCloseTo(0.08);
    expect(e.confidence).toBe("high");
  });

  it("reads a base plus a per-level step as one flat bonus, not a per-level multiple", () => {
    const e = get("BodyguardTraining")!.effects!.find((x) => x.stat === "armor")!;
    expect(e.flat).toBe(15);
    expect(e.plusPerLevel).toBe(2);
    expect(e.per).toBeUndefined();
    // 15 + 2 x 9 = 33 armor at level 9: a few percent of strength, not the 135 a "per level" reading would give.
    const r = scoreAugment(aug("X"), board(basic.units, 9), data, "3-2", { X: { confidence: "high", effects: [e] } });
    const wrong = scoreAugment(aug("X"), board(basic.units, 9), data, "3-2", { X: { confidence: "high", effects: [{ ...e, plusPerLevel: undefined, per: "level" }] } });
    expect(r.statLift).toBeLessThan(wrong.statLift / 2);
  });

  it("reads gold now and gold each stage, and rerolls", () => {
    const e = get("MoneyHungryPlus")!;
    expect(e.resources!.gold).toBe(13);
    expect(e.recurring!.gold!.perStage).toBe(7);
    expect(get("RollingForDays")!.resources!.rerolls).toBe(10);
  });

  it("treats 'begin combat at 80% health' as a cost, not a bonus", () => {
    const e = get("GlassCannon_Gold")!;
    const hp = e.effects!.find((x) => x.stat === "health")!;
    expect(hp.pct).toBeCloseTo(-0.2);
    expect(hp.scope).toBe("back");
    expect(e.effects!.find((x) => x.stat === "damageAmp")!.pct).toBeCloseTo(0.25);
  });

  it("doesn't model augments that rewrite the board", () => {
    expect(get("Dummify")!.confidence).toBe("none");
  });

  it("covers most augments", () => {
    const all = Object.values(EFFECTS);
    const scored = all.filter((e) => e.confidence !== "none").length;
    expect(all.length).toBeGreaterThanOrEqual(240);
    expect(scored / all.length).toBeGreaterThan(0.85);
    for (const e of all) {
      for (const eff of e.effects ?? []) expect(eff.flat !== undefined || eff.pct !== undefined).toBe(true);
      for (const [k, v] of Object.entries(e.resources ?? {})) if (typeof v === "number") expect(v, k).toBeGreaterThanOrEqual(0);
    }
  });
});
