// Writes src/planner/augmentStages.data.json: the tier and the stages
// (2-1, 3-2, 4-2) each Set 18 augment can be offered at, as listed by
// tactics.tools (https://tactics.tools/info/augments). Neither Riot's export
// nor our set data carries this. The page marks a stage an augment can't be
// offered at with a dimmed chip (class "opacity-30"), so this reads the page
// HTML; if its markup changes the sanity checks below fail instead of
// writing nonsense. Re-run after a patch that adds or changes augments:
//
//   npm run augment-stages
//
// Only facts are kept (tier and three flags per augment id), never text or
// images.

import { writeFileSync } from "node:fs";

const PAGE = "https://tactics.tools/info/augments";
const STAGES = ["2-1", "3-2", "4-2"];

const res = await fetch(PAGE, { headers: { "user-agent": "Mozilla/5.0 (tft-platform augment stage sync)" } });
if (!res.ok) throw new Error(`${PAGE}: HTTP ${res.status}`);
const html = await res.text();

// Tier sections, in page order: <h2>Silver</h2> ... cards ... <h2>Gold</h2> ...
const sections = [...html.matchAll(/<h[23][^>]*>(Silver|Gold|Prismatic|Black Market)<\/h[23]>/g)].map((m) => ({
  at: m.index,
  tier: m[1].toLowerCase(),
}));
if (sections.length < 3) throw new Error("tier sections not found; the page layout changed");

const augments = {};
const cardStart = 'class="p-4 rounded text-white1  bg-bg"';
let from = 0;
for (;;) {
  const start = html.indexOf(cardStart, from);
  if (start < 0) break;
  const end = html.indexOf(cardStart, start + cardStart.length);
  const card = html.slice(start, end < 0 ? undefined : end);
  from = start + cardStart.length;

  const id = /id="((?:DA|TFT)[A-Za-z0-9_]+)"/.exec(card)?.[1];
  const chips = [...card.matchAll(/<div class="px-2 py-\[6px\] rounded bg-bg2 ([^"]*)">(\d-\d)<\/div>/g)];
  if (!id || chips.length !== STAGES.length || chips.some((c, i) => c[2] !== STAGES[i])) continue;
  const tier = [...sections].reverse().find((s) => s.at < start)?.tier;
  augments[id] = { tier, stages: chips.map((c) => (c[1].includes("opacity-30") ? 0 : 1)) };
}

const n = Object.keys(augments).length;
const partial = Object.values(augments).filter((a) => a.stages.includes(0) && a.stages.includes(1)).length;
if (n < 100 || partial < 20) throw new Error(`read ${n} augments (${partial} with limited stages); the page layout changed`);

const out = new URL("../src/planner/augmentStages.data.json", import.meta.url);
writeFileSync(
  out,
  JSON.stringify({ source: PAGE, fetched: new Date().toISOString().slice(0, 10), stages: STAGES, augments }, null, 1) + "\n",
);
console.log(`wrote ${n} augments (${partial} limited to some stages) to ${out.pathname}`);
