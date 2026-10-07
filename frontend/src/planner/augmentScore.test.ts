import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { AugmentEffects, ASSUMPTIONS, EFFECTS, boardValue, rankAugments, scoreAugment, teamPower } from "./augmentScore";
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
  const gold = fx({ resources: { gold: 20 } });

  it("prices gold higher early than late", () => {
    const early = scoreAugment(aug("X"), basic, data, "2-1", gold);
    const late = scoreAugment(aug("X"), basic, data, "4-2", gold);
    expect(early.ge!).toBeGreaterThan(late.ge!);
    expect(late.ge!).toBeLessThan(20); // spent imperfectly
  });

  it("counts recurring gold by the stages left", () => {
    const stage = fx({ resources: { gold: 5 }, recurring: { gold: { perStage: 7 } } });
    const early = scoreAugment(aug("X"), basic, data, "2-1", stage);
    const late = scoreAugment(aug("X"), basic, data, "4-2", stage);
    expect(early.parts.some((p) => /a stage/.test(p.label))).toBe(true);
    expect(early.ge!).toBeGreaterThan(late.ge! * 1.4);
  });

  // What a part is worth before the board's room limits it.
  const raw = (r: ReturnType<typeof scoreAugment>) => r.parts.filter((p) => !p.label.startsWith("less:")).reduce((a, p) => a + p.ge, 0);

  it("prices items, units and rerolls with the table", () => {
    const items = scoreAugment(aug("X"), basic, data, "3-2", fx({ resources: { components: 2, completed: 1 } }));
    expect(raw(items)).toBeCloseTo((2 * ASSUMPTIONS.ge.component + ASSUMPTIONS.ge.completed) * ASSUMPTIONS.usefulness.item, 6);
    const units = scoreAugment(aug("X"), basic, data, "3-2", fx({ resources: { units: [{ n: 1, cost: 3, star: 2 }] } }));
    expect(raw(units)).toBeCloseTo(3 * 3 * ASSUMPTIONS.usefulness.unit, 6);
  });

  it("expresses the total as a share of the board's value", () => {
    const r = scoreAugment(aug("X"), basic, data, "3-2", gold);
    const denominator = Math.max(boardValue(basic, data), ASSUMPTIONS.impactFloor * basic.level * ASSUMPTIONS.targetValuePerSlot);
    expect(r.impact!).toBeCloseTo(r.ge! / denominator, 9);
    // Tank 2★ (2 x 3) + Carry 2★ (4 x 3) + two items.
    expect(boardValue(basic, data)).toBe(2 * 3 + 4 * 3 + 2 * ASSUMPTIONS.ge.itemOnBoard);
  });

  it("combines stat and resource effects into one breakdown", () => {
    const both = scoreAugment(aug("X"), basic, data, "3-2", fx({ effects: [{ kind: "stat", scope: "team", stat: "health", pct: 0.1 }], resources: { gold: 10 } }));
    expect(both.parts.length).toBeGreaterThanOrEqual(2);
    expect(both.parts.some((p) => p.lift !== undefined)).toBe(true);
    expect(both.ge!).toBeCloseTo(both.parts.reduce((a, p) => a + p.ge, 0), 9);
  });
});

// The point of the estimate: the same augment is worth different amounts on different boards.
describe("augment score: depends on the board", () => {
  const fullBoard = board(
    [
      placed("Tank", 0, ["A", "B", "C"], 3),
      placed("Tank", 1, ["A", "B", "C"], 3),
      placed("Carry", 21, ["A", "B", "C"], 3),
      placed("Carry", 22, ["A", "B", "C"], 3),
      placed("Carry", 23, [], 3),
      placed("Tank", 2, [], 3),
      placed("Carry", 24, [], 3),
      placed("Tank", 3, [], 3),
      placed("Carry", 25, [], 3),
    ],
    9,
  );
  const sparse = board([placed("Tank", 0, [], 1)], 8);

  it("values the same gold less on a board with little room left", () => {
    const gold = fx({ resources: { gold: 30 } });
    const onSparse = scoreAugment(aug("X"), sparse, data, "3-2", gold);
    const onFull = scoreAugment(aug("X"), fullBoard, data, "3-2", gold);
    expect(onFull.ge!).toBeLessThan(onSparse.ge!);
    expect(onFull.caveats.join(" ")).toMatch(/limited by how much more value/);
  });

  it("values items by the free item slots, not just their price", () => {
    const items = fx({ resources: { completed: 3 } });
    const open = scoreAugment(aug("X"), board([placed("Carry", 21, [], 2), placed("Tank", 0, [], 2), placed("Carry", 22, [], 2)], 8), data, "3-2", items);
    const loaded = scoreAugment(aug("X"), board([placed("Carry", 21, ["A", "B", "C"], 2), placed("Tank", 0, ["A", "B", "C"], 2), placed("Carry", 22, ["A", "B", "C"], 2)], 8), data, "3-2", items);
    expect(loaded.ge!).toBeLessThan(open.ge!);
    expect(loaded.caveats.join(" ")).toMatch(/item slots/);
  });

  it("values XP less the higher the level", () => {
    const xp = fx({ resources: { xp: 10 } });
    const at = (level: number) => scoreAugment(aug("X"), board(basic.units, level), data, "3-2", xp).ge!;
    expect(at(6)).toBeGreaterThan(at(8));
    expect(at(8)).toBeGreaterThan(at(9));
    expect(at(10)).toBe(0);
  });

  it("no longer ranks the same augments first on every board", () => {
    const effects: Record<string, AugmentEffects> = {
      Economy: { confidence: "high", resources: { gold: 25, xp: 10 } },
      Stats: { confidence: "high", effects: [{ kind: "stat", scope: "team", stat: "health", pct: 0.3 }, { kind: "stat", scope: "team", stat: "damageAmp", pct: 0.15 }] },
    };
    const offered = ["Economy", "Stats"].map((n) => ({ apiName: n, name: n, tier: 2, desc: "", values: {} })) as never;
    const first = (b: Board) => rankAugments(offered, b, data, "3-2", effects)[0].apiName;
    expect(first(sparse)).toBe("Economy");
    expect(first(fullBoard)).toBe("Stats");
  });

  it("keeps a one-unit board's impact sane (no +700%)", () => {
    const r = scoreAugment(aug("X"), board([placed("Tank", 0, [], 1)], 4), data, "3-2", fx({ resources: { gold: 30 } }));
    expect(r.impact!).toBeLessThan(1.2);
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
      Big: { confidence: "high", resources: { gold: 30 } },
      Unknown: { confidence: "none" },
    };
    const ranked = rankAugments([aug("Unknown"), aug("Small"), aug("Big")].map((a) => ({ ...a, name: a.apiName, desc: "", values: {} })) as never, basic, data, "3-2", effects);
    expect(ranked.map((r) => r.apiName)).toEqual(["Big", "Small", "Unknown"]);
  });

  it("teamPower grows with stars and items", () => {
    const power = (b: Board) => {
      const r = scoreAugment(aug("X"), b, data, "3-2", fx({ resources: { gold: 1 } }));
      return r.impact;
    };
    expect(power(basic)).not.toBeNull();
    expect(teamPower([])).toBe(0);
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
