// Prints what the augment estimate suggests for a spread of boards, for a person to read and judge:
//
//   npx vite-node scripts/augment-review.ts <set data json> [comp slug filter] [--now] [--n=6] [--explain=<augment name>]
//
// Boards: TFT Academy's guides from the dev half (see academy-check.ts; their final board read as the goal), with the
// augments the guide recommends marked "*" and listed after; never the held-out half.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { rankAugments } from "../src/planner/augmentScore";
import { augmentTier, availableAt } from "../src/planner/augmentStages";
import { Board } from "../src/planner/board";

const args = process.argv.slice(2).filter((a) => !a.endsWith("augment-review.ts"));
const [setFile, filter] = args.filter((a) => !a.startsWith("--"));
const mode = args.includes("--now") ? "now" : "goal";
if (process.env.RT) (await import("../src/planner/augmentScore")).ASSUMPTIONS.rerollTempo = Number(process.env.RT);
const N = Number(args.find((a) => a.startsWith("--n="))?.slice(4) ?? 6);
const explain = args.find((a) => a.startsWith("--explain="))?.slice(10);
const data = JSON.parse(readFileSync(setFile, "utf8"));
const ref = JSON.parse(readFileSync(join(__dirname, ".academy", "reference.json"), "utf8"));
const unitById = new Map<string, any>(data.units.map((u: any) => [u.apiName, u]));
const itemName = new Map<string, string>(data.items.map((i: any) => [i.apiName, i.name]));
const holdable = new Set(data.items.filter((i: any) => ["completed", "emblem", "artifact", "radiant"].includes(i.kind)).map((i: any) => i.apiName));
const base = (slug: string) => slug.replace(/-copy$/, "");
const hash = (s: string) => [...s].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7);
const comps = ref.comps.filter((c: any) => hash(base(c.slug)) % 2 === 0 && (!filter || c.slug.includes(filter)));

function toBoard(units: any[], level: number): Board {
  const used = new Set<number>();
  const placed = units.filter((u) => unitById.has(u.id)).map((u) => {
    let pos = typeof u.pos === "number" && u.pos >= 0 && u.pos < 28 && !used.has(u.pos) ? u.pos : -1;
    if (pos < 0) pos = [...Array(28).keys()].find((p) => !used.has(p))!;
    used.add(pos);
    return { id: u.id, star: Math.max(1, Math.min(3, u.star)) as 1 | 2 | 3, pos, items: u.items.filter((i: string) => holdable.has(i)).slice(0, 3), extra: [] };
  });
  return { set: data.setNumber, title: "", level, augments: [null, null, null], units: placed };
}

for (const c of comps) {
  const b = toBoard(c.final, Math.max(8, Math.min(10, c.final.length)));
  const desc = b.units.map((u) => `${unitById.get(u.id).name}${u.star > 1 ? ` ${u.star}★` : ""}(${unitById.get(u.id).cost})${u.items.length ? ` [${u.items.map((i) => itemName.get(i) ?? i).join(", ")}]` : ""}`).join("; ");
  console.log(`\n=== ${c.slug} (lv ${b.level})\n${desc}`);
  for (const [slot, st] of ["2-1", "3-2", "4-2"].entries()) {
    const pool = data.augments.filter((a: any) => availableAt(a, slot));
    const r = rankAugments(pool, b, data, st, undefined, [], mode as any).filter((x) => x.impact !== null);
    const top = r.slice(0, N).map((x) => `${c.augments.includes(x.apiName) ? "*" : ""}${x.name} ${x.impact!.toFixed(2)} {${x.parts.slice().sort((p, q) => Math.abs(q.dp) - Math.abs(p.dp)).slice(0, 1).map((p) => p.label.slice(0, 40)).join("")}}`);
    const recs = c.augments.map((id: string) => { const i = r.findIndex((x) => x.apiName === id); return i >= 0 ? `${r[i].name}#${i + 1}` : null; }).filter(Boolean);
    console.log(`  ${st}: ${top.join(" | ")}\n        guide: ${recs.join(", ")}`);
    const x = explain && r.find((y) => y.name === explain);
    if (x) console.log(`        ${x.name} #${r.indexOf(x) + 1} ${x.impact!.toFixed(3)}: ${x.parts.map((p) => `${p.label} ${p.dp.toFixed(3)}${p.note ? ` (${p.note})` : ""}`).join("; ")}`);
  }
}
