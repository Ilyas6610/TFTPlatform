import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  ApiError,
  ExploreOptions,
  ExploreResult,
  ExploreRow,
  PlacementStats,
  SetData,
  explore,
  getExploreOptions,
  getSetData,
} from "../api/client";
import { AssetKind, GameIcon } from "../assets/tft";
import { CURRENT_TFT_SET } from "../config";

// The page URL holds the search in the API's own format (see
// handlers_explore.go), so a search is a shareable link:
//   unit=ID[*minStar][:I1,I2]   item=ID   trait=ID[*minUnits]   queue=1100   level=8-

interface UnitCond {
  id: string;
  minStar: number; // 0 = any
  items: string[];
}

interface TraitCond {
  id: string;
  minUnits: number; // 0 = any active
  maxUnits: number; // 0 = no upper bound; an exact tier is min..(next breakpoint - 1)
}

const QUEUE_NAMES: Record<number, string> = {
  1090: "Normal",
  1100: "Ranked",
  1130: "Hyper Roll",
  1160: "Double Up",
};

const MIN_RELIABLE_BOARDS = 20;

function parseUnit(v: string): UnitCond {
  const [spec, items = ""] = v.split(":");
  const [id, star = "0"] = spec.split("*");
  return { id, minStar: Number(star) || 0, items: items ? items.split(",") : [] };
}

function formatUnit(u: UnitCond): string {
  return u.id + (u.minStar > 1 ? `*${u.minStar}` : "") + (u.items.length ? `:${u.items.join(",")}` : "");
}

function parseTrait(v: string): TraitCond {
  const [id, n = ""] = v.split("*");
  const [min = "0", max = "0"] = n.split("-");
  return { id, minUnits: Number(min) || 0, maxUnits: Number(max) || 0 };
}

function formatTrait(t: TraitCond): string {
  if (t.minUnits <= 0) return t.id;
  return `${t.id}*${t.minUnits}${t.maxUnits > 0 ? `-${t.maxUnits}` : ""}`;
}

/** The select value for a trait condition: "min-max", "min-" or "". */
const traitRangeValue = (t: { minUnits: number; maxUnits: number }) =>
  t.minUnits > 0 ? `${t.minUnits}-${t.maxUnits || ""}` : "";

const pct = (n: number) => `${Math.round(n * 100)}%`;
const avg = (n: number) => n.toFixed(2);

export default function ExplorerPage() {
  const [params, setParams] = useSearchParams();
  const set = Number(params.get("set")) || CURRENT_TFT_SET;
  const units = params.getAll("unit").map(parseUnit);
  const items = params.getAll("item");
  const traits = params.getAll("trait").map(parseTrait);
  const queues = params.getAll("queue");
  const level = params.get("level") ?? "";

  const [options, setOptions] = useState<ExploreOptions | null>(null);
  const [setData, setSetData] = useState<SetData | null>(null);
  const [result, setResult] = useState<ExploreResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setOptions(null);
    getExploreOptions(set)
      .then(setOptions)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "failed to load options"));
    getSetData(set)
      .then(setSetData)
      .catch(() => setSetData(null)); // names fall back to ids
  }, [set]);

  // Default to ranked games once options show they exist.
  useEffect(() => {
    if (options && !params.has("queue") && !params.has("anyQueue") && options.queues.some((q) => q.id === 1100)) {
      update((p) => p.set("queue", "1100"));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options]);

  // The API takes the same parameters as the page.
  const apiQuery = useMemo(() => {
    const q = new URLSearchParams();
    q.set("set", String(set));
    for (const k of ["queue", "level", "unit", "item", "trait"])
      for (const v of params.getAll(k)) if (v) q.append(k, v);
    return q.toString();
  }, [params, set]);

  useEffect(() => {
    // Wait for options: they decide the default queue, and searching before
    // that would run (and show) an unfiltered search first.
    if (!options) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    explore(apiQuery)
      .then((r) => !cancelled && setResult(r))
      .catch((e: unknown) => !cancelled && setError(e instanceof ApiError ? e.message : "search failed"))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [apiQuery, options]);

  function update(fn: (p: URLSearchParams) => void) {
    const next = new URLSearchParams(params);
    next.set("set", String(set));
    fn(next);
    setParams(next, { replace: true });
  }
  const setAll = (key: string, values: string[]) =>
    update((p) => {
      p.delete(key);
      values.forEach((v) => p.append(key, v));
    });
  const setUnits = (us: UnitCond[]) => setAll("unit", us.map(formatUnit));
  const setTraits = (ts: TraitCond[]) => setAll("trait", ts.map(formatTrait));

  const names = useMemo(() => buildNames(setData), [setData]);
  const addUnit = (id: string) =>
    !units.some((u) => u.id === id) && setUnits([...units, { id, minStar: 0, items: [] }]);
  const addItem = (id: string) => !items.includes(id) && setAll("item", [...items, id]);
  const addTrait = (id: string, minUnits = 0, maxUnits = 0) =>
    setTraits([...traits.filter((t) => t.id !== id), { id, minUnits, maxUnits }]);

  return (
    <div className="explorer">
      <div className="panel explorer-controls">
        <h2>Explorer</h2>
        <p className="muted">
          Placement stats for every board matching all conditions, with what else those boards ran. Built from{" "}
          {options ? `${options.boards} ` : ""}boards in ingested Set {set} matches.
        </p>

        <div className="set-picker">
          <label className="muted">
            Queue{" "}
            <select
              value={params.has("anyQueue") ? "any" : (queues[0] ?? "any")}
              onChange={(e) =>
                update((p) => {
                  p.delete("queue");
                  p.delete("anyQueue");
                  if (e.target.value === "any") p.set("anyQueue", "1");
                  else p.set("queue", e.target.value);
                })
              }
            >
              <option value="any">All queues</option>
              {options?.queues.map((q) => (
                <option key={q.id} value={q.id}>
                  {QUEUE_NAMES[q.id] ?? `Queue ${q.id} (${q.gameType})`} · {q.boards}
                </option>
              ))}
            </select>
          </label>
          <label className="muted">
            Level{" "}
            <select
              value={level}
              onChange={(e) => update((p) => (e.target.value ? p.set("level", e.target.value) : p.delete("level")))}
            >
              <option value="">Any</option>
              {[10, 9, 8, 7, 6].map((n) => (
                <option key={n} value={`${n}-`}>
                  {n}+
                </option>
              ))}
              <option value="-7">7 or lower</option>
            </select>
          </label>
          {(units.length > 0 || items.length > 0 || traits.length > 0) && (
            <button type="button" onClick={() => update((p) => ["unit", "item", "trait"].forEach((k) => p.delete(k)))}>
              Clear conditions
            </button>
          )}
        </div>

        <div className="conditions">
          {units.map((u, i) => (
            <div className="condition" key={u.id}>
              <GameIcon
                kind="champions"
                id={u.id}
                size={28}
                fallbackSrc={names.icon(u.id)}
                fallbackName={names.name(u.id)}
              />
              <strong>{names.name(u.id)}</strong>
              <select
                value={u.minStar}
                onChange={(e) =>
                  setUnits(units.map((x, j) => (j === i ? { ...x, minStar: Number(e.target.value) } : x)))
                }
              >
                <option value={0}>any ★</option>
                <option value={2}>2★+</option>
                <option value={3}>3★+</option>
              </select>
              {u.items.map((it) => (
                <button
                  type="button"
                  key={it}
                  className="condition-item"
                  title={`Remove ${names.name(it)}`}
                  onClick={() =>
                    setUnits(units.map((x, j) => (j === i ? { ...x, items: x.items.filter((y) => y !== it) } : x)))
                  }
                >
                  <GameIcon kind="items" id={it} size={20} fallbackSrc={names.icon(it)} fallbackName={names.name(it)} />
                </button>
              ))}
              {u.items.length < 3 && (
                <Picker
                  exclude={u.items}
                  placeholder="+ item on unit"
                  kind="items"
                  options={options?.items ?? []}
                  names={names}
                  onPick={(it) =>
                    !u.items.includes(it) &&
                    setUnits(units.map((x, j) => (j === i ? { ...x, items: [...x.items, it] } : x)))
                  }
                />
              )}
              <button type="button" className="remove" onClick={() => setUnits(units.filter((_, j) => j !== i))}>
                ×
              </button>
            </div>
          ))}
          {items.map((it) => (
            <div className="condition" key={it}>
              <GameIcon kind="items" id={it} size={28} fallbackSrc={names.icon(it)} fallbackName={names.name(it)} />
              <strong>{names.name(it)}</strong>
              <span className="muted">anywhere</span>
              <button
                type="button"
                className="remove"
                onClick={() =>
                  setAll(
                    "item",
                    items.filter((x) => x !== it),
                  )
                }
              >
                ×
              </button>
            </div>
          ))}
          {traits.map((t, i) => (
            <div className="condition" key={t.id}>
              <GameIcon
                kind="traits"
                id={t.id}
                size={24}
                fallbackSrc={names.icon(t.id)}
                fallbackName={names.name(t.id)}
              />
              <strong>{names.name(t.id)}</strong>
              <select
                value={traitRangeValue(t)}
                onChange={(e) => {
                  const [min = "0", max = "0"] = e.target.value.split("-");
                  setTraits(
                    traits.map((x, j) =>
                      j === i ? { ...x, minUnits: Number(min) || 0, maxUnits: Number(max) || 0 } : x,
                    ),
                  );
                }}
              >
                <option value="">any active</option>
                {names.tiers(t.id).map((tier) => (
                  <optgroup key={tier.minUnits} label={`Tier ${tier.index}`}>
                    {tier.maxUnits > 0 && (
                      <option value={`${tier.minUnits}-${tier.maxUnits}`}>
                        exactly {tier.minUnits === tier.maxUnits ? tier.minUnits : `${tier.minUnits}–${tier.maxUnits}`}
                      </option>
                    )}
                    <option value={`${tier.minUnits}-`}>{tier.minUnits}+</option>
                  </optgroup>
                ))}
              </select>
              <button type="button" className="remove" onClick={() => setTraits(traits.filter((_, j) => j !== i))}>
                ×
              </button>
            </div>
          ))}
        </div>

        <div className="set-picker">
          <Picker
            exclude={units.map((u) => u.id)}
            placeholder="Add unit…"
            kind="champions"
            options={options?.units ?? []}
            names={names}
            onPick={addUnit}
          />
          <Picker
            exclude={items}
            placeholder="Add item…"
            kind="items"
            options={options?.items ?? []}
            names={names}
            onPick={addItem}
          />
          <Picker
            placeholder="Add trait…"
            kind="traits"
            // Traits already chosen drop out with all their tiers.
            options={traitPickerOptions(options?.traits ?? [], names).filter(
              (o) => !traits.some((t) => o.id === t.id || o.id.startsWith(`${t.id}#`)),
            )}
            names={names}
            onPick={(key) => {
              const [id, tierIndex] = key.split("#");
              const tier = tierIndex ? names.tier(id, Number(tierIndex)) : undefined;
              addTrait(id, tier?.minUnits ?? 0, tier?.maxUnits ?? 0);
            }}
          />
        </div>
      </div>

      {error && <div className="error-box">{error}</div>}
      {loading && !result && <p className="muted">Loading...</p>}

      {result && (
        <div className={loading ? "explorer-results stale" : "explorer-results"}>
          <Summary result={result} />

          <div className="explorer-grid">
            {(result.unitItems ?? []).map((rows, i) =>
              units[i] ? (
                <div className="panel" key={units[i].id}>
                  <h3 className="game-label">
                    <GameIcon
                      kind="champions"
                      id={units[i].id}
                      size={24}
                      fallbackSrc={names.icon(units[i].id)}
                      fallbackName={names.name(units[i].id)}
                    />
                    Items on {names.name(units[i].id)}
                  </h3>
                  <div className="star-split">
                    {result.unitStars?.[i]?.map((st) => (
                      <span
                        key={st.tier}
                        className="star-chip"
                        title={`Boards whose best ${names.name(units[i].id)} is ${st.tier}★`}
                      >
                        {"★".repeat(st.tier ?? 1)} <strong>{st.boards}</strong>{" "}
                        <span className={placementTone(st)}>{avg(st.avgPlacement)}</span>
                      </span>
                    ))}
                  </div>
                  <Breakdown
                    rows={rows}
                    kind="items"
                    names={names}
                    total={result.summary.boards}
                    onPick={(it) =>
                      units[i].items.length < 3 &&
                      !units[i].items.includes(it) &&
                      setUnits(units.map((x, j) => (j === i ? { ...x, items: [...x.items, it] } : x)))
                    }
                  />
                </div>
              ) : null,
            )}
            <div className="panel">
              <h3>Units</h3>
              <Breakdown
                rows={result.units}
                kind="champions"
                names={names}
                total={result.summary.boards}
                onPick={addUnit}
              />
            </div>
            <div className="panel">
              <h3>Items</h3>
              <Breakdown
                rows={result.items}
                kind="items"
                names={names}
                total={result.summary.boards}
                onPick={addItem}
              />
            </div>
            <div className="panel">
              <h3>Traits</h3>
              <TraitsBreakdown
                rows={result.traits}
                names={names}
                total={result.summary.boards}
                onPick={(id, tier) => addTrait(id, tier?.minUnits ?? 0, tier?.maxUnits ?? 0)}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Summary({ result }: { result: ExploreResult }) {
  const s = result.summary;
  const b = result.baseline;
  const max = Math.max(1, ...s.placements);
  const delta = s.avgPlacement - b.avgPlacement;
  if (s.boards === 0) return <div className="panel muted">No boards match these conditions.</div>;
  return (
    <div className="panel">
      <div className="stat-tiles">
        <Tile label="Boards" value={String(s.boards)} sub={`${pct(s.boards / Math.max(1, b.boards))} of all`} />
        <Tile
          label="Avg place"
          value={avg(s.avgPlacement)}
          sub={`${delta <= 0 ? "" : "+"}${delta.toFixed(2)} vs ${avg(b.avgPlacement)} overall`}
          tone={Math.abs(delta) < 0.05 ? undefined : delta < 0 ? "good" : "bad"}
        />
        <Tile label="Top 4" value={pct(s.top4Rate)} sub={`${pct(b.top4Rate)} overall`} />
        <Tile label="Win" value={pct(s.winRate)} sub={`${pct(b.winRate)} overall`} />
      </div>
      {s.boards < MIN_RELIABLE_BOARDS && (
        <p className="warning-box">Only {s.boards} boards match — treat these numbers as rough.</p>
      )}
      <div className="placement-bars" aria-label="Placement distribution">
        {s.placements.map((n, i) => (
          <div key={i} className="placement-bar" title={`${n} boards placed ${i + 1}`}>
            <span className="bar-count">{n}</span>
            <div className={`fill placement-${i + 1}`} style={{ height: `${(n / max) * 100}%` }} />
            <span className="muted">{i + 1}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function Tile({ label, value, sub, tone }: { label: string; value: string; sub: string; tone?: "good" | "bad" }) {
  return (
    <div className="stat-tile">
      <div className="muted">{label}</div>
      <div className={`stat-value ${tone ?? ""}`}>{value}</div>
      <div className="muted stat-sub">{sub}</div>
    </div>
  );
}

const PREVIEW_ROWS = 10;

function Breakdown({
  rows,
  kind,
  names,
  total,
  onPick,
}: {
  rows: ExploreRow[];
  kind: AssetKind;
  names: Names;
  total: number;
  onPick: (id: string, row: ExploreRow) => void;
}) {
  const [all, setAll] = useState(false);
  useEffect(() => setAll(false), [rows]);
  if (rows.length === 0) return <p className="muted">Nothing to show.</p>;
  const shown = all ? rows : rows.slice(0, PREVIEW_ROWS);
  return (
    <>
      <table className="breakdown">
        <thead>
          <tr>
            <th></th>
            <th title="Boards with it (share of matching boards)">Boards</th>
            <th title="Average placement">Avg</th>
            <th>Top 4</th>
          </tr>
        </thead>
        <tbody>
          {shown.map((r) => (
            <tr
              key={r.id}
              className="clickable"
              title={`${names.name(r.id)} — add as a condition`}
              onClick={() => onPick(r.id, r)}
            >
              <td className="name-cell">
                <GameIcon
                  kind={kind}
                  id={r.id}
                  size={22}
                  fallbackSrc={names.icon(r.id)}
                  fallbackName={names.name(r.id)}
                />
                <span>{names.name(r.id)}</span>
              </td>
              <StatCells row={r} total={total} />
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length > PREVIEW_ROWS && (
        <button type="button" className="show-more" onClick={() => setAll(!all)}>
          {all ? "Show less" : `Show all ${rows.length}`}
        </button>
      )}
    </>
  );
}

function StatCells({ row: r, total }: { row: PlacementStats; total: number }) {
  return (
    <>
      <td className="num">
        {r.boards} <span className="muted share">{pct(r.boards / Math.max(1, total))}</span>
      </td>
      <td className={`num ${placementTone(r)}`}>{avg(r.avgPlacement)}</td>
      <td className="num">{pct(r.top4Rate)}</td>
    </>
  );
}

// Trait style -> the bronze/silver/gold/prismatic badge classes (as in
// SetInfoPage).
const TIER_STYLE: Record<number, number> = { 1: 1, 2: 2, 3: 2, 4: 3, 5: 3, 6: 4 };

/**
 * Traits grouped by trait: a summary row (any active tier) followed by one
 * row per tier reached. Clicking a tier filters to exactly that tier.
 */
function TraitsBreakdown({
  rows,
  names,
  total,
  onPick,
}: {
  rows: ExploreRow[];
  names: Names;
  total: number;
  onPick: (id: string, tier?: TraitTier) => void;
}) {
  const [all, setAll] = useState(false);
  useEffect(() => setAll(false), [rows]);
  const groups = useMemo(() => {
    const byTrait = new Map<string, ExploreRow[]>();
    for (const r of rows) byTrait.set(r.id, [...(byTrait.get(r.id) ?? []), r]);
    return [...byTrait.entries()]
      .map(([id, tiers]) => {
        // A board has one tier per trait, so tier rows add up exactly.
        const boards = tiers.reduce((n, t) => n + t.boards, 0);
        const weighted = (k: "avgPlacement" | "top4Rate" | "winRate") =>
          tiers.reduce((n, t) => n + t[k] * t.boards, 0) / Math.max(1, boards);
        const summary: PlacementStats = {
          boards,
          avgPlacement: weighted("avgPlacement"),
          top4Rate: weighted("top4Rate"),
          winRate: weighted("winRate"),
        };
        return { id, summary, tiers: [...tiers].sort((a, b) => (a.tier ?? 0) - (b.tier ?? 0)) };
      })
      .sort((a, b) => b.summary.boards - a.summary.boards);
  }, [rows]);
  if (groups.length === 0) return <p className="muted">Nothing to show.</p>;
  const shown = all ? groups : groups.slice(0, 8);
  return (
    <>
      <table className="breakdown traits-breakdown">
        <thead>
          <tr>
            <th></th>
            <th>Boards</th>
            <th>Avg</th>
            <th>Top 4</th>
          </tr>
        </thead>
        {shown.map((g) => (
          <tbody key={g.id} className="trait-group">
            <tr
              className="clickable explore-trait-row"
              title={`${names.name(g.id)}, any tier — add as a condition`}
              onClick={() => onPick(g.id)}
            >
              <td className="name-cell">
                <GameIcon
                  kind="traits"
                  id={g.id}
                  size={20}
                  fallbackSrc={names.icon(g.id)}
                  fallbackName={names.name(g.id)}
                />
                <span>{names.name(g.id)}</span>
              </td>
              <StatCells row={g.summary} total={total} />
            </tr>
            {g.tiers.map((r) => {
              const tier = names.tier(g.id, r.tier ?? 0);
              const label = tier ? tierLabel(tier) : `tier ${r.tier}`;
              return (
                <tr
                  key={r.tier}
                  className="clickable explore-tier-row"
                  title={`${names.name(g.id)} ${label} — add as a condition`}
                  onClick={() => onPick(g.id, tier)}
                >
                  <td className="name-cell">
                    <span className={`trait-badge trait-style-${TIER_STYLE[tier?.style ?? 1] ?? 1}`}>{label}</span>
                  </td>
                  <StatCells row={r} total={total} />
                </tr>
              );
            })}
          </tbody>
        ))}
      </table>
      {groups.length > 8 && (
        <button type="button" className="show-more" onClick={() => setAll(!all)}>
          {all ? "Show less" : `Show all ${groups.length} traits`}
        </button>
      )}
    </>
  );
}

const tierLabel = (t: TraitTier) =>
  t.maxUnits === 0 ? `${t.minUnits}+` : t.minUnits === t.maxUnits ? `${t.minUnits}` : `${t.minUnits}–${t.maxUnits}`;

function placementTone(r: PlacementStats): string {
  if (r.boards < 5) return "muted";
  return r.avgPlacement <= 4 ? "good" : r.avgPlacement >= 5 ? "bad" : "";
}

/** Searchable dropdown over ids present in the match data, most common first. */
interface PickerOption {
  id: string;
  boards: number;
  label?: string; // searchable text; defaults to the id's display name
  sublabel?: string; // shown instead of label on indented rows
  iconId?: string; // icon to show, if not the id's own
  indent?: boolean; // a sub-entry of the row above
  badge?: { text: string; style: number }; // shown instead of an icon
}

/**
 * Trait picker entries: each trait ("any tier"), then one indented entry per
 * tier seen in the data, so a tier can be picked directly. Tier entries'
 * ids are "<traitId>#<tier>".
 */
function traitPickerOptions(traits: ExploreOptions["traits"], names: Names): PickerOption[] {
  const out: PickerOption[] = [];
  for (const t of traits) {
    const name = names.name(t.id);
    out.push({ id: t.id, boards: t.boards, label: `${name} — any tier` });
    for (const tc of t.tiers) {
      const tier = names.tier(t.id, tc.tier);
      const text = tier ? tierLabel(tier) : `tier ${tc.tier}`;
      out.push({
        id: `${t.id}#${tc.tier}`,
        boards: tc.boards,
        // Searchable by trait name; shown as just the tier under it.
        label: `${name} ${text}`,
        sublabel: "units", // the tier badge already shows the range
        indent: true,
        badge: { text, style: TIER_STYLE[tier?.style ?? 1] ?? 1 },
      });
    }
  }
  return out;
}

function Picker({
  placeholder,
  kind,
  options,
  names,
  onPick,
  exclude = [],
}: {
  placeholder: string;
  kind: AssetKind;
  options: PickerOption[];
  names: Names;
  onPick: (id: string) => void;
  /** Ids already chosen, hidden from the list. */
  exclude?: string[];
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const label = (o: PickerOption) => o.label ?? names.name(o.id);
  const shown = options
    .filter((o) => !exclude.includes(o.id) && label(o).toLowerCase().includes(query.trim().toLowerCase()))
    .slice(0, 60);
  const pick = (id: string) => {
    onPick(id);
    setQuery("");
    setOpen(false);
  };
  return (
    <div className="picker">
      <input
        value={query}
        placeholder={placeholder}
        onChange={(e) => {
          setQuery(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && shown[0]) pick(shown[0].id);
          if (e.key === "Escape") setOpen(false);
        }}
      />
      {open && shown.length > 0 && (
        <ul className="picker-list">
          {shown.map((o) => (
            <li key={o.id} className={o.indent ? "picker-sub" : undefined} onMouseDown={() => pick(o.id)}>
              {o.badge ? (
                <span className={`trait-badge trait-style-${o.badge.style}`}>{o.badge.text}</span>
              ) : (
                <GameIcon
                  kind={kind}
                  id={o.iconId ?? o.id}
                  size={20}
                  fallbackSrc={names.icon(o.iconId ?? o.id)}
                  fallbackName={names.name(o.iconId ?? o.id)}
                />
              )}
              <span>{o.indent ? (o.sublabel ?? label(o)) : label(o)}</span>
              <span className="muted">{o.boards}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

interface TraitTier {
  index: number; // 1-based, as tier_current
  minUnits: number;
  maxUnits: number; // 0 for the top tier (no upper bound)
  style: number;
}

interface Names {
  name: (id: string) => string;
  icon: (id: string) => string | undefined;
  tiers: (traitId: string) => TraitTier[];
  /** The breakpoint for tier_current = tier (1-based). */
  tier: (traitId: string, tier: number) => TraitTier | undefined;
}

/** Display names, icons and trait tiers from the set data. */
function buildNames(d: SetData | null): Names {
  const byId = new Map<string, { name: string; icon?: string }>();
  const traitTiers = new Map<string, TraitTier[]>();
  if (d) {
    for (const x of [...d.units, ...d.items, ...(d.wisps ?? []), ...d.augments]) byId.set(x.apiName, x);
    for (const t of d.traits) {
      byId.set(t.apiName, t);
      const bps = t.breakpoints.filter((b) => b.minUnits > 0);
      traitTiers.set(
        t.apiName,
        bps.map((b, i) => ({
          index: i + 1,
          minUnits: b.minUnits,
          // A tier runs up to the next breakpoint; the last one is open.
          // Some traits repeat a breakpoint (Rival: 1, 1, 2), so a tier
          // never ends below where it starts.
          maxUnits: i + 1 < bps.length ? Math.max(b.minUnits, bps[i + 1].minUnits - 1) : 0,
          style: b.style,
        })),
      );
    }
  }
  return {
    name: (id) => byId.get(id)?.name ?? id.replace(/^(DA_18_|DA_|TFT\d*_Item_|TFT\d*_)/, ""),
    icon: (id) => byId.get(id)?.icon,
    tiers: (id) => traitTiers.get(id) ?? [],
    tier: (id, tier) => traitTiers.get(id)?.[tier - 1],
  };
}
