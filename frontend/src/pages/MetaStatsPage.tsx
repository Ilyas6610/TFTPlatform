import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  ApiError,
  ExploreOptions,
  ExploreRow,
  MetaResult,
  MetaUnit,
  SetData,
  explore,
  getExploreOptions,
  getMetaBuilds,
  getSetData,
} from "../api/client";
import { GameIcon } from "../assets/tft";
import { Names, TraitsBreakdown, avg, buildNames, pct, placementTone } from "../components/stats";
import { CURRENT_TFT_SET } from "../config";

// Meta: how every unit performs and is built, and how traits do by tier,
// computed live from ingested matches (default: ranked games). Builds are
// exact 3-item sets; anything here links into the Explorer for a closer look.

const QUEUE_NAMES: Record<number, string> = { 1090: "Normal", 1100: "Ranked", 1130: "Hyper Roll", 1160: "Double Up" };
const MIN_GAMES_FOR_AVG_SORT = 10;
const UNITS_PREVIEW = 24;

type Tab = "units" | "traits";
type Sort = "games" | "avg";

export default function MetaStatsPage() {
  const { set = String(CURRENT_TFT_SET) } = useParams();
  const setNumber = Number(set) || CURRENT_TFT_SET;
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const tab: Tab = params.get("tab") === "traits" ? "traits" : "units";
  const sort: Sort = params.get("sort") === "avg" ? "avg" : "games";
  const queue = params.get("queue") ?? "";
  const [query, setQuery] = useState("");
  const [cost, setCost] = useState<number | null>(null);
  const [showAll, setShowAll] = useState(false);

  const [options, setOptions] = useState<ExploreOptions | null>(null);
  const [setData, setSetData] = useState<SetData | null>(null);
  const [meta, setMeta] = useState<MetaResult | null>(null);
  const [traits, setTraits] = useState<ExploreRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  function update(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next, { replace: true });
  }

  useEffect(() => {
    setOptions(null);
    getExploreOptions(setNumber)
      .then(setOptions)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "failed to load meta"));
    getSetData(setNumber)
      .then(setSetData)
      .catch(() => setSetData(null));
  }, [setNumber]);

  // Default to ranked games when the set has them.
  useEffect(() => {
    if (options && !params.has("queue") && options.queues.some((q) => q.id === 1100)) update("queue", "1100");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options]);

  const scope = useMemo(() => {
    const q = new URLSearchParams({ set: String(setNumber) });
    if (queue && queue !== "any") q.set("queue", queue);
    return q.toString();
  }, [setNumber, queue]);

  useEffect(() => {
    if (!options) return; // wait for the default queue
    let cancelled = false;
    setError(null);
    const load =
      tab === "units"
        ? getMetaBuilds(scope).then((m) => !cancelled && setMeta(m))
        : explore(scope).then((r) => !cancelled && setTraits(r.traits));
    load.catch((e: unknown) => !cancelled && setError(e instanceof ApiError ? e.message : "failed to load meta"));
    return () => {
      cancelled = true;
    };
  }, [scope, tab, options]);

  useEffect(() => setShowAll(false), [scope, cost, sort, query]);

  const names = useMemo(() => buildNames(setData), [setData]);
  const costs = useMemo(() => new Map(setData?.units.map((u) => [u.apiName, u.cost]) ?? []), [setData]);
  const exploreLink = (extra: string) => `/explore?${scope}${queue === "any" ? "&anyQueue=1" : ""}&${extra}`;

  const units = useMemo(() => {
    if (!meta) return [];
    const q = query.trim().toLowerCase();
    const list = meta.units.filter(
      (u) => (cost === null || costs.get(u.id) === cost) && (!q || names.name(u.id).toLowerCase().includes(q)),
    );
    if (sort === "avg") {
      // Ranking by average needs a sample; rarely played units go last.
      const reliable = (u: MetaUnit) => u.boards >= MIN_GAMES_FOR_AVG_SORT;
      list.sort((a, b) => Number(reliable(b)) - Number(reliable(a)) || a.avgPlacement - b.avgPlacement);
    }
    return list;
  }, [meta, query, cost, sort, costs, names]);
  const shownUnits = showAll || query ? units : units.slice(0, UNITS_PREVIEW);

  return (
    <div className="meta-page">
      <div className="panel meta-header">
        <div>
          <h2>Set {setNumber} meta</h2>
          <p className="muted">
            {meta ? `${meta.boards} boards` : options ? `${options.boards} boards` : "…"} from ingested matches. Builds
            are exact 3-item sets seen at least 3 times; click any build or row to dig into it in the Explorer.
          </p>
        </div>
        <div className="set-picker">
          <label className="muted">
            Queue{" "}
            <select value={queue || "any"} onChange={(e) => update("queue", e.target.value)}>
              <option value="any">All queues</option>
              {options?.queues.map((q) => (
                <option key={q.id} value={q.id}>
                  {QUEUE_NAMES[q.id] ?? `Queue ${q.id}`} · {q.boards}
                </option>
              ))}
            </select>
          </label>
          <label className="muted">
            Set{" "}
            <select value={setNumber} onChange={(e) => navigate(`/meta/${e.target.value}?${params.toString()}`)}>
              {[CURRENT_TFT_SET, CURRENT_TFT_SET - 1].map((n) => (
                <option key={n} value={n}>
                  Set {n}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      <nav className="tabs">
        {(["units", "traits"] as const).map((t) => (
          <a
            key={t}
            href={`?${new URLSearchParams({ ...Object.fromEntries(params), tab: t })}`}
            className={tab === t ? "active" : ""}
            onClick={(e) => {
              e.preventDefault();
              update("tab", t === "units" ? "" : t);
            }}
          >
            {t === "units" ? "Units & builds" : "Traits"}
          </a>
        ))}
      </nav>

      {error && <div className="error-box">{error}</div>}

      {tab === "units" && (
        <>
          <div className="set-picker">
            {[null, 1, 2, 3, 4, 5].map((c) => (
              <button key={String(c)} type="button" disabled={cost === c} onClick={() => setCost(c)}>
                {c === null ? "All" : `${c}-cost`}
              </button>
            ))}
            <input
              className="meta-search"
              placeholder="Search units…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <label className="muted">
              Sort{" "}
              <select value={sort} onChange={(e) => update("sort", e.target.value === "games" ? "" : e.target.value)}>
                <option value="games">Most played</option>
                <option value="avg">Best average ({MIN_GAMES_FOR_AVG_SORT}+ games)</option>
              </select>
            </label>
          </div>
          {!meta && !error && <p className="muted">Loading...</p>}
          {meta && units.length === 0 && <p className="muted">No units match.</p>}
          <div className="meta-grid">
            {shownUnits.map((u) => (
              <UnitCard key={u.id} unit={u} cost={costs.get(u.id)} names={names} exploreLink={exploreLink} />
            ))}
          </div>
          {!showAll && !query && units.length > UNITS_PREVIEW && (
            <button type="button" className="show-more" onClick={() => setShowAll(true)}>
              Show all {units.length} units
            </button>
          )}
        </>
      )}

      {tab === "traits" && (
        <div className="panel meta-traits">
          {!traits && !error && <p className="muted">Loading...</p>}
          {traits && (
            <TraitsBreakdown
              rows={traits}
              names={names}
              total={options?.queues.find((q) => String(q.id) === queue)?.boards ?? options?.boards ?? 0}
              onPick={(id, tier) =>
                navigate(exploreLink(`trait=${id}${tier ? `*${tier.minUnits}-${tier.maxUnits || ""}` : ""}`))
              }
            />
          )}
        </div>
      )}
    </div>
  );
}

function UnitCard({
  unit: u,
  cost,
  names,
  exploreLink,
}: {
  unit: MetaUnit;
  cost?: number;
  names: Names;
  exploreLink: (extra: string) => string;
}) {
  const lowSample = u.boards < MIN_GAMES_FOR_AVG_SORT;
  return (
    <div className="panel meta-card">
      <Link className="meta-card-head" to={exploreLink(`unit=${u.id}`)} title="Open in Explorer">
        <GameIcon
          kind="champions"
          id={u.id}
          size={44}
          fallbackSrc={names.icon(u.id)}
          fallbackName={names.name(u.id)}
          className={cost ? `cost-${cost}` : ""}
        />
        <div>
          <strong>{names.name(u.id)}</strong>
          {cost && <span className="muted"> · {cost}-cost</span>}
          <div className="meta-stats">
            <span>
              <b>{u.boards}</b> games
            </span>
            <span className={lowSample ? "muted" : placementTone(u)}>
              <b>{avg(u.avgPlacement)}</b> avg
            </span>
            <span>
              <b>{pct(u.top4Rate)}</b> top 4
            </span>
            <span className="muted">{pct(u.pickRate)} pick</span>
          </div>
        </div>
      </Link>

      {u.builds.length > 0 ? (
        <>
          <div className="meta-section-title muted meta-build-head">
            <span>Top builds</span>
            <span className="build-stats">
              <span>games</span>
              <span>avg</span>
              <span>top 4</span>
            </span>
          </div>
          {u.builds.map((b) => (
            <Link
              key={b.items.join("+")}
              className="meta-build"
              to={exploreLink(`unit=${u.id}:${b.items.join(",")}`)}
              title="Open this exact build in Explorer"
            >
              <span className="build-items">
                {b.items.map((it, i) => (
                  <GameIcon
                    key={i}
                    kind="items"
                    id={it}
                    size={26}
                    fallbackSrc={names.icon(it)}
                    fallbackName={names.name(it)}
                  />
                ))}
              </span>
              <span className="build-stats">
                <b>{b.boards}</b>
                <span className={b.boards < 5 ? "muted" : placementTone(b)}>{avg(b.avgPlacement)}</span>
                <span className="muted">{pct(b.top4Rate)}</span>
              </span>
            </Link>
          ))}
        </>
      ) : (
        <div className="meta-section-title muted">No full build seen 3+ times yet</div>
      )}

      {u.items.length > 0 && (
        <>
          <div className="meta-section-title muted">Most used items</div>
          <div className="meta-items">
            {u.items.map((it) => (
              <Link
                key={it.id}
                to={exploreLink(`unit=${u.id}:${it.id}`)}
                title={`${names.name(it.id)}: ${it.boards} games, ${avg(it.avgPlacement)} avg`}
              >
                <GameIcon
                  kind="items"
                  id={it.id}
                  size={24}
                  fallbackSrc={names.icon(it.id)}
                  fallbackName={names.name(it.id)}
                />
                <span className="muted">{it.boards}</span>
              </Link>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
