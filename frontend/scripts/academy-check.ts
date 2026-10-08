// Compares the augment estimate with TFT Academy's comp guides: for each guide's board,
// where do the augments its authors recommend land in our ranking of what that stage offers?
//
//   node scripts/fetch-academy-reference.mjs                         # once; local, gitignored
//   npx vite-node scripts/academy-check.ts <set data json> [--holdout]
//
// It's a reference, not ground truth (a guide lists augments that suit the comp, not a ranking,
// and its authors weigh things the model can't see), so it is never used to fit numbers. To keep
// it from becoming a target anyway, the comps are split in two by name: change the model looking
// at the dev half only, and read the held-out half (--holdout) once, at the end.
//
// Per stage it reports, for the recommended augments that stage can offer:
//   - mean percentile in our ranking (0 = our top pick, 0.5 = random);
//   - recall@12: the share of them in our visible top 12;
//   - hit@3: the share of boards with at least one of them in our top 3;
// against two baselines: ranking by designer tier, and by the model's own average score over all
// guide boards (board-blind; if the per-board ranking doesn't beat it, reading the board adds nothing).

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { rankAugments } from "../src/planner/augmentScore";
import { augmentTier, availableAt } from "../src/planner/augmentStages";
import { Board } from "../src/planner/board";

const args = process.argv.slice(2).filter((a) => !a.endsWith("academy-check.ts"));
const setFile = args.find((a) => !a.startsWith("--"));
if (!setFile) throw new Error("usage: vite-node scripts/academy-check.ts <set data json> [--holdout]");
const holdout = args.includes("--holdout");
const data = JSON.parse(readFileSync(setFile, "utf8"));
const ref = JSON.parse(readFileSync(join(__dirname, ".academy", "reference.json"), "utf8"));
const unitById = new Map<string, any>(data.units.map((u: any) => [u.apiName, u]));
const holdable = new Set(data.items.filter((i: any) => ["completed", "emblem", "artifact", "radiant"].includes(i.kind)).map((i: any) => i.apiName));
const augByName = new Map<string, any>(data.augments.map((a: any) => [a.apiName, a]));

// Split by the comp's name without "-copy", so a guide and its variant share a half.
const base = (slug: string) => slug.replace(/-copy$/, "");
const hash = (s: string) => [...s].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7);
const comps = ref.comps.filter((c: any) => (hash(base(c.slug)) % 2 === 1) === holdout);

function toBoard(units: any[], level: number): Board {
  const used = new Set<number>();
  const placed = units
    .filter((u) => unitById.has(u.id))
    .map((u) => {
      let pos = typeof u.pos === "number" && u.pos >= 0 && u.pos < 28 && !used.has(u.pos) ? u.pos : -1;
      if (pos < 0) pos = [...Array(28).keys()].find((p) => !used.has(p))!;
      used.add(pos);
      return { id: u.id, star: Math.max(1, Math.min(3, u.star)) as 1 | 2 | 3, pos, items: u.items.filter((i: string) => holdable.has(i)).slice(0, 3), extra: [] };
    });
  return { set: data.setNumber, title: "", level, augments: [null, null, null], units: placed };
}

const STAGES = [
  { label: "2-1", slot: 0, board: (c: any) => toBoard(c.early, Math.max(4, Math.min(6, c.early.length))) },
  { label: "3-2", slot: 1, board: (c: any) => toBoard(c.final, 7) },
  { label: "4-2", slot: 2, board: (c: any) => toBoard(c.final, Math.max(8, Math.min(10, c.final.length))) },
];

type Row = { comp: string; ranked: string[]; scores: Map<string, number>; recs: string[] };
const pct = (list: string[], id: string) => list.indexOf(id) / Math.max(1, list.length - 1);
const mean = (xs: number[]) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : NaN);
const f = (x: number) => (Number.isNaN(x) ? "  -  " : x.toFixed(3));

console.log(`TFT Academy patch ${ref.patch}, fetched ${ref.fetchedAt.slice(0, 10)}; ${holdout ? "HELD-OUT" : "dev"} half: ${comps.length} of ${ref.comps.length} comps`);
let unknownRecs = 0;
for (const st of STAGES) {
  const pool = data.augments.filter((a: any) => availableAt(a, st.slot));
  const rows: Row[] = [];
  for (const c of comps) {
    const b = st.board(c);
    if (b.units.length === 0) continue;
    const r = rankAugments(pool, b, data, st.label).filter((x) => x.impact !== null);
    const ranked = r.map((x) => x.apiName);
    const recs = c.augments.filter((id: string) => ranked.includes(id));
    unknownRecs += c.augments.filter((id: string) => !augByName.has(id)).length;
    if (recs.length) rows.push({ comp: c.slug, ranked, scores: new Map(r.map((x) => [x.apiName, x.impact!])), recs });
  }
  // Board-blind baseline: each augment's mean score over the same boards.
  const meanScore = new Map<string, number>();
  for (const row of rows) for (const [id, s] of row.scores) meanScore.set(id, (meanScore.get(id) ?? 0) + s / rows.length);
  const blind = (row: Row) => [...row.ranked].sort((a, b) => (meanScore.get(b) ?? 0) - (meanScore.get(a) ?? 0));
  const byTier = (row: Row) => [...row.ranked].sort((a, b) => augmentTier(augByName.get(b)) - augmentTier(augByName.get(a)));

  const measure = (order: (row: Row) => string[]) => {
    const p: number[] = [], recall: number[] = [], hit: number[] = [];
    for (const row of rows) {
      const list = order(row);
      for (const id of row.recs) { p.push(pct(list, id)); recall.push(list.indexOf(id) < 12 ? 1 : 0); }
      hit.push(row.recs.some((id) => list.indexOf(id) < 3) ? 1 : 0);
    }
    return { p: mean(p), recall: mean(recall), hit: mean(hit) };
  };
  const m = measure((row) => row.ranked), bb = measure(blind), t = measure(byTier);
  const nRecs = rows.reduce((n, r) => n + r.recs.length, 0);
  console.log(`\n${st.label}: ${rows.length} boards, ${nRecs} recommended augments offered at this stage (pool ${pool.length})`);
  console.log(`                    mean pct (lower better)  recall@12  hit@3`);
  console.log(`  model             ${f(m.p)}                    ${f(m.recall)}      ${f(m.hit)}`);
  console.log(`  board-blind mean  ${f(bb.p)}                    ${f(bb.recall)}      ${f(bb.hit)}`);
  console.log(`  designer tier     ${f(t.p)}                    ${f(t.recall)}      ${f(t.hit)}`);
  console.log(`  random            0.500                    ${f(12 / Math.max(1, pool.length))}`);
  // The recommended augments we rank lowest, across boards: where to look next.
  const worst = new Map<string, number[]>();
  for (const row of rows) for (const id of row.recs) (worst.get(id) ?? worst.set(id, []).get(id)!).push(pct(row.ranked, id));
  const w = [...worst].filter(([, v]) => v.length >= 2).map(([id, v]) => [augByName.get(id)?.name ?? id, mean(v), v.length] as const).sort((a, b) => b[1] - a[1]).slice(0, 6);
  console.log(`  recommended but ranked low (name, mean pct, guides): ${w.map(([n, p, k]) => `${n} ${p.toFixed(2)}×${k}`).join(", ")}`);
}
if (unknownRecs) console.log(`\n(${unknownRecs} recommended augment ids aren't in this set data)`);
