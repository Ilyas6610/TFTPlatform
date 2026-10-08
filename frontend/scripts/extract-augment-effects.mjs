// Turns each augment's rendered description into typed effects for the
// planner's augment impact estimate (src/planner/augmentScore.ts).
//
//   node scripts/extract-augment-effects.mjs --file set18.json        # a saved /sets/18/data body
//   node scripts/extract-augment-effects.mjs --url http://localhost:8080/api/v1/sets/18/data
//
// Writes src/planner/augmentEffects.data.json. The parse is rule-based and
// deliberately conservative: a sentence it doesn't understand is kept in
// `unparsed` and lowers the augment's confidence, and an augment it can't read
// at all gets no effects (the UI shows it as "not scored"). Re-run after a
// patch, then look at the printed coverage and the diff.

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const OUT = join(here, "../src/planner/augmentEffects.data.json");

const args = process.argv.slice(2);
const opt = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : null;
};

async function loadSet() {
  const file = opt("--file");
  if (file) return JSON.parse(readFileSync(file, "utf8"));
  const url = opt("--url") ?? "http://localhost:8080/api/v1/sets/18/data";
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

const WORDS = { a: 1, an: 1, one: 1, two: 2, three: 3, four: 4, five: 5, six: 6 };
const count = (s) => (/^\d+$/.test(s) ? Number(s) : (WORDS[s.toLowerCase()] ?? 1));

// Stat names as they appear in descriptions -> our stat keys.
const STATS = [
  ["Armor and Magic Resist", ["armor", "mr"]],
  ["Attack Damage and Ability Power", ["ad", "ap"]],
  ["Attack Damage & Ability Power", ["ad", "ap"]],
  ["Critical Strike Chance", ["crit"]],
  ["Critical Strike Damage", ["critDmg"]],
  ["Magic Resist", ["mr"]],
  ["Attack Damage", ["ad"]],
  ["Ability Power", ["ap"]],
  ["Attack Speed", ["as"]],
  ["Damage Amp", ["damageAmp"]],
  ["Durability", ["durability"]],
  ["Omnivamp", ["omnivamp"]],
  ["Armor", ["armor"]],
  ["Health", ["health"]],
];
const STAT_RE = new RegExp(
  `(\\d+(?:\\.\\d+)?)(%?)\\s+(?:permanent\\s+)?(?:max\\s+)?(${STATS.map(([n]) => n.replace(/[&]/g, "\\&")).join("|")})\\b`,
  "gi",
);
const statKeys = (name) => STATS.find(([n]) => n.toLowerCase() === name.toLowerCase())?.[1] ?? [];

/** Who a sentence's stat bonus goes to. */
let TRAIT_NAMES = [];
function scopeOf(s) {
  const t = s.toLowerCase();
  // "Your Lunar champions", "adjacent to an Elderwood plant": the bonus goes to that trait's units.
  const named = TRAIT_NAMES.find((n) => new RegExp(`\\b${n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\b`).test(s));
  if (named && !/(back row|back 2 rows|front row)/.test(t)) return { scope: "trait", trait: named };
  // "Champions in your back 2 rows gain ... for each champion that starts combat in your front row": the subject is the back.
  if (/(back row|back 2 rows)/.test(t)) return { scope: "back", rows: /back 2 rows/.test(t) ? 2 : 1 };
  if (/(front row|first row|center of the front)/.test(t)) return { scope: "front" };
  if (/(without items|aren't holding items|no items equipped)/.test(t)) return { scope: "unheld" };
  if (/(holding an item|holding items|equipped with an emblem|holding a|holding the|with an item)/.test(t)) return { scope: "holders" };
  if (/(your team|all allies|your units|allies gain|your champions gain|champions gain|allies in the same row)/.test(t)) return { scope: "team" };
  if (/(grants?|gains?) (?:the|its|their) holders?|their holders|whenever their holders/.test(t)) return { scope: "holders" };
  if (/adjacent champions|nearby ally|nearest champion/.test(t)) return { scope: "team" };
  if (/^your [a-z'’. ]+s (?:grant|give)/.test(t) || /^[a-z'’. ]+ grants its holder/.test(t) || /allies holding/.test(t)) return { scope: "holders" };
  const cost = t.match(/your (\d)-cost champions/);
  if (cost) return { scope: "cost", cost: Number(cost[1]) };
  return { scope: "unknown" };
}

/** Modifiers that change how much of a fight a stat bonus covers. */
function timingOf(s) {
  const out = {};
  const after = s.match(/after (\d+) seconds (?:of|in) combat/i);
  if (after) out.delay = Number(after[1]);
  const every = s.match(/every (\d+) seconds/i);
  if (every && /(gain|grants?)\s/i.test(s)) out.everySeconds = Number(every[1]);
  if (/for the rest of combat|each second|per second/i.test(s)) out.ramp = true;
  const forSecs = s.match(/for (\d+) seconds/i);
  if (forSecs && !after) out.forSeconds = Number(forSecs[1]);
  return out;
}

const SPLIT = /(?<=[.!?])\s+(?=[A-Z])|\s*\/\s*/;

function parseAugment(a, itemsByName, unitCosts) {
  const effects = [];
  const res = { gold: 0, xp: 0, rerolls: 0, components: 0, completed: 0, artifacts: 0, emblems: 0, units: [], named: [] };
  const recurring = {};
  const unparsed = [];
  const conditionalText = [];
  let covered = 0;
  let total = 0;

  const sentences = a.desc
    .replace(/B\.F\./g, "B·F·")
    .replace(/\n+/g, " / ")
    .split(SPLIT)
    .map((s) => s.trim().replace(/B·F·/g, "B.F."))
    .filter(Boolean)
    // Trailing credits ("TFT Macao Open, 2024") and flavour aren't effects.
    .filter((s) => !/(championship|open|crown|tactician's crown),?\s*\d{4}$/i.test(s) && !/^\w[\w &'-]*,\s*\d{4}$/.test(s));

  for (const s of sentences) {
    total++;
    let understood = false;
    const before = JSON.stringify(res);
    const lower = s.toLowerCase();

    // ---- stats ----
    const startsAt = s.match(/begin combat at (\d+)% Health/i);
    if (startsAt) {
      const sc0 = scopeOf(s);
      effects.push({ kind: "stat", ...sc0, stat: "health", pct: -(1 - Number(startsAt[1]) / 100) });
    }
    const statHits = [...s.matchAll(STAT_RE)].filter((m) => !(startsAt && /at\s*$/i.test(s.slice(Math.max(0, m.index - 4), m.index))));
    const perUnit =
      /for each (ally|champion)[^.]*shares? a trait|that shares a trait/.test(lower)
        ? "trait"
        : /for each non-unique trait|per non-unique trait/.test(lower)
          ? "activeTrait"
          : /for each bronze-tier trait/.test(lower)
            ? "bronzeTrait"
            : /for each champion that starts combat in your front row/.test(lower)
              ? "frontRowUnit"
              : /per player level/.test(lower) && !/increased by \d+ per player level/.test(lower)
                ? "level"
                : /for each item equipped to that champion/.test(lower)
                  ? "oneUnitItem"
                  : /for each item equipped/.test(lower)
                  ? "item"
                  : /for each emblem/.test(lower)
                    ? "emblem"
                    : /for each unique solar/.test(lower)
                      ? "uniqueOther"
                      : null;
    const isGrant = /(gains?|grants?|become|become|begin combat)/i.test(s) && !/^gain an? /i.test(s);
    // "drop below 35 Health", "Your Tactician loses 20 Health" aren't bonuses to the board.
    const notABonus = /\b(below|loses?|lose all)\b|tactician/i.test(s) && !/tactician's (crown|cape|shield)/i.test(s);
    if (statHits.length && !notABonus && (isGrant || /% max health|health shield/i.test(s))) {
      const sc = scopeOf(s);
      const timing = timingOf(s);
      const step = s.match(/increased by (\d+) per player level/i);
      for (const m of statHits) {
        for (const key of statKeys(m[3])) {
          const isPct = m[2] === "%";
          const v = Number(m[1]);
          effects.push({
            kind: "stat",
            ...sc,
            stat: key,
            ...(isPct ? { pct: v / 100 } : { flat: v }),
            ...(perUnit ? { per: perUnit } : {}),
            ...(step && !isPct ? { plusPerLevel: Number(step[1]) } : {}),
            ...timing,
          });
        }
      }
      understood = true;
    }

    // ---- resources ----
    let hit = false;
    let work = s; // matched "choose 1 of N" phrases are blanked so the generic patterns don't count them again
    const choose = s.match(/Choose 1 of (\d+) (Radiant Items|Artifacts|components)/i);
    if (choose) {
      if (/radiant/i.test(choose[2])) res.radiants = (res.radiants ?? 0) + 1;
      else if (/artifact/i.test(choose[2])) res.artifacts += 1.3;
      else res.components += 1.3;
      hit = true;
      work = s.replace(choose[0], " ");
    }
    const take = (re, fn) => {
      for (const m of work.matchAll(re)) {
        fn(m);
        hit = true;
      }
    };
    // recurring first, so "7 gold now and every round" isn't also counted twice
    const everyRound = /every round for the rest of the game|each round for the rest of the game/i.test(s);
    const everyStage = /at the start of (?:every|each) stage/i.test(s);
    const nextRounds = s.match(/(?:at the start of the )?next (\d+) rounds/i);
    take(/(?:gain|get|and)\s+(\d+)\s+gold(?!\s+of)/gi, (m) => {
      res.gold += Number(m[1]);
      if (everyRound) (recurring.gold ??= {}).perRound = Number(m[1]);
    });
    take(/(\d+)\s+gold\s+at the start of (?:every|each) stage/gi, (m) => ((recurring.gold ??= {}).perStage = Number(m[1])));
    take(/(?:gain|get)\s+(\d+)\s+XP/gi, (m) => {
      res.xp += Number(m[1]);
      if (everyStage) (recurring.xp ??= {}).perStage = Number(m[1]);
      if (nextRounds) (recurring.xp ??= {}).rounds = Number(nextRounds[1]);
    });
    take(/(\d+)\s+(?:free\s+)?(?:Shop\s+)?re-?rolls?/gi, (m) => {
      res.rerolls += Number(m[1]);
      if (everyStage) (recurring.rerolls ??= {}).perStage = Number(m[1]);
      // "N rerolls every round" (the number sits next to "every round")
      if (new RegExp(`${m[1]}\\s+(?:free\\s+)?(?:Shop\\s+)?re-?rolls?\\s+(?:every|each) round`, "i").test(s)) (recurring.rerolls ??= {}).perRound = Number(m[1]);
    });
    // "16 free rerolls now and 3 every round": the recurring part is the number before "every round"
    take(/(?:now and|then)\s+(\d+)\s+(?:every|each) round/gi, (m) => {
      if (/re-?roll/i.test(s)) (recurring.rerolls ??= {}).perRound = Number(m[1]);
    });
    take(/(?:gain|get)\s+(a|an)\s+(?:free\s+)?Shop reroll/gi, (m) => {
      res.rerolls += count(m[1]);
      if (/every round/i.test(s)) (recurring.rerolls ??= {}).perRound = count(m[1]);
    });
    take(/(a|an|\d+|two|three|four)\s+(?:random\s+)?(?:item\s+)?components?\b(?!\s+anvil)/gi, (m) => {
      if (/(whenever|each time|every)/i.test(s) && !/gain/i.test(s)) return;
      res.components += count(m[1]);
      if (nextRounds) (recurring.components ??= {}).rounds = Number(nextRounds[1]);
    });
    take(/(a|an|\d+|two)\s+(?:random\s+)?component anvils?/gi, (m) => (res.components += 1.3 * count(m[1])));
    take(/(a|an|\d+|two)\s+(?:random\s+)?(?:completed\s+)?(?:item|items)\b/gi, (m) => {
      if (/completed/i.test(m[0])) res.completed += count(m[1]);
    });
    take(/(a|an|\d+|two)\s+(?:random\s+)?Completed Item anvils?/gi, (m) => (res.completed += 1.3 * count(m[1])));
    // Radiant items are their own kind in the scorer; a named radiant (the Lucky Item Chest, Thief's Gloves) is counted once, by name below.
    take(/(a|an|\d+)\s+(?:random\s+)?Radiant Items?\b(?!\s*\w*'s)/gi, (m) => (res.radiants = (res.radiants ?? 0) + count(m[1])));
    take(/(a|an|\d+|two)\s+(?:random\s+)?Artifacts?(?:\s+anvils?)?/gi, (m) => (res.artifacts += count(m[1])));
    take(/(a|an|\d+|two|three)\s+(?:random\s+)?Emblems?/gi, (m) => {
      if (/matches their class|that trait|that Emblem|of that/i.test(s) && /champion/i.test(s)) return;
      res.emblems += count(m[1]);
    });
    take(/(a|an|\d+|two)\s+Reforgers?/gi, (m) => (res.components += 1.5 * count(m[1])));
    take(/(a|an|\d+|two)\s+(?:Magnetic|Golden Item)\s+Removers?/gi, () => (res.components += 1));
    // Champion Duplicators copy a unit (the Lesser one: 3-cost or less; the plain one is taken as 4-cost or less).
    take(/(a|an|\d+|two)\s+(Lesser\s+)?Champion Duplicators?/gi, (m) =>
      (res.duplicators ??= []).push({ n: count(m[1]), maxCost: m[2] ? 3 : 4 }),
    );
    if (/^gain another after \d+ player combats/i.test(s) && res.duplicators?.length) {
      res.duplicators[res.duplicators.length - 1].n += 1;
      hit = true;
    }
    if (/^this item allows you to copy/i.test(s)) hit = true; // the duplicator's own description
    take(/(?:gain|get)\s+(a|an|\d+|two|three)\s+(?:random\s+)?(?:(\d)-star\s+)?(\d)-cost\s+(?:non-Tank\s+|Tank\s+|\w+\s+)?champions?/gi, (m) =>
      res.units.push({ n: count(m[1]), cost: Number(m[3]), star: m[2] ? Number(m[2]) : 1 }),
    );
    take(/(a|an|\d+|two)\s+(?:random\s+)?(\d)-star\s+(\d)-cost\s+champion/gi, (m) =>
      res.units.push({ n: count(m[1]), cost: Number(m[3]), star: Number(m[2]) }),
    );
    take(/gold of random champions/gi, () => {});
    take(/Gain (\d+) gold of random champions|Gain (\d+) gold of/gi, () => {});
    const bp = s.match(/Gain (\d+) gold of random champions/i);
    if (bp) {
      res.units.push({ n: Number(bp[1]) / 3, cost: 3, star: 1 }); // gold's worth of champions
    }

    const bench = s.match(/(\d+) rightmost bench slots transform into random champions of the same cost/i);
    if (bench) {
      res.benchTransform = { slots: Number(bench[1]) };
      hit = true;
    }
    const copiesOf = s.match(/Gain (\d+) copies of a random (\d)-cost champion/i);
    if (copiesOf) {
      res.units.push({ n: Number(copiesOf[1]), cost: Number(copiesOf[2]), star: 1 });
      hit = true;
    }
    const eachCost = s.match(/Gain a copy of each (\d)-cost champion/i);
    if (eachCost) {
      res.allOfCost = Number(eachCost[1]);
      hit = true;
    }
    if (/gain another copy of them at the start of each round/i.test(s)) {
      const last = res.units[res.units.length - 1];
      if (last) {
        (recurring.copies ??= {}).perRound = 1;
        recurring.copies.cost = last.cost;
        recurring.copies.n = 1; // "another copy of them": one a round
      }
      hit = true;
    }

    // named items the set knows ("Gain a Rabadon's Deathcap")
    for (const [name, item] of itemsByName) {
      const re = new RegExp(`(?:gain|get)\\s+(a|an|\\d+|two|three|four)\\s+(?:random\\s+)?${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}s?\\b`, "i");
      const mm = s.match(re);
      if (mm) {
        res.named.push({ name, n: count(mm[1]), kind: item.kind });
        hit = true;
      }
    }

    // fixed champions ("Gain a Leona, a Kayle, and a Sejuani"): costs come from the set's units
    if (/^gain\b/i.test(s)) {
      for (const [uname, cost] of unitCosts) {
        const re = new RegExp(`\\b(?:a|an)\\s+${uname.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\b`);
        if (re.test(s)) {
          res.units.push({ n: 1, cost, star: 1, fixed: true, name: uname });
          hit = true;
        }
      }
    }

    if (hit) understood = true;

    // "Flip a coin. If heads, gain X. If tails, gain Y" (two branches), and rewards that depend on something
    // happening ("When 2 players are eliminated, gain 40 gold"): count half.
    const conditional = /^(when|if)\b/i.test(s) && !/if heads|if tails/i.test(s);
    if (conditional && JSON.stringify(res) !== before) conditionalText.push(s);
    if (/if heads|if tails/i.test(s) || conditional) {
      const was = JSON.parse(before);
      for (const k of ["gold", "xp", "rerolls", "components", "completed", "artifacts", "emblems", "radiants"]) {
        const delta = (res[k] ?? 0) - (was[k] ?? 0);
        if (delta) res[k] = (was[k] ?? 0) + delta / 2;
      }
    }

    if (/win streak to \+4/i.test(s)) understood = true; // CalledShot: tempo, no stat value
    if (/(more health than you|if heads|flip a coin|roll a die|roll \d dice)/i.test(s)) understood = understood || false;

    if (understood) covered++;
    else unparsed.push(s);
  }

  const resourceTotal =
    res.gold + res.xp + res.rerolls + res.components + res.completed + res.artifacts + res.emblems + (res.radiants ?? 0) + res.units.length + res.named.length +
    (res.duplicators?.length ?? 0) + (res.benchTransform ? 1 : 0) + (res.allOfCost ? 1 : 0);
  const hasResources = resourceTotal > 0 || Object.keys(recurring).length > 0;
  const out = {};
  if (effects.length) out.effects = effects;
  if (hasResources) {
    const r = { ...res };
    for (const k of Object.keys(r)) if (Array.isArray(r[k]) ? r[k].length === 0 : r[k] === 0 || r[k] === undefined) delete r[k];
    out.resources = r;
    if (Object.keys(recurring).length) out.recurring = recurring;
  }
  const ratio = total ? covered / total : 0;
  out.confidence = !effects.length && !hasResources ? "none" : unparsed.length === 0 ? "high" : ratio >= 0.5 ? "medium" : "low";
  if (unparsed.length) out.unparsed = unparsed;
  if (conditionalText.length) out.conditional = conditionalText;
  return out;
}

// Augments whose effect isn't a bonus to the board, or is too tangled to price.
const NOT_MODELLED = {
  DA_Dummify: "replaces your whole board with a training dummy",
  DA_TimeSkip: "turns off your shop for three rounds, then gives XP",
  DA_SoloLeveling: "shrinks your team to one champion for five combats",
  DA_HardBargain: "gives up carousel choices for gold and player health",
  DA_FutureFocused: "costs Tactician health for later champions",
  DA_Expedition: "loses a bench champion every round for a reward",
};

const set = await loadSet();
// Trait names that read as the subject of a bonus ("Your Lunar champions"); very short ones would match ordinary words.
TRAIT_NAMES = [...new Set(set.traits.map((t) => t.name))].filter((n) => n.length >= 4);
const itemsByName = new Map();
for (const it of [...set.items, ...(set.wisps ?? [])]) {
  if (it.name && ["component", "completed", "emblem", "artifact", "radiant"].includes(it.kind) && !itemsByName.has(it.name)) itemsByName.set(it.name, it);
}
// "Giant's Belts" etc. are matched by the singular name plus an optional s.

const unitCosts = new Map();
for (const u of set.units) {
  // base names only: "Lux (Coven)" style forms share the base unit's cost
  const base = u.name.replace(/\s*\(.*\)$/, "");
  if (!unitCosts.has(base)) unitCosts.set(base, u.cost);
}

const result = {};
const stats = { high: 0, medium: 0, low: 0, none: 0 };
for (const a of set.augments) {
  const p = NOT_MODELLED[a.apiName] ? { confidence: "none", unparsed: [NOT_MODELLED[a.apiName]] } : parseAugment(a, itemsByName, unitCosts);
  // Augments built around a trait (the set data lists it) are worth little to a board that doesn't play it.
  if (a.traits?.length && p.confidence !== "none") p.traits = a.traits;
  result[a.apiName] = p;
  stats[p.confidence]++;
}

const file = {
  setNumber: set.setNumber,
  version: set.version,
  note: "Generated by scripts/extract-augment-effects.mjs from the augments' rendered descriptions. Estimates only: see src/planner/augmentScore.ts.",
  augments: result,
};
writeFileSync(OUT, JSON.stringify(file, null, 1) + "\n");
console.log(`wrote ${OUT}: ${set.augments.length} augments; confidence`, stats);
