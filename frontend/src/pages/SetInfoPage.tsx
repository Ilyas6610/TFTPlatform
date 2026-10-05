import { useEffect, useMemo, useState } from "react";
import { Link, NavLink, useParams, useSearchParams } from "react-router-dom";
import {
  ApiError,
  PatchChange,
  PatchNotes,
  SetAugment,
  SetData,
  SetItem,
  SetTrait,
  SetUnit,
  getSetData,
  getSetPatches,
} from "../api/client";
import { AssetKind, GameIcon } from "../assets/tft";
import { CURRENT_TFT_SET } from "../config";

const TABS = [
  ["units", "Units"],
  ["traits", "Traits"],
  ["augments", "Augments"],
  ["items", "Items"],
  ["patches", "Patch notes"],
] as const;

type Tab = (typeof TABS)[number][0];

const ICON_KIND: Record<PatchChange["category"], AssetKind> = {
  unit: "champions",
  trait: "traits",
  augment: "augments",
  item: "items",
};

const CATEGORY_TAB: Record<PatchChange["category"], Tab> = {
  unit: "units",
  trait: "traits",
  augment: "augments",
  item: "items",
};

export default function SetInfoPage() {
  const { set = String(CURRENT_TFT_SET), tab = "units" } = useParams();
  const [params, setParams] = useSearchParams();
  const setNumber = Number(set);
  const version = params.get("version") ?? undefined;
  const query = params.get("q") ?? "";

  const [data, setData] = useState<SetData | null>(null);
  const [patches, setPatches] = useState<PatchNotes[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    getSetData(setNumber, version)
      .then((d) => !cancelled && setData(d))
      .catch((e: unknown) => {
        if (cancelled) return;
        setData(null);
        setError(e instanceof ApiError ? e.message : "failed to load set data");
      });
    return () => {
      cancelled = true;
    };
  }, [setNumber, version]);

  useEffect(() => setPatches(null), [setNumber]);

  useEffect(() => {
    if (tab !== "patches" || patches) return;
    getSetPatches(setNumber)
      .then(setPatches)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "failed to load patch notes"));
  }, [setNumber, tab, patches]);

  function updateParam(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next, { replace: true });
  }

  const tabQuery = version ? `?version=${encodeURIComponent(version)}` : "";

  return (
    <div>
      <div className="set-picker set-info-header">
        <h2>Set {setNumber}</h2>
        {data && (
          <>
            <label htmlFor="patch-select" className="muted">
              Patch:
            </label>
            <select
              id="patch-select"
              value={data.version}
              onChange={(e) => updateParam("version", e.target.value === data.versions[0].version ? "" : e.target.value)}
            >
              {data.versions.map((v, i) => (
                <option key={v.version} value={v.version}>
                  {v.patch}
                  {i === 0 ? " (latest)" : ""}
                </option>
              ))}
            </select>
          </>
        )}
      </div>

      <nav className="tabs">
        {TABS.map(([id, label]) => (
          <NavLink key={id} to={`/set/${setNumber}/${id}${tabQuery}`} className={tab === id ? "active" : ""}>
            {label}
          </NavLink>
        ))}
      </nav>

      {error && <div className="error-box">{error}</div>}
      {!error && !data && <p className="muted">Loading...</p>}

      {data && tab !== "patches" && (
        <input
          className="set-search"
          type="search"
          placeholder={`Search ${tab}…`}
          value={query}
          onChange={(e) => updateParam("q", e.target.value)}
        />
      )}

      {data && tab === "units" && <UnitsTab data={data} query={query} />}
      {data && tab === "traits" && <TraitsTab data={data} query={query} />}
      {data && tab === "augments" && <AugmentsTab augments={data.augments} query={query} />}
      {data && tab === "items" && <ItemsTab items={data.items} query={query} />}
      {data && tab === "patches" && <PatchesTab data={data} patches={patches} setNumber={setNumber} />}
    </div>
  );
}

function matches(query: string, ...fields: (string | undefined)[]): boolean {
  const q = query.trim().toLowerCase();
  return !q || fields.some((f) => f?.toLowerCase().includes(q));
}

function fmt(n: number): string {
  return String(Math.round(n * 100) / 100);
}

/** Description text with line breaks preserved. */
function Desc({ text }: { text: string }) {
  return <p className="set-desc">{text}</p>;
}

const STAT_LABELS: [key: string, label: string, format?: (n: number) => string][] = [
  ["hp", "Health"],
  ["damage", "Attack Damage"],
  ["attackSpeed", "Attack Speed"],
  ["armor", "Armor"],
  ["magicResist", "Magic Resist"],
  ["initialMana", "Starting Mana"],
  ["mana", "Mana"],
  ["range", "Range"],
  ["critChance", "Crit Chance", (n) => `${fmt(n * 100)}%`],
  ["critMultiplier", "Crit Damage", (n) => `${fmt(n * 100)}%`],
];

function traitLookup(traits: SetTrait[]) {
  return new Map(traits.map((t) => [t.name, t]));
}

function UnitsTab({ data, query }: { data: SetData; query: string }) {
  const traits = useMemo(() => traitLookup(data.traits), [data.traits]);
  const units = data.units.filter((u) => matches(query, u.name, u.ability.name, u.ability.desc, ...u.traits));
  return (
    <>
      <p className="muted">
        Ability numbers the game calculates at runtime aren&apos;t in Riot&apos;s published data for this set, so
        they show as &quot;?&quot;. Stats, traits and costs are exact.
      </p>
      {units.length === 0 && <p className="muted">No units match.</p>}
      <div className="set-grid">
        {units.map((u) => (
          <UnitCard key={u.apiName} unit={u} traits={traits} />
        ))}
      </div>
    </>
  );
}

function UnitCard({ unit: u, traits }: { unit: SetUnit; traits: Map<string, SetTrait> }) {
  return (
    <div className="panel set-card" id={u.apiName}>
      <div className="set-card-head">
        <GameIcon kind="champions" id={u.apiName} size={56} fallbackSrc={u.icon} fallbackName={u.name} className={`cost-${u.cost}`} />
        <div>
          <h3>{u.name}</h3>
          <span className="muted">{u.cost}-cost</span>
          <div className="set-trait-list">
            {u.traits.map((name) => {
              const t = traits.get(name);
              return (
                <span key={name} className="game-label">
                  {t && <GameIcon kind="traits" id={t.apiName} size={16} fallbackSrc={t.icon} fallbackName={t.name} />}
                  {name}
                </span>
              );
            })}
          </div>
        </div>
      </div>
      <dl className="set-stats">
        {STAT_LABELS.filter(([k]) => u.stats[k] !== undefined).map(([k, label, format]) => (
          <div key={k}>
            <dt>{label}</dt>
            <dd>{(format ?? fmt)(u.stats[k])}</dd>
          </div>
        ))}
      </dl>
      {u.ability.name && (
        <>
          <h4>{u.ability.name}</h4>
          <Desc text={u.ability.desc} />
        </>
      )}
    </div>
  );
}

// CommunityDragon trait styles -> the bronze/silver/gold/prismatic classes
// used for match traits.
const BREAKPOINT_STYLE: Record<number, number> = { 1: 1, 2: 2, 3: 2, 4: 3, 5: 3, 6: 4 };

function TraitsTab({ data, query }: { data: SetData; query: string }) {
  const traits = data.traits.filter((t) =>
    matches(query, t.name, t.desc, ...t.breakpoints.map((b) => b.text)),
  );
  return (
    <div className="set-list">
      {traits.length === 0 && <p className="muted">No traits match.</p>}
      {traits.map((t) => {
        const units = data.units.filter((u) => u.traits.includes(t.name));
        return (
          <div className="panel" key={t.apiName} id={t.apiName}>
            <div className="set-card-head">
              <GameIcon kind="traits" id={t.apiName} size={40} fallbackSrc={t.icon} fallbackName={t.name} />
              <h3>{t.name}</h3>
            </div>
            {t.desc && <Desc text={t.desc} />}
            {t.breakpoints.some((b) => b.text) && (
              <ul className="breakpoints">
                {t.breakpoints.map((b, i) => (
                  <li key={i}>
                    <span className={`trait-badge trait-style-${BREAKPOINT_STYLE[b.style] ?? 1}`}>{b.minUnits}</span>
                    <span className="set-desc">{b.text?.replace(/^\(\d+\)\s*/, "")}</span>
                  </li>
                ))}
              </ul>
            )}
            {units.length > 0 && (
              <div className="unit-grid">
                {units.map((u) => (
                  <GameIcon key={u.apiName} kind="champions" id={u.apiName} size={36} fallbackSrc={u.icon} fallbackName={u.name} className={`cost-${u.cost}`} />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

const TIER_NAMES = ["Other", "Silver", "Gold", "Prismatic"];

function AugmentsTab({ augments, query }: { augments: SetAugment[]; query: string }) {
  const [tier, setTier] = useState<number | null>(null);
  const shown = augments.filter((a) => (tier === null || a.tier === tier) && matches(query, a.name, a.desc, ...(a.traits ?? [])));
  return (
    <>
      <div className="set-picker">
        {[null, 1, 2, 3].map((t) => (
          <button key={String(t)} type="button" disabled={tier === t} onClick={() => setTier(t)}>
            {t === null ? "All" : TIER_NAMES[t]}
          </button>
        ))}
        <span className="muted">{shown.length} augments</span>
      </div>
      <div className="set-list">
        {shown.map((a) => (
          <div className="panel set-row" key={a.apiName} id={a.apiName}>
            <GameIcon kind="augments" id={a.apiName} size={40} fallbackSrc={a.icon} fallbackName={a.name} />
            <div>
              <h4>
                {a.name} <span className={`tier-tag tier-${a.tier}`}>{TIER_NAMES[a.tier]}</span>
                {a.traits?.map((t) => (
                  <span key={t} className="tag">
                    {t}
                  </span>
                ))}
              </h4>
              <Desc text={a.desc} />
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

const ITEM_SECTIONS: [kind: string, title: string][] = [
  ["component", "Components"],
  ["completed", "Completed items"],
  ["emblem", "Emblems"],
  ["artifact", "Artifacts"],
  ["radiant", "Radiant items"],
  ["consumable", "Consumables"],
  ["special", "Set-specific"],
  ["other", "Other"],
];

function ItemsTab({ items, query }: { items: SetItem[]; query: string }) {
  const byApi = useMemo(() => new Map(items.map((i) => [i.apiName, i])), [items]);
  const shown = items.filter((i) => matches(query, i.name, i.desc));
  return (
    <>
      {shown.length === 0 && <p className="muted">No items match.</p>}
      {ITEM_SECTIONS.map(([kind, title]) => {
        // Same-named variants (base / upgraded / prismatic) share one card.
        const groups = new Map<string, SetItem[]>();
        for (const it of shown.filter((i) => i.kind === kind)) {
          groups.set(it.name, [...(groups.get(it.name) ?? []), it]);
        }
        if (groups.size === 0) return null;
        return (
          <section key={kind}>
            <h3>
              {title} <span className="muted">({groups.size})</span>
            </h3>
            <div className="set-list">
              {[...groups.values()].map((variants) => {
                const first = variants[0];
                return (
                  <div className="panel set-row" key={first.apiName} id={first.apiName}>
                    <GameIcon kind="items" id={first.apiName} size={40} fallbackSrc={first.icon} fallbackName={first.name} />
                    <div>
                      <h4>
                        {first.name}
                        {first.composition && first.composition.length === 2 && (
                          <span className="item-recipe">
                            {first.composition.map((c, i) => (
                              <GameIcon key={i} kind="items" id={c} size={20} fallbackSrc={byApi.get(c)?.icon} fallbackName={byApi.get(c)?.name} />
                            ))}
                          </span>
                        )}
                      </h4>
                      {variants.map((v) => (
                        <div key={v.apiName}>
                          {variants.length > 1 && <span className="tag">{v.variant || "base"}</span>}
                          <Desc text={v.desc} />
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          </section>
        );
      })}
    </>
  );
}

const FIELD_LABELS: Record<string, string> = Object.fromEntries(STAT_LABELS.map(([k, label]) => [k, label]));

/** "InvokerManaBonus" -> "Invoker Mana Bonus"; keeps a "(5) " breakpoint prefix. */
function fieldLabel(field: string): string {
  if (FIELD_LABELS[field]) return FIELD_LABELS[field];
  return field.replace(/([a-z])([A-Z])/g, "$1 $2").replace(/^./, (c) => c.toUpperCase());
}

const CATEGORY_TITLES: Record<PatchChange["category"], string> = {
  unit: "Units",
  trait: "Traits",
  augment: "Augments",
  item: "Items",
};

function PatchesTab({ data, patches, setNumber }: { data: SetData; patches: PatchNotes[] | null; setNumber: number }) {
  // Icons for changed entities come from the latest data (removed ones
  // fall back to text).
  const icons = useMemo(() => {
    const m = new Map<string, string | undefined>();
    for (const x of [...data.units, ...data.traits, ...data.augments, ...data.items]) m.set(x.apiName, x.icon);
    return m;
  }, [data]);

  if (!patches) return <p className="muted">Loading...</p>;
  if (patches.length === 0) {
    return <p className="muted">Only one patch has been recorded for this set so far — notes appear after the next patch.</p>;
  }
  return (
    <div>
      <p className="muted">
        Generated by comparing each patch&apos;s game data with the previous one: every number that changed, plus
        anything added or removed.
      </p>
      {patches.map((p) => (
        <div className="panel" key={p.version}>
          <h2>
            Patch {p.patch} <span className="muted patch-sub">vs {p.previousPatch} · {p.changes.length} changes</span>
          </h2>
          {p.changes.length === 0 && <p className="muted">No changes to this set's numbers.</p>}
          {(["unit", "trait", "augment", "item"] as const).map((category) => {
            const changes = p.changes.filter((c) => c.category === category);
            if (changes.length === 0) return null;
            const entities = new Map<string, PatchChange[]>();
            for (const c of changes) entities.set(c.apiName, [...(entities.get(c.apiName) ?? []), c]);
            return (
              <section key={category}>
                <h3>{CATEGORY_TITLES[category]}</h3>
                <ul className="patch-list">
                  {[...entities.values()].map((cs) => (
                    <li key={cs[0].apiName}>
                      <Link
                        className="game-label"
                        to={`/set/${setNumber}/${CATEGORY_TAB[category]}?q=${encodeURIComponent(cs[0].name)}`}
                      >
                        <GameIcon kind={ICON_KIND[category]} id={cs[0].apiName} size={24} fallbackSrc={icons.get(cs[0].apiName)} fallbackName={cs[0].name} />
                        {cs[0].name}
                      </Link>
                      <ul>
                        {cs.map((c, i) => (
                          <li key={i}>
                            <ChangeLine change={c} />
                          </li>
                        ))}
                      </ul>
                    </li>
                  ))}
                </ul>
              </section>
            );
          })}
        </div>
      ))}
    </div>
  );
}

function ChangeLine({ change: c }: { change: PatchChange }) {
  if (c.kind === "added") return <span className="change-added">New</span>;
  if (c.kind === "removed") return <span className="change-removed">Removed</span>;
  if (c.kind === "text") return <span className="muted">Description updated</span>;
  const direction = c.old !== undefined && c.new !== undefined ? (c.new > c.old ? "up" : "down") : null;
  return (
    <span>
      {fieldLabel(c.field ?? "")}:{" "}
      <span className="muted">{c.old !== undefined ? fmt(c.old) : "—"}</span> →{" "}
      <strong>{c.new !== undefined ? fmt(c.new) : "—"}</strong>
      {direction && <span className={`change-${direction}`}>{direction === "up" ? " ▲" : " ▼"}</span>}
    </span>
  );
}
