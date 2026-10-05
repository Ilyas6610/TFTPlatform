// Downloads TFT champion/trait/augment/item metadata and icons from Riot's
// Data Dragon CDN into public/tft/, and writes public/tft/manifest.json
// mapping each Riot API id (e.g. "TFT17_Jinx", "TFT_Item_InfinityEdge") to
// its display name and local icon path. The UI reads only the manifest, so
// re-running this after a patch is all that's needed to pick up new content.
//
// Usage: npm run assets [-- --version 16.19.1]   (defaults to latest)
// Already-downloaded images are skipped, so re-runs are cheap.

import { mkdir, writeFile, access } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const DDRAGON = "https://ddragon.leagueoflegends.com";
const OUT_DIR = join(dirname(fileURLToPath(import.meta.url)), "..", "public", "tft");
const CONCURRENCY = 16;

// manifest key -> Data Dragon data file
const SOURCES = {
  champions: "tft-champion",
  traits: "tft-trait",
  augments: "tft-augments",
  items: "tft-item",
};

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`GET ${url}: ${res.status}`);
  return res.json();
}

async function exists(path) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

async function download(url, path) {
  if (await exists(path)) return "skipped";
  const res = await fetch(url);
  if (!res.ok) throw new Error(`GET ${url}: ${res.status}`);
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, Buffer.from(await res.arrayBuffer()));
  return "downloaded";
}

async function pool(tasks, n) {
  const results = [];
  let next = 0;
  async function worker() {
    while (next < tasks.length) {
      const i = next++;
      results[i] = await tasks[i]().catch((e) => e);
    }
  }
  await Promise.all(Array.from({ length: n }, worker));
  return results;
}

async function main() {
  const vIdx = process.argv.indexOf("--version");
  const version = vIdx > 0 ? process.argv[vIdx + 1] : (await getJSON(`${DDRAGON}/api/versions.json`))[0];
  console.log(`Data Dragon version ${version}`);

  const manifest = { version, champions: {}, traits: {}, augments: {}, items: {} };
  const images = new Map(); // local relative path -> remote url

  for (const [kind, file] of Object.entries(SOURCES)) {
    const { data } = await getJSON(`${DDRAGON}/cdn/${version}/data/en_US/${file}.json`);
    for (const entry of Object.values(data)) {
      const { group, full } = entry.image;
      const rel = `${group}/${full}`;
      images.set(rel, `${DDRAGON}/cdn/${version}/img/${rel}`);
      manifest[kind][entry.id] = {
        name: entry.name,
        icon: `/tft/${rel}`,
        ...(entry.cost !== undefined && { cost: entry.cost }),
      };
    }
    console.log(`${kind}: ${Object.keys(manifest[kind]).length}`);
  }

  const entries = [...images];
  const results = await pool(
    entries.map(([rel, url]) => () => download(url, join(OUT_DIR, rel))),
    CONCURRENCY,
  );
  const failed = results.map((r, i) => [r, entries[i][0]]).filter(([r]) => r instanceof Error);
  const downloaded = results.filter((r) => r === "downloaded").length;
  console.log(`images: ${downloaded} downloaded, ${results.length - downloaded - failed.length} cached, ${failed.length} failed`);
  for (const [err, rel] of failed) console.warn(`  ${rel}: ${err.message}`);

  // Drop manifest entries whose icon failed so the UI falls back to text.
  const failedIcons = new Set(failed.map(([, rel]) => `/tft/${rel}`));
  for (const kind of Object.keys(SOURCES)) {
    for (const [id, e] of Object.entries(manifest[kind])) {
      if (failedIcons.has(e.icon)) delete e.icon;
    }
  }

  await mkdir(OUT_DIR, { recursive: true });
  await writeFile(join(OUT_DIR, "manifest.json"), JSON.stringify(manifest));
  console.log(`wrote ${join(OUT_DIR, "manifest.json")}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
