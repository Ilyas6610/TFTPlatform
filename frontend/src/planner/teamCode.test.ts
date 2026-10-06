import { describe, expect, it } from "vitest";
import { TEAM_CODE_SLOTS, decodeTeamCode, encodeTeamCode, teamCodeSet } from "./teamCode";

// Set 18 style: codes above 255, so three digits per slot.
const wide = { Ahri: 1001, Ashe: 1008, Zyra: 1084 };
// Older sets: codes below 256, two digits.
const narrow = { A: 1, B: 10, C: 255 };

describe("encodeTeamCode", () => {
  it("uses prefix 02 and three hex digits per slot when codes exceed 255", () => {
    const t = encodeTeamCode(["Ahri", "Ashe"], wide, 18);
    expect(t.code).toBe(`02${"3e9"}${"3f0"}${"000".repeat(8)}TFTSet18`);
    expect(t.included).toEqual(["Ahri", "Ashe"]);
  });

  it("uses prefix 01 and two digits for older sets", () => {
    expect(encodeTeamCode(["A", "B"], narrow, 13).code).toBe(`01${"01"}${"0a"}${"00".repeat(8)}TFTSet13`);
    expect(encodeTeamCode(["C"], narrow, 13).code).toBe(`01ff${"00".repeat(9)}TFTSet13`);
  });

  it("an empty team is all empty slots", () => {
    expect(encodeTeamCode([], wide, 18).code).toBe(`02${"000".repeat(10)}TFTSet18`);
  });

  it("each distinct unit takes one slot, however many copies", () => {
    expect(encodeTeamCode(["Ahri", "Ahri", "Ashe", "Ahri"], wide, 18).included).toEqual(["Ahri", "Ashe"]);
  });

  it("reports units the planner doesn't know and units past ten", () => {
    const many = Object.fromEntries(Array.from({ length: 12 }, (_, i) => [`U${i}`, 1001 + i]));
    const t = encodeTeamCode([...Object.keys(many), "Nope"], many, 18);
    expect(t.included).toHaveLength(TEAM_CODE_SLOTS);
    expect(t.overflow).toEqual(["U10", "U11"]);
    expect(t.unknown).toEqual(["Nope"]);
    expect(t.code.length).toBe(2 + 3 * TEAM_CODE_SLOTS + "TFTSet18".length);
  });
});

describe("decodeTeamCode", () => {
  it("round-trips", () => {
    const t = encodeTeamCode(["Zyra", "Ahri"], wide, 18);
    expect(decodeTeamCode(t.code, wide)).toEqual({ ok: true, set: 18, units: ["Zyra", "Ahri"] });
  });

  it("reads the classic two-digit format and either letter case", () => {
    expect(decodeTeamCode(`01010A${"00".repeat(8)}TFTSet13`, narrow)).toEqual({ ok: true, set: 13, units: ["A", "B"] });
    expect(decodeTeamCode(`  02${"3E9"}${"000".repeat(9)}tftset18 `, wide)).toEqual({ ok: true, set: 18, units: ["Ahri"] });
  });

  it("rejects garbage, a wrong length and unknown units", () => {
    expect(decodeTeamCode("hello", wide).ok).toBe(false);
    expect(decodeTeamCode(`02${"3e9"}TFTSet18`, wide).ok).toBe(false);
    expect(decodeTeamCode(`02${"fff"}${"000".repeat(9)}TFTSet18`, wide).ok).toBe(false);
    expect(decodeTeamCode("", wide).ok).toBe(false);
  });
});

describe("teamCodeSet", () => {
  it("names the set without decoding the units", () => {
    expect(teamCodeSet(`01${"01".repeat(10)}TFTSet9`)).toBe(9);
    expect(teamCodeSet(` 02${"3e9".repeat(10)}TFTSet18 `)).toBe(18);
    expect(teamCodeSet("nope")).toBeNull();
  });
});
