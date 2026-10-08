// Fetches TFT Academy's comp guides (tftacademy.com/tierlist/comps) as an outside
// reference for the augment estimate: each guide's final and early boards (units,
// stars, items, positions) and the augments its authors recommend. One request: the
// tier list page's data holds every guide.
//
//   node scripts/fetch-academy-reference.mjs        # writes scripts/.academy/reference.json
//
// The file is a local, gitignored evaluation fixture, read by scripts/academy-check.ts:
// their guides are their work, so they're neither committed nor shipped, and the model is
// never fitted to them (see academy-check.ts on how they're used).

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const OUT = join(here, ".academy", "reference.json");
const URL = "https://tftacademy.com/tierlist/comps/__data.json";

// SvelteKit's payload is "devalue"-flattened: every value lives in one array and
// objects and arrays refer to their members by index.
function decode(flat) {
  const memo = new Map();
  const get = (i) => {
    if (i === -1) return null;
    if (memo.has(i)) return memo.get(i);
    const v = flat[i];
    if (Array.isArray(v)) {
      if (typeof v[0] === "string" && ["Date", "Set", "Map", "BigInt", "RegExp"].includes(v[0])) return v[1] ?? null;
      const out = [];
      memo.set(i, out);
      for (const j of v) out.push(get(j));
      return out;
    }
    if (v && typeof v === "object") {
      const out = {};
      memo.set(i, out);
      for (const [k, j] of Object.entries(v)) out[k] = get(j);
      return out;
    }
    return v;
  };
  return get(0);
}

const res = await fetch(URL, { headers: { "User-Agent": "tft-platform augment-estimate check (local dev)" } });
if (!res.ok) throw new Error(`${URL}: ${res.status}`);
const body = await res.json();
const nodes = body.nodes.map((n) => (n?.type === "data" ? decode(n.data) : null));
const patch = nodes[0]?.patch ?? null;
const guides = nodes.flatMap((n) => n?.guides ?? []);
if (guides.length === 0) throw new Error("no guides in the tier list data (the page's shape changed?)");

const board = (units) =>
  (units ?? [])
    .filter((u) => u?.apiName)
    .map((u) => ({ id: u.apiName, star: u.stars ?? 1, items: u.items ?? [], pos: u.boardIndex ?? null }));

const comps = guides
  .filter((g) => g.isPublic !== false)
  .map((g) => ({
    slug: g.compSlug,
    title: g.title,
    tier: g.tier ?? null,
    set: g.set ?? null,
    style: g.style ?? null,
    final: board(g.finalComp),
    early: board(g.earlyComp),
    augments: (g.augments ?? []).filter((a) => a?.apiName && !a.disabled).map((a) => a.apiName),
  }))
  .filter((c) => c.final.length > 0 && c.augments.length > 0);

mkdirSync(dirname(OUT), { recursive: true });
writeFileSync(
  OUT,
  JSON.stringify({ source: "https://tftacademy.com/tierlist/comps", fetchedAt: new Date().toISOString(), patch, comps }, null, 1) + "\n",
);
console.log(`${comps.length} comps (patch ${patch}) -> ${OUT}`);
