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
  ["Mana Regen", ["manaRegen"]],
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
  // "Your 3-cost champions and all allies equipped with an Emblem": the cost is the main group (few hold emblems).
  const cost = t.match(/your (\d)-cost champions/);
  if (cost) return { scope: "cost", cost: Number(cost[1]) };
  if (/(holding (?:spatula|frying pan)|equipped with an? emblem|holding an? emblem)/.test(t)) return { scope: "holders", heldKind: "emblem" };
  if (/(holding an item|holding items|equipped with|holding a|holding the|with an item)/.test(t)) return { scope: "holders" };
  if (/(your team|all allies|your units|allies gain|your champions gain|champions gain|allies in the same row)/.test(t)) return { scope: "team" };
  if (/(grants?|gains?) (?:the|its|their) holders?|their holders|whenever their holders/.test(t)) return { scope: "holders" };
  if (/adjacent champions|nearby ally|nearest champion/.test(t)) return { scope: "team" };
  if (/^your [a-z'’. ]+s (?:grant|give)/.test(t) || /^[a-z'’. ]+ grants its holder/.test(t) || /allies holding/.test(t)) return { scope: "holders" };
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

/** "Allies equipped with Hand of Justice gain ...": the bonus goes to the holders of the items the augment gave, at most that many units. */
function holderLimit(s, res) {
  if (!/equipped with (?!an? emblem|emblems)/i.test(s) || !res.named.length) return {};
  const n = res.named.filter((x) => new RegExp(`equipped with (?:an? )?${x.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`, "i").test(s)).reduce((a, x) => a + x.n, 0);
  return n > 0 ? { maxUnits: n } : {};
}

function parseAugment(a, itemsByName, unitCosts) {
  const effects = [];
  const res = { gold: 0, xp: 0, rerolls: 0, components: 0, completed: 0, artifacts: 0, emblems: 0, units: [], named: [] };
  const recurring = {};
  const unparsed = [];
  const conditionalText = [];
  const notes = []; // sentences that describe an item just gained: read, but not counted as a further reward
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

  let repeats = 0;
  for (const s of sentences) {
    total++;
    let understood = false;
    const before = JSON.stringify(res);
    // Precision is a keyword: "Your team gains ... and Precision" lets their ability damage crit;
    // its definition ("Precision: Ability damage can critically strike. Additional Precision grants
    // 10% Critical Strike Damage.") adds nothing for a unit that has it once.
    if (/^(?:precision:|additional precision)/i.test(s)) {
      covered++;
      continue;
    }
    if (/\bprecision\b/i.test(s)) effects.push({ kind: "stat", ...scopeOf(s), ...holderLimit(s, res), stat: "abilityCrit", pct: 1 });
    // "At the start of the next 2 stages, gain the package again": the whole reward repeats.
    const again = s.match(/next (\d+|two|three) stages?,? gain (?:the|this|that) [\w ]*?again/i);
    if (again) {
      repeats += count(again[1]);
      covered++;
      continue;
    }
    const namedBefore = res.named.length;
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
              : /after (?:each |every )?player combat/.test(lower)
                ? "combat"
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
      const sc = { ...scopeOf(s), ...holderLimit(s, res) };
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
    // "Gain a Radiant Thief's Gloves item. This equips 2 random Radiant items every round.": the second sentence says what the
    // item just gained does, not a further reward, so its items aren't counted again.
    const describesGainedItem = /^(this|it|that)\b/i.test(s) && !/^(this|it) (augment|will)\b/i.test(s);
    if (describesGainedItem) {
      covered++;
      notes.push(s);
      continue;
    }
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
    // "Gain 2 XP at the start of player combat rounds", "After player combat, gain 2 XP if you won or 3 XP if you lost":
    // every player combat (the two amounts averaged); the scorer counts the combats left.
    const perCombat = /(?:at the start of player combat rounds|after player combat)/i.test(s) && [...s.matchAll(/(\d+)\s+XP/gi)].map((x) => Number(x[1]));
    if (perCombat && perCombat.length) (recurring.xp ??= {}).perCombat = perCombat.reduce((a, b) => a + b, 0) / perCombat.length;
    take(/(?:gain|get)\s+(\d+)\s+XP/gi, (m) => {
      if (perCombat && perCombat.length) return; // counted per combat above
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
    // "Each time your shop is rerolled you have a 45% chance to gain a free reroll"
    take(/(\d+)%\s+chance to (?:gain|get)\s+a\s+free\s+(?:Shop\s+)?re-?roll/gi, (m) => {
      if (/rerolled/i.test(s)) res.rerollRefund = Number(m[1]) / 100;
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
    // (not "Completed Item anvils": the anvil pattern below counts those)
    take(/(a|an|\d+|two)\s+(?:random\s+)?(?:completed\s+)?(?:item|items)\b(?!\s+anvils?)/gi, (m) => {
      if (!/completed/i.test(m[0])) return;
      // "2 random completed items that build from Needlessly Large Rod": items of one component, kept apart so the
      // scorer can value them by how much the board builds from it.
      const from = s.match(/build(?:s)?\s+from\s+(?:a\s+|an\s+)?(.+?)\.?\s*$/i);
      const comp = from && [...itemsByName].find(([n, it]) => it.kind === "component" && n.toLowerCase() === from[1].toLowerCase())?.[1];
      if (comp) res.named.push({ name: `completed items from ${comp.name}`, n: count(m[1]), kind: "completed", from: comp.name });
      else res.completed += count(m[1]);
    });
    take(/(a|an|\d+|two)\s+(?:random\s+)?Completed Item anvils?/gi, (m) => (res.completed += 1.3 * count(m[1])));
    // Radiant items are their own kind in the scorer; a named radiant (the Lucky Item Chest, Thief's Gloves) is counted once, by name below.
    take(/(a|an|\d+)\s+(?:random\s+)?Radiant Items?\b(?!\s*\w*'s)/gi, (m) => (res.radiants = (res.radiants ?? 0) + count(m[1])));
    take(/(a|an|\d+|two)\s+(?:random\s+)?Artifacts?(?:\s+anvils?)?/gi, (m) => (res.artifacts += count(m[1])));
    take(/(a|an|\d+|two|three)\s+(?:random\s+)?Emblems?/gi, (m) => {
      if (/matches their class|that trait|that Emblem|of that/i.test(s) && /champion/i.test(s)) return;
      if (/(with|holding)\s*$/i.test(work.slice(Math.max(0, m.index - 9), m.index))) return; // "allies equipped with an Emblem"
      res.emblems += count(m[1]);
    });
    take(/(a|an|\d+|two)\s+Reforgers?/gi, (m) => (res.components += 1.5 * count(m[1])));
    take(/(a|an|\d+|two)\s+(?:Magnetic|Golden Item)\s+Removers?/gi, () => (res.components += 1));
    // Champion Duplicators copy a unit (the Lesser one: 3-cost or less; the plain one is taken as 4-cost or less).
    take(/(a|an|\d+|two)\s+(Lesser\s+)?Champion Duplicators?/gi, (m) =>
      (res.duplicators ??= []).push({ n: count(m[1]), maxCost: m[2] ? 3 : 4 }),
    );
    // "Gain an Artifact Anvil now and after every 10 player combats": how many more depends on the stage (the scorer counts them).
    const everyCombats = s.match(/now and after every (\d+) player combats/i);
    if (everyCombats && /artifact/i.test(s)) {
      recurring.artifacts = { everyCombats: Number(everyCombats[1]) };
      hit = true;
    }
    if (/^after \d+ (?:player )?combats, gain another/i.test(s) && res.emblems > 0 && !res.duplicators?.length) {
      res.emblems += 1;
      hit = true;
    }
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
    // "a package containing a 5-cost champion, ..." (a star level is the pattern above)
    take(/containing\s+(a|an|\d+|two)\s+(?:random\s+)?(\d)-cost\s+champions?/gi, (m) =>
      res.units.push({ n: count(m[1]), cost: Number(m[2]), star: 1 }),
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
    // Longest names first, each match blanked out, so "a Tear of the Goddess" isn't also read as "a Tear".
    let rest = s;
    for (const [name, item] of [...itemsByName].sort((a, b) => b[0].length - a[0].length)) {
      // "Gain a Red Buff, a Blue Buff, and a Champion Duplicator": every item of the list after "gain".
      const re = new RegExp(`(\\b(?:gain|get)\\s+(?:[^.]*?(?:,|\\band)\\s+)?)(a|an|\\d+|two|three|four)\\s+(?:random\\s+)?${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}s?\\b`, "i");
      const mm = rest.match(re);
      if (mm) {
        res.named.push({ name: item.name, n: count(mm[2]), kind: item.kind });
        // Blank only this item (not the "gain ..." before it, which may name another).
        const from = mm.index + mm[1].length;
        rest = rest.slice(0, from) + " ".repeat(mm[0].length - mm[1].length) + rest.slice(mm.index + mm[0].length);
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
    // "When you reach Level 9, gain 27 gold": certain for a board that gets there; the scorer checks the level.
    // Only when it is the augment's whole reward (Max Build's 10 rerolls come now, its level-9 part stays a half).
    const had = JSON.parse(before);
    const nothingYet = ["gold", "xp", "rerolls", "components", "completed", "artifacts", "emblems"].every((k) => !had[k]) && !had.units.length && !had.named.length && !had.duplicators?.length;
    const reach = nothingYet && s.match(/^when you reach level (\d+)/i);
    if (reach && JSON.stringify(res) !== before) res.atLevel = Number(reach[1]);
    const conditional = /^(when|if)\b/i.test(s) && !/if heads|if tails/i.test(s) && !reach;
    if (conditional && JSON.stringify(res) !== before) conditionalText.push(s);
    if (/if heads|if tails/i.test(s) || conditional) {
      const was = JSON.parse(before);
      for (const k of ["gold", "xp", "rerolls", "components", "completed", "artifacts", "emblems", "radiants"]) {
        const delta = (res[k] ?? 0) - (was[k] ?? 0);
        if (delta) res[k] = (was[k] ?? 0) + delta / 2;
      }
      for (const n of res.named.slice(namedBefore)) n.n /= 2; // a named reward on one branch ("a Radiant Lucky Item Chest" if heads)
    }

    if (/win streak to \+4/i.test(s)) understood = true; // CalledShot: tempo, no stat value
    if (/(more health than you|if heads|flip a coin|roll a die|roll \d dice)/i.test(s)) understood = understood || false;

    if (understood) covered++;
    else unparsed.push(s);
  }

  if (repeats) {
    // Each repeat delivers the same reward again later in the game.
    for (const k of ["gold", "xp", "rerolls", "components", "completed", "artifacts", "emblems", "radiants"]) if (res[k]) res[k] *= 1 + repeats;
    for (const u of res.units) u.n *= 1 + repeats;
    for (const n of res.named) n.n *= 1 + repeats;
  }

  const resourceTotal =
    res.gold + res.xp + res.rerolls + res.components + res.completed + res.artifacts + res.emblems + (res.radiants ?? 0) + res.units.length + res.named.length +
    (res.duplicators?.length ?? 0) + (res.benchTransform ? 1 : 0) + (res.allOfCost ? 1 : 0) + (res.rerollRefund ? 1 : 0);
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
  if (notes.length) out.notes = notes;
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
// "Giant's Belts" etc. are matched by the singular name plus an optional s. Descriptions also
// write some names differently from the item data: without punctuation ("BF Swords" for
// "B.F. Sword") and, for Tear of the Goddess, as just "a Tear". Both are added as aliases.
for (const [name, item] of [...itemsByName]) {
  const plain = name.replace(/\./g, "");
  if (plain !== name && !itemsByName.has(plain)) itemsByName.set(plain, item);
}
// "Hands of Justice": names of the form "X of Y" pluralise their first word.
for (const [name, item] of [...itemsByName]) {
  const m = name.match(/^(\w+) (of .+)$/i);
  if (m && !itemsByName.has(`${m[1]}s ${m[2]}`)) itemsByName.set(`${m[1]}s ${m[2]}`, item);
}
for (const [alias, name] of [["Tear", "Tear Of The Goddess"]]) {
  const item = [...itemsByName].find(([n]) => n.toLowerCase() === name.toLowerCase())?.[1];
  if (item && !itemsByName.has(alias)) itemsByName.set(alias, item);
}

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
