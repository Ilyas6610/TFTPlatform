// Fits the board value model the augment estimate prices things with: how
// much a final board's placement shifts, within its lobby, with the stars of its
// units (by cost), its items and its level. Writes src/planner/boardValue.data.json.
//
//   # 1. export final boards (a set's ranked games) from the database:
//   docker exec deploy-postgres-1 psql -U tft -d tft -tAc "copy (select json_build_object(
//     'm', mp.match_id, 'p', mp.placement, 'lv', mp.level, 't', m.game_datetime,
//     'u', (select coalesce(json_agg(json_build_object('id', e->>'character_id', 's', (e->>'tier')::int,
//           'i', coalesce(e->'itemNames', '[]'::jsonb))), '[]'::json) from jsonb_array_elements(mp.units) e))
//     from match_participants mp join matches m using (match_id)
//     where m.tft_set_number = 18 and m.queue_id = 1100) to stdout" > boards.jsonl
//   # 2. fit:
//   node scripts/fit-board-value.mjs --boards boards.jsonl --set set18.json     # /sets/18/data body
//
// Items are split by the class of their holder (a unit's cost and star), so an item is worth more on a 3-star carry than on a
// 1-star filler. The model is a ridge regression on lobby-centred features (every board has
// its lobby's average subtracted), so skill differences between lobbies and
// patch-wide effects drop out and only "better than the other seven" is
// learned. It is correlational (strong players hold more items and 3-stars),
// which is why the augment estimate only uses it as an exchange rate between
// items, star-ups and stats, and why the held-out ranking accuracy is
// printed and stored with the coefficients. Refit after a patch or a new set.

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const OUT = join(here, "../src/planner/boardValue.data.json");
const args = process.argv.slice(2);
const opt = (n) => (args.includes(n) ? args[args.indexOf(n) + 1] : null);
const boardsFile = opt("--boards");
const setFile = opt("--set");
if (!boardsFile || !setFile) throw new Error("usage: fit-board-value.mjs --boards boards.jsonl --set set18.json");

const set = JSON.parse(readFileSync(setFile, "utf8"));
const unitCost = new Map(set.units.map((u) => [u.apiName, u.cost]));
const holdable = new Set(set.items.filter((i) => ["completed", "emblem", "artifact", "radiant"].includes(i.kind)).map((i) => i.apiName));

// Features: units by cost (1-5) and star (1-3), the items held by units of each cost and star, level.
const CLASSES = [1, 2, 3, 4, 5].flatMap((c) => [1, 2, 3].map((s) => `cost${c}star${s}`));
export const FEATURES = [...CLASSES, ...CLASSES.map((k) => `items_${k}`), "level"];

function features(b) {
  const f = new Array(FEATURES.length).fill(0);
  for (const u of b.u) {
    const cost = unitCost.get(u.id);
    if (!cost) continue;
    const k = (cost - 1) * 3 + Math.min(3, Math.max(1, u.s)) - 1;
    f[k]++;
    for (const it of u.i) if (holdable.has(it)) f[15 + k]++;
  }
  f[30] = b.lv;
  return f;
}

const byMatch = new Map();
for (const line of readFileSync(boardsFile, "utf8").split("\n")) {
  if (!line.trim()) continue;
  const b = JSON.parse(line);
  if (!byMatch.has(b.m)) byMatch.set(b.m, []);
  byMatch.get(b.m).push(b);
}
const lobbies = [...byMatch.values()].filter((l) => l.length === 8).sort((a, b) => a[0].t.localeCompare(b[0].t));
if (lobbies.length < 200) throw new Error(`only ${lobbies.length} full lobbies: too few to fit`);

function centred(lobby) {
  const F = lobby.map(features);
  const mean = F[0].map((_, j) => F.reduce((a, r) => a + r[j], 0) / F.length);
  const y = lobby.map((b) => b.p);
  const ym = y.reduce((a, v) => a + v, 0) / y.length;
  return { X: F.map((r) => r.map((v, j) => v - mean[j])), y: y.map((v) => v - ym), F, p: y };
}

function ridge(ls, lambda) {
  const d = FEATURES.length;
  const A = Array.from({ length: d }, (_, i) => Array.from({ length: d }, (_, j) => (i === j ? lambda : 0)));
  const bv = new Array(d).fill(0);
  for (const l of ls) {
    const { X, y } = centred(l);
    for (let r = 0; r < X.length; r++)
      for (let i = 0; i < d; i++) {
        bv[i] += X[r][i] * y[r];
        for (let j = 0; j < d; j++) A[i][j] += X[r][i] * X[r][j];
      }
  }
  // Gaussian elimination
  for (let i = 0; i < d; i++) {
    let piv = i;
    for (let r = i + 1; r < d; r++) if (Math.abs(A[r][i]) > Math.abs(A[piv][i])) piv = r;
    [A[i], A[piv]] = [A[piv], A[i]];
    [bv[i], bv[piv]] = [bv[piv], bv[i]];
    for (let r = i + 1; r < d; r++) {
      const m = A[r][i] / A[i][i];
      for (let c = i; c < d; c++) A[r][c] -= m * A[i][c];
      bv[r] -= m * bv[i];
    }
  }
  const w = new Array(d).fill(0);
  for (let i = d - 1; i >= 0; i--) {
    let s = bv[i];
    for (let c = i + 1; c < d; c++) s -= A[i][c] * w[c];
    w[i] = s / A[i][i];
  }
  return w;
}

const ranks = (xs) => {
  const idx = xs.map((v, i) => [v, i]).sort((a, b) => a[0] - b[0]);
  const r = new Array(xs.length);
  for (let i = 0; i < idx.length; ) {
    let j = i;
    while (j + 1 < idx.length && idx[j + 1][0] === idx[i][0]) j++;
    for (let k = i; k <= j; k++) r[idx[k][1]] = (i + j) / 2;
    i = j + 1;
  }
  return r;
};
const spearman = (a, b) => {
  const ra = ranks(a), rb = ranks(b), n = a.length;
  const ma = ra.reduce((x, y) => x + y) / n, mb = rb.reduce((x, y) => x + y) / n;
  let num = 0, da = 0, db = 0;
  for (let i = 0; i < n; i++) {
    num += (ra[i] - ma) * (rb[i] - mb);
    da += (ra[i] - ma) ** 2;
    db += (rb[i] - mb) ** 2;
  }
  return da && db ? num / Math.sqrt(da * db) : 0;
};

function evaluate(ls, w) {
  let rho = 0, pair = 0, pn = 0;
  for (const l of ls) {
    const { F, p } = centred(l);
    const score = F.map((r) => -r.reduce((a, v, j) => a + v * w[j], 0));
    rho += spearman(score, p.map((v) => -v));
    for (let i = 0; i < 8; i++)
      for (let j = i + 1; j < 8; j++) {
        if (p[i] === p[j] || score[i] === score[j]) continue;
        pn++;
        if (score[i] > score[j] === p[i] < p[j]) pair++;
      }
  }
  return { spearman: rho / ls.length, pairwise: pair / pn };
}

// Honest check: fit on the oldest 60% of lobbies, test on the newest 40%.
const cut = Math.floor(lobbies.length * 0.6);
const heldOut = evaluate(lobbies.slice(cut), ridge(lobbies.slice(0, cut), 1));
// Baselines the same test: what the board's level alone, or its gold cost, would predict.
const baseline = (fn) => {
  let rho = 0;
  for (const l of lobbies.slice(cut)) rho += spearman(l.map(fn), l.map((b) => -b.p));
  return rho / (lobbies.length - cut);
};
const goldCost = (b) => b.u.reduce((a, u) => a + (unitCost.get(u.id) ?? 0) * 3 ** (Math.min(3, Math.max(1, u.s)) - 1), 0);

const w = ridge(lobbies, 1);
const coef = Object.fromEntries(FEATURES.map((n, i) => [n, +w[i].toFixed(4)]));
// The average value of one more item across the boards seen (weighted by how often each holder class holds one).
let itemFreq = new Array(15).fill(0);
for (const l of lobbies) for (const b of l) { const f = features(b); for (let k = 0; k < 15; k++) itemFreq[k] += f[15 + k]; }
const itemTotal = itemFreq.reduce((a, v) => a + v, 0);
const itemGainMean = -itemFreq.reduce((a, v, k) => a + v * w[15 + k], 0) / itemTotal;
const out = {
  setNumber: set.setNumber,
  version: set.version,
  fittedOn: { lobbies: lobbies.length, boards: lobbies.length * 8, from: lobbies[0][0].t, to: lobbies[lobbies.length - 1][0].t },
  heldOut: { note: "fit on the oldest 60% of lobbies, tested on the newest 40%", ...Object.fromEntries(Object.entries(heldOut).map(([k, v]) => [k, +v.toFixed(3)])), levelOnlySpearman: +baseline((b) => b.lv).toFixed(3), goldCostSpearman: +baseline(goldCost).toFixed(3) },
  note: "Placement change within a lobby per +1 of the feature (negative is better). Correlational; used as an exchange rate, see scripts/fit-board-value.mjs.",
  itemGainMean: +itemGainMean.toFixed(4),
  coefficients: coef,
};
writeFileSync(OUT, JSON.stringify(out, null, 1) + "\n");
console.log(`wrote ${OUT}: ${lobbies.length} lobbies; held out spearman ${heldOut.spearman.toFixed(3)} pairwise ${heldOut.pairwise.toFixed(3)} (level alone ${out.heldOut.levelOnlySpearman}, gold cost ${out.heldOut.goldCostSpearman})`);
console.log("mean item gain", itemGainMean.toFixed(3), "level", coef.level);
