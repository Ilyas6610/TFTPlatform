// Checks how the augment estimate behaves on real stored boards. There are no augment
// outcomes to test it against (Riot's match data has none), so these are checks that
// don't need them; run after changing the model or after a patch:
//
//   npx vite-node scripts/augment-checks.ts <set data json> <boards.jsonl> [boards to use]
//
// (the same two files scripts/fit-board-value.mjs takes). It prints each check with PASS or
// FAIL, and exits 1 if one fails:
//   - designer tiers: Silver < Gold < Prismatic augments should score higher on average
//     (Spearman between tier and mean score), an independent signal that doesn't depend on how
//     the estimate was built;
//   - trait augments aren't recommended to boards that don't play the trait;
//   - sizes are plausible: the median augment moves placement by a tenth, none by more than the cap;
//   - the board matters: rankings differ between boards, and copy-giving augments (duplicators,
//     random champions, Pandora's Bench...) rank higher on boards of cheap carries than on boards of
//     expensive ones;
//   - the top pick isn't the same augment for every board.

import { readFileSync } from "node:fs";
import { ASSUMPTIONS, EFFECTS, rankAugments } from "../src/planner/augmentScore";
import { buildPlan } from "../src/planner/augmentPlan";
import { augmentTier, availableAt } from "../src/planner/augmentStages";
import { Board, computeTraits } from "../src/planner/board";

const [setFile, boardsFile, nArg] = process.argv.slice(2).filter((a) => !a.startsWith("--") && !a.endsWith("augment-checks.ts"));
if (!setFile || !boardsFile) throw new Error("usage: vite-node scripts/augment-checks.ts <set data json> <boards.jsonl> [n]");
const data = JSON.parse(readFileSync(setFile, "utf8"));
const unitById = new Map<string, any>(data.units.map((u: any) => [u.apiName, u]));
const holdable = new Set(data.items.filter((i: any) => ["completed", "emblem", "artifact", "radiant"].includes(i.kind)).map((i: any) => i.apiName));

// Boards: strong final boards (top 4, level 8+), tanks in front, the rest behind; a fixed shuffle so runs repeat.
let seed = 12345;
const rnd = () => (seed = (seed * 1664525 + 1013904223) % 4294967296) / 4294967296;
const rows = readFileSync(boardsFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
const good = rows.filter((r) => r.lv >= 8 && r.p <= 4 && r.u.length >= 7);
for (let i = good.length - 1; i > 0; i--) { const j = Math.floor(rnd() * (i + 1)); [good[i], good[j]] = [good[j], good[i]]; }
const boards: Board[] = good.slice(0, Number(nArg ?? 70)).map((r: any) => {
  const us = r.u.filter((u: any) => unitById.has(u.id)).sort((a: any, b: any) => (unitById.get(b.id).stats.hp ?? 0) - (unitById.get(a.id).stats.hp ?? 0));
  return { set: data.setNumber, title: "", level: r.lv, augments: [null, null, null], units: us.map((u: any, i: number) => ({ id: u.id, star: Math.max(1, Math.min(3, u.s)) as 1 | 2 | 3, pos: i < 3 ? i : 21 + (i - 3), items: u.i.filter((x: string) => holdable.has(x)).slice(0, 3), extra: [] })) };
});

const stages = ["2-1", "3-2", "4-2"];
const pools = [0, 1, 2].map((s) => data.augments.filter((a: any) => availableAt(a, s)));
const ranks = (xs: number[]) => { const idx = xs.map((v, i) => [v, i]).sort((a, b) => a[0] - b[0]); const r = new Array(xs.length); for (let i = 0; i < idx.length; ) { let j = i; while (j + 1 < idx.length && idx[j + 1][0] === idx[i][0]) j++; for (let k = i; k <= j; k++) r[idx[k][1]] = (i + j) / 2; i = j + 1; } return r; };
const spearman = (a: number[], b: number[]) => { const ra = ranks(a), rb = ranks(b), n = a.length; const ma = ra.reduce((x, y) => x + y) / n, mb = rb.reduce((x, y) => x + y) / n; let num = 0, da = 0, db = 0; for (let i = 0; i < n; i++) { num += (ra[i] - ma) * (rb[i] - mb); da += (ra[i] - ma) ** 2; db += (rb[i] - mb) ** 2; } return num / Math.sqrt(da * db); };
const isCopy = (id: string) => { const e: any = (EFFECTS as any)[id]; return !!(e?.resources?.duplicators || e?.resources?.benchTransform || e?.resources?.units?.length || e?.recurring?.copies || e?.resources?.allOfCost); };

const sum = new Map<string, { n: number; s: number; tier: number }>();
const top1 = new Map<string, number>();
const all: number[] = [];
const lowB: number[] = [], highB: number[] = [];
const midScores: Map<string, number>[] = [];
let traitTop = 0, traitMismatch = 0;
for (const b of boards) {
  const plan = buildPlan(b, data);
  const group = plan.lowCostShare >= 0.6 ? lowB : plan.lowCostShare <= 0.15 ? highB : null;
  const played = new Map(computeTraits(b, data).map((t) => [t.name, t.count]));
  for (let s = 0; s < 3; s++) {
    const r = rankAugments(pools[s], b, data, stages[s]);
    if (s === 1) midScores.push(new Map(r.map((x) => [x.apiName, x.impact ?? 0])));
    r.forEach((x, i) => {
      if (x.impact === null) return;
      all.push(x.impact);
      const e = sum.get(x.apiName) ?? { n: 0, s: 0, tier: augmentTier(data.augments.find((a: any) => a.apiName === x.apiName)) };
      e.n++; e.s += x.impact; sum.set(x.apiName, e);
      if (group && s < 2 && isCopy(x.apiName)) group.push(1 - i / r.length);
    });
    top1.set(r[0].name, (top1.get(r[0].name) ?? 0) + 1);
    for (const x of r.slice(0, 5)) {
      const traits: string[] = data.augments.find((a: any) => a.apiName === x.apiName)?.traits ?? [];
      if (traits.length) { traitTop++; if (traits.every((t) => (played.get(t) ?? 0) < ASSUMPTIONS.playedTraitUnits)) traitMismatch++; }
    }
  }
}
const known = [...sum.values()].filter((e) => e.tier > 0);
const tierRho = spearman(known.map((e) => e.tier), known.map((e) => e.s / e.n));
const tierMeans = [1, 2, 3].map((t) => { const m = known.filter((e) => e.tier === t).map((e) => e.s / e.n); return m.reduce((a, b) => a + b, 0) / Math.max(1, m.length); });
all.sort((a, b) => a - b);
const q = (p: number) => all[Math.floor(p * (all.length - 1))];
const mean = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / Math.max(1, xs.length);
let agree = 0, an = 0;
for (let i = 0; i + 1 < midScores.length; i += 2) { const keys = [...midScores[i].keys()]; agree += spearman(keys.map((k) => midScores[i].get(k)!), keys.map((k) => midScores[i + 1].get(k)!)); an++; }
const copyDelta = mean(lowB) - mean(highB);
const distinct = top1.size;

const checks: [string, boolean, string][] = [
  ["tier order: Silver < Gold < Prismatic means", tierMeans[0] < tierMeans[1] && tierMeans[1] < tierMeans[2], `means ${tierMeans.map((x) => x.toFixed(3)).join(" < ")}`],
  ["tier correlation (Spearman, designer tier vs mean score) >= 0.25", tierRho >= 0.25, tierRho.toFixed(3)],
  ["trait augments in the top 5 only for boards that play the trait", traitMismatch === 0, `${traitMismatch} of ${traitTop} mismatched`],
  ["median augment moves placement by 0.03-0.2", q(0.5) >= 0.03 && q(0.5) <= 0.2, `median ${q(0.5).toFixed(3)}, p90 ${q(0.9).toFixed(2)}, p99 ${q(0.99).toFixed(2)}`],
  ["no augment above the cap", q(1) <= ASSUMPTIONS.softCap + 1e-9, `max ${q(1).toFixed(2)} (cap ${ASSUMPTIONS.softCap})`],
  ["rankings differ between boards (mean Spearman between two boards < 0.97)", agree / an < 0.97, (agree / an).toFixed(3)],
  ["copy-giving augments rank higher on cheap-carry boards than on expensive-carry boards", copyDelta >= 0.05, `${mean(lowB).toFixed(3)} vs ${mean(highB).toFixed(3)} (n ${lowB.length / 1} / ${highB.length} scores)`],
  ["at least 5 different top picks across boards and stages", distinct >= 5, `${distinct} distinct over ${boards.length * 3}`],
];
console.log(`${boards.length} real boards x 3 stages; ${sum.size} augments scored`);
let failed = 0;
for (const [name, ok, detail] of checks) { console.log(`${ok ? "PASS" : "FAIL"}  ${name}: ${detail}`); if (!ok) failed++; }
console.log("most common top picks: " + [...top1.entries()].sort((a, b) => b[1] - a[1]).slice(0, 5).map(([n, c]) => `${n} ${c}`).join(", "));
process.exit(failed ? 1 : 0);
