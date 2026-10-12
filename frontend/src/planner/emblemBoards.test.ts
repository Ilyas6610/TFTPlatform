import { describe, expect, it } from "vitest";
import { SetData } from "../api/client";
import { suggestEmblemBoards } from "./emblemBoards";

const stats = { hp: 800, damage: 60, armor: 40, magicResist: 40, attackSpeed: 0.7, critChance: 0.25, critMultiplier: 1.4, range: 1 };
const unit = (apiName: string, cost: number, traits: string[]) => ({ apiName, name: apiName, cost, traits, stats, ability: { name: "", desc: "", values: {} } });
const trait = (name: string, at: number[], styles: number[]) => ({
  apiName: `T_${name}`,
  name,
  desc: "",
  breakpoints: at.map((minUnits, i) => ({ minUnits, maxUnits: at[i + 1] ? at[i + 1] - 1 : 25000, style: styles[i], text: "" })),
});
// Wood: 2/4/6 (bronze, silver, gold); Fae: 2/4; Brawl: 2/4. Five Wood units exist, so 6 Wood needs an emblem.
const data = {
  setNumber: 18,
  version: "test",
  units: [
    unit("W1", 1, ["Wood"]), unit("W2", 2, ["Wood"]), unit("W3", 3, ["Wood", "Brawl"]), unit("W4", 4, ["Wood"]), unit("W5", 3, ["Wood", "Fae"]),
    unit("F1", 1, ["Fae"]), unit("F2", 2, ["Fae", "Brawl"]), unit("F3", 4, ["Fae"]),
    unit("B1", 1, ["Brawl"]), unit("B2", 3, ["Brawl"]), unit("X5", 5, ["Solo"]), unit("Y5", 5, ["Brawl"]),
  ],
  traits: [trait("Wood", [2, 4, 6], [1, 3, 5]), trait("Fae", [2, 4], [1, 3]), trait("Brawl", [2, 4], [1, 3]), trait("Solo", [1], [4])],
  augments: [],
  items: [
    { apiName: "WoodEmblem", name: "Wood Emblem", kind: "emblem" },
    { apiName: "FaeEmblem", name: "Fae Emblem", kind: "emblem" },
  ],
  wisps: [],
} as unknown as SetData;

const count = (b: ReturnType<typeof suggestEmblemBoards>[number], name: string) => b.traits.find((t) => t.name === name)?.count ?? 0;

describe("boards for held emblems", () => {
  it("reaches the emblem's trait: six Wood with five Wood units and a Wood Emblem", () => {
    const [best] = suggestEmblemBoards(["WoodEmblem"], 8, data);
    expect(count(best, "Wood")).toBe(6);
    expect(best.holders).toHaveLength(1);
    const holder = best.board.units.find((u) => u.id === best.holders[0].unit)!;
    expect(data.units.find((u) => u.apiName === holder.id)!.traits).not.toContain("Wood");
    expect(holder.items).toContain("WoodEmblem");
  });

  it("puts copies of one emblem on different units", () => {
    const [best] = suggestEmblemBoards(["WoodEmblem", "WoodEmblem"], 8, data);
    expect(new Set(best.holders.map((h) => h.unit)).size).toBe(2);
    expect(count(best, "Wood")).toBeGreaterThanOrEqual(6);
  });

  it("fills the level's slots and returns boards that differ", () => {
    for (const level of [7, 8, 9]) {
      const boards = suggestEmblemBoards(["FaeEmblem"], level, data);
      expect(boards.length).toBeGreaterThan(1);
      for (const b of boards) expect(b.board.units).toHaveLength(level);
      const [a, b] = boards.map((x) => new Set(x.board.units.map((u) => u.id)));
      expect([...a].every((id) => b.has(id))).toBe(false);
    }
  });

  it("builds around the emblems held: Fae boards reach 4 Fae", () => {
    const [best] = suggestEmblemBoards(["FaeEmblem"], 7, data);
    expect(count(best, "Fae")).toBeGreaterThanOrEqual(4);
  });
});
