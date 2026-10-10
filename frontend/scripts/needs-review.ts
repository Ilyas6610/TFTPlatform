// Walks boards through 2-1 -> 3-2 -> 4-2 with the needs model (planner/augmentNeeds.ts): at each stage the top pick
// is taken and becomes an earlier pick for the next stage. For reading, not fitting.
//
//   npx vite-node scripts/needs-review.ts <set data json> <comps json from /api/v1/meta/comps> [n comps] [--first=<augment name>]
import { readFileSync } from "node:fs";
import { rankByNeeds, needsOf } from "../src/planner/augmentNeeds";
import { availableAt } from "../src/planner/augmentStages";
import { Board } from "../src/planner/board";

const args = process.argv.slice(2).filter((a) => !a.endsWith("needs-review.ts"));
const [setFile, compsFile, nArg] = args.filter((a) => !a.startsWith("--"));
const first = args.find((a) => a.startsWith("--first="))?.slice(8);
const data = JSON.parse(readFileSync(setFile, "utf8"));
const comps = JSON.parse(readFileSync(compsFile, "utf8")).comps.slice(0, Number(nArg ?? 50));
const unitById = new Map<string, any>(data.units.map((u: any) => [u.apiName, u]));
const holdable = new Set(data.items.filter((i: any) => ["completed", "emblem", "artifact", "radiant"].includes(i.kind)).map((i: any) => i.apiName));
const STAGES = ["2-1", "3-2", "4-2"];
const pools = [0, 1, 2].map((s) => data.augments.filter((a: any) => availableAt(a, s)));
const kind = (label: string) => label.split(":")[0];

const tally: Record<string, Record<string, number>> = {};
for (const c of comps) {
  const us = c.board.filter((u: any) => unitById.has(u.id));
  const front = us.filter((u: any) => !u.items.length || (unitById.get(u.id).stats?.range ?? 1) <= 1);
  let pos = 0;
  const units = us.map((u: any) => ({ id: u.id, star: u.star, pos: front.includes(u) ? pos++ : 21 + (pos++ % 7), items: u.items.filter((i: string) => holdable.has(i)).slice(0, 3), extra: [] }));
  const slots = us.reduce((a: number, u: any) => a + (u.id.includes("ElderDragon") ? 2 : 1), 0);
  const board: Board = { set: data.setNumber, title: "", level: Math.max(7, Math.min(10, slots)), augments: [null, null, null], units };
  const needs = needsOf(board, data);
  const names = us.map((u: any) => `${unitById.get(u.id).name.replace(/\s*\(.*\)$/, "")}${u.star > 1 ? u.star + "★" : ""}${u.items.length ? "◆" + u.items.length : ""}`).join(" ");
  console.log(`\n=== ${needs.label} · ${names}\n    ${needs.notes.join(" · ")}`);
  const given: string[] = [];
  const path: string[] = [];
  for (let s = 0; s < 3; s++) {
    if (s === 0 && first) {
      const a = data.augments.find((x: any) => x.name === first);
      given.push(a.apiName);
      path.push(`${first} (given)`);
      console.log(`  2-1: ${first} (given)`);
      continue;
    }
    const r = rankByNeeds(pools[s].filter((a: any) => !given.includes(a.apiName)), board, data, STAGES[s], given).filter((x) => x.impact !== null);
    const top = r[0];
    const mainKind = (x: any) => kind([...x.parts].sort((p: any, q: any) => q.dp - p.dp)[0]?.label ?? "?");
    console.log(`  ${STAGES[s]}: ${r.slice(0, 6).map((x) => `${x.name} ${x.impact!.toFixed(2)} [${mainKind(x)}]`).join(" | ")}`);
    given.push(top.apiName);
    path.push(mainKind(top));
  }
  const key = path.join(" → ");
  (tally[needs.label] ??= {})[key] = ((tally[needs.label] ??= {})[key] ?? 0) + 1;
}
console.log("\n=== paths by archetype");
for (const [a, t] of Object.entries(tally)) console.log(a, JSON.stringify(t));
