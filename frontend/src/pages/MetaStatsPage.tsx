import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  ApiError,
  ExploreOptions,
  ExploreRow,
  MetaComp,
  MetaResult,
  MetaUnit,
  SetData,
  explore,
  getExploreOptions,
  getMetaBuilds,
  getMetaComps,
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

type Tab = "comps" | "units" | "traits";
type Sort = "games" | "avg";
// One Explorer condition as [param, value], e.g. ["unit", "DA_Ahri18:DA_Blue"].
type ExploreCond = [string, string];

export default function MetaStatsPage() {
  const { set = String(CURRENT_TFT_SET) } = useParams();
  const setNumber = Number(set) || CURRENT_TFT_SET;
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const tabParam = params.get("tab");
  const tab: Tab = tabParam === "units" || tabParam === "traits" ? tabParam : "comps";
  const sort: Sort = params.get("sort") === "avg" ? "avg" : "games";
  const queue = params.get("queue") ?? "";
  const [query, setQuery] = useState("");
  const [cost, setCost] = useState<number | null>(null);
  const [showAll, setShowAll] = useState(false);

  const [options, setOptions] = useState<ExploreOptions | null>(null);
  const [setData, setSetData] = useState<SetData | null>(null);
  const [meta, setMeta] = useState<MetaResult | null>(null);
  const [traits, setTraits] = useState<ExploreRow[] | null>(null);
  const [comps, setComps] = useState<{ boards: number; comps: MetaComp[] } | null>(null);
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
      tab === "comps"
        ? getMetaComps(scope).then((c) => !cancelled && setComps(c))
        : tab === "units"
          ? getMetaBuilds(scope).then((m) => !cancelled && setMeta(m))
          : explore(scope).then((r) => !cancelled && setTraits(r.traits));
    load.catch((e: unknown) => !cancelled && setError(e instanceof ApiError ? e.message : "failed to load meta"));
    return () => {
      cancelled = true;
    };
  }, [scope, tab, options]);

  useEffect(() => setShowAll(false), [scope, cost, sort, query]);

  const names = useMemo(() => buildNames(setData), [setData]);
  // Boards in the selected queue (or all of them).
  const scopeBoards =
    queue && queue !== "any" ? options?.queues.find((q) => String(q.id) === queue)?.boards : options?.boards;
  const costs = useMemo(() => new Map(setData?.units.map((u) => [u.apiName, u.cost]) ?? []), [setData]);
  // Explorer URL for the current scope plus conditions, encoded by URLSearchParams.
  const exploreLink = (conds: ExploreCond[]) => {
    const q = new URLSearchParams(scope);
    if (queue === "any") q.set("anyQueue", "1");
    for (const [k, v] of conds) q.append(k, v);
    return `/explore?${q}`;
  };

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
            {scopeBoards !== undefined ? `${scopeBoards} boards` : "…"} from ingested matches. Builds are exact 3-item
            sets seen at least 3 times; click any build or row to dig into it in the Explorer.
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
        {(["comps", "units", "traits"] as const).map((t) => (
          <a
            key={t}
            href={`?${new URLSearchParams({ ...Object.fromEntries(params), tab: t })}`}
            className={tab === t ? "active" : ""}
            onClick={(e) => {
              e.preventDefault();
              update("tab", t === "comps" ? "" : t);
            }}
          >
            {t === "comps" ? "Comps" : t === "units" ? "Units & builds" : "Traits"}
          </a>
        ))}
      </nav>

      {error && <div className="error-box">{error}</div>}

      {tab === "comps" && (
        <>
          <p className="muted">
            Team comps grouped from {comps ? `${comps.boards} ` : ""}final boards (level 8+). Each shows the exact board
            players run most, with the usual star levels and items, and the ways players adjust it.
          </p>
          {!comps && !error && <p className="muted">Loading...</p>}
          {comps && comps.comps.length === 0 && <p className="muted">Not enough games yet to group comps.</p>}
          <div className="comp-list">
            {comps?.comps.map((c, i) => (
              <CompCard
                key={c.board.map((u) => u.id).join(",")}
                comp={c}
                rank={i + 1}
                names={names}
                costs={costs}
                exploreLink={exploreLink}
              />
            ))}
          </div>
        </>
      )}

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
                navigate(exploreLink([["trait", `${id}${tier ? `*${tier.minUnits}-${tier.maxUnits || ""}` : ""}`]]))
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
  exploreLink: (conds: ExploreCond[]) => string;
}) {
  const lowSample = u.boards < MIN_GAMES_FOR_AVG_SORT;
  return (
    <div className="panel meta-card">
      <Link className="meta-card-head" to={exploreLink([["unit", u.id]])} title="Open in Explorer">
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
              to={exploreLink([["unit", `${u.id}:${b.items.join(",")}`]])}
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
                to={exploreLink([["unit", `${u.id}:${it.id}`]])}
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

function CompCard({
  comp: c,
  rank,
  names,
  costs,
  exploreLink,
}: {
  comp: MetaComp;
  rank: number;
  names: Names;
  costs: Map<string, number>;
  exploreLink: (conds: ExploreCond[]) => string;
}) {
  // Named by the traits it invests in (a higher tier or 3+ units), then its
  // itemized carries. Flexible boards with only 2-unit traits go by their
  // carries alone ("Aphelios & Nidalee").
  const mainTraits = c.traits
    .filter((t) => t.tier >= 2 || t.units >= 3)
    .sort((a, b) => b.tier - a.tier || b.units - a.units)
    .slice(0, 2);
  const carries = c.board.filter((u) => u.items.length > 0).slice(0, 2);
  // The Explorer takes up to 6 unit conditions: the comp's most core units.
  const core = [...c.board].sort((a, b) => b.frequency - a.frequency).slice(0, 6);
  const unitIcon = (id: string, size: number) => (
    <GameIcon
      kind="champions"
      id={id}
      size={size}
      fallbackSrc={names.icon(id)}
      fallbackName={names.name(id)}
      className={costs.get(id) ? `cost-${costs.get(id)}` : ""}
    />
  );
  return (
    <div className="panel comp-card">
      <div className="comp-head">
        <span className="comp-rank muted">#{rank}</span>
        <div className="comp-title">
          <div className="comp-name">
            {mainTraits.map((t) => (
              <span key={t.id} className="game-label">
                <GameIcon
                  kind="traits"
                  id={t.id}
                  size={18}
                  fallbackSrc={names.icon(t.id)}
                  fallbackName={names.name(t.id)}
                />
                {names.name(t.id)} {t.units}
              </span>
            ))}
            {carries.length > 0 && (
              <span className={mainTraits.length > 0 ? "muted" : undefined}>
                {mainTraits.length > 0 ? "· " : ""}
                {carries.map((u) => names.name(u.id)).join(" & ")}
              </span>
            )}
          </div>
          <div className="meta-stats">
            <span>
              <b>{c.boards}</b> games
            </span>
            <span className={placementTone(c)}>
              <b>{avg(c.avgPlacement)}</b> avg
            </span>
            <span>
              <b>{pct(c.top4Rate)}</b> top 4
            </span>
            <span>
              <b>{pct(c.winRate)}</b> win
            </span>
            <span className="muted">{pct(c.playRate)} of boards</span>
          </div>
        </div>
        <Link className="comp-explore" to={exploreLink(core.map((u): ExploreCond => ["unit", u.id]))}>
          Explore →
        </Link>
      </div>

      <div className="comp-board">
        {c.board.map((u) => (
          <div
            key={u.id}
            className="comp-unit"
            title={`${names.name(u.id)} — in ${pct(u.frequency)} of this comp's boards`}
          >
            <span className="unit-stars">{"★".repeat(u.star || 1)}</span>
            {unitIcon(u.id, 52)}
            <span className="comp-unit-name">{names.name(u.id)}</span>
            <span className="unit-items">
              {u.items.map((it, i) => (
                <GameIcon
                  key={i}
                  kind="items"
                  id={it}
                  size={17}
                  fallbackSrc={names.icon(it)}
                  fallbackName={names.name(it)}
                />
              ))}
            </span>
          </div>
        ))}
      </div>
      <p className="muted comp-exact">
        Exact board: <b>{c.boardStats.boards}</b> games ·{" "}
        <span className={placementTone(c.boardStats)}>{avg(c.boardStats.avgPlacement)}</span> avg ·{" "}
        {pct(c.boardStats.top4Rate)} top 4
      </p>

      {(c.variants.length > 0 || c.flex.length > 0) && (
        <div className="comp-alternatives">
          {c.variants.length > 0 && (
            <div>
              <div className="meta-section-title muted">Variations</div>
              {c.variants.map((v, i) => (
                <div key={i} className="comp-variant">
                  <span className="comp-swap">
                    {v.remove.map((id) => (
                      <span key={id} className="swap-out" title={`without ${names.name(id)}`}>
                        −{unitIcon(id, 24)}
                      </span>
                    ))}
                    {v.add.map((id) => (
                      <span key={id} className="swap-in" title={`with ${names.name(id)}`}>
                        +{unitIcon(id, 24)}
                      </span>
                    ))}
                    <span className="muted swap-label">
                      {v.remove.length === 0 ? "level up: " : ""}
                      {[...v.remove.map((id) => `−${names.name(id)}`), ...v.add.map((id) => `+${names.name(id)}`)].join(
                        " ",
                      )}
                    </span>
                  </span>
                  <span className="build-stats">
                    <b>{v.boards}</b>
                    <span className={v.boards < 5 ? "muted" : placementTone(v)}>{avg(v.avgPlacement)}</span>
                    <span className="muted">{pct(v.top4Rate)}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
          {c.flex.length > 0 && (
            <div>
              <div className="meta-section-title muted">Flex units</div>
              <div className="comp-flex">
                {c.flex.map((f) => (
                  <span
                    key={f.id}
                    className="comp-flex-unit"
                    title={`${names.name(f.id)}: in ${pct(f.frequency)} of boards, ${avg(f.avgPlacement)} avg when played`}
                  >
                    {unitIcon(f.id, 28)}
                    <span>
                      {pct(f.frequency)}
                      <br />
                      <span className={placementTone(f)}>{avg(f.avgPlacement)}</span>
                    </span>
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
