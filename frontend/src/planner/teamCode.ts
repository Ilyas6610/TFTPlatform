// The in-game Team Planner's copy/paste codes. A code holds up to 10 units
// and nothing else: no positions, stars, items or augments.
//
//   <prefix> <10 slots> TFTSet<N>
//   prefix  "01" with 2 hex digits per slot, "02" with 3 digits per slot
//   slot    the champion's Team Planner code in hex, "00"/"000" for an empty one
//
// Each champion's code is published by CommunityDragon
// (tftchampions-teamplanner.json; the API serves it as /sets/{set}/planner-codes).
// Sets up to 17 have codes below 256; Set 18's start at 1001, which is why
// it uses 3 digits. The format is as reverse-engineered by community tools
// (for example github.com/zhenga8533/tfteam, src/features/builder/team-code.ts);
// Riot doesn't document it.

export const TEAM_CODE_SLOTS = 10;

const PATTERN = /^0[0-9a-f]([0-9a-f]+)TFTSet(\d+)$/i;

export interface EncodedTeam {
  code: string;
  /** Units that made it into the code. */
  included: string[];
  /** Units the planner doesn't know (no code for them). */
  unknown: string[];
  /** Units past the 10 the planner holds. */
  overflow: string[];
}

/** Builds a pasteable code from unit apiNames; each distinct unit takes one slot. */
export function encodeTeamCode(units: string[], codes: Record<string, number>, set: number): EncodedTeam {
  const wide = Object.values(codes).some((c) => c > 0xff);
  const digits = wide ? 3 : 2;
  const included: string[] = [];
  const unknown: string[] = [];
  const overflow: string[] = [];
  for (const u of new Set(units)) {
    if (!codes[u]) unknown.push(u);
    else if (included.length < TEAM_CODE_SLOTS) included.push(u);
    else overflow.push(u);
  }
  const slots = Array.from({ length: TEAM_CODE_SLOTS }, (_, i) =>
    (included[i] ? codes[included[i]] : 0).toString(16).padStart(digits, "0"),
  );
  return { code: `0${wide ? 2 : 1}${slots.join("")}TFTSet${set}`, included, unknown, overflow };
}

/** The set number a code names, without decoding its units; null if it isn't a code. */
export function teamCodeSet(code: string): number | null {
  const set = code.trim().match(PATTERN)?.[2];
  return set ? Number(set) : null;
}

export type DecodedTeam = { ok: true; set: number; units: string[] } | { ok: false; error: string };

/** Reads a code back into unit apiNames; the width per slot comes from the payload length. */
export function decodeTeamCode(code: string, codes: Record<string, number>): DecodedTeam {
  const m = code.trim().match(PATTERN);
  if (!m) return { ok: false, error: "That doesn't look like a Team Planner code." };
  const [, payload, set] = m;
  if (payload.length % TEAM_CODE_SLOTS !== 0) return { ok: false, error: "That Team Planner code is malformed." };
  const digits = payload.length / TEAM_CODE_SLOTS;
  const byCode = new Map(Object.entries(codes).map(([id, c]) => [c, id]));
  const units: string[] = [];
  for (let i = 0; i < payload.length; i += digits) {
    const value = parseInt(payload.slice(i, i + digits), 16);
    if (value === 0) continue;
    const id = byCode.get(value);
    if (!id) return { ok: false, error: "That code has a unit this set doesn't have. Is it from another set?" };
    if (!units.includes(id)) units.push(id);
  }
  return { ok: true, set: Number(set), units };
}
