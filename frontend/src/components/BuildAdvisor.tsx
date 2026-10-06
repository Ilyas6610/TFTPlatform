import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  ApiError,
  ExploreOptions,
  SetData,
  SuggestBuild,
  SuggestComp,
  SuggestResult,
  SuggestStep,
  getExploreSuggest,
} from "../api/client";
import { GameIcon } from "../assets/tft";
import { CompName } from "./CompName";
import { Picker, PickerOption } from "./Picker";
import { Names, avg, placementTone, pct } from "./stats";

// "What can I build?": the player lists the units they hold and the items in
// their inventory (whole or still components) and gets builds from real
// boards that fit. The lists live in the URL (hu = unit, hi = item, repeated
// once per copy) so a situation is a shareable link; the board scope (set,
// queue, level) comes from the Explorer's own controls.

const MAX_UNITS = 15;
const MAX_ITEMS = 30;

export function BuildAdvisor({
  scope,
  options,
  setData,
  names,
}: {
  /** Explorer scope as an API query: set, queue, level. */
  scope: string;
  options: ExploreOptions | null;
  setData: SetData | null;
  names: Names;
}) {
  const [params, setParams] = useSearchParams();
  const units = params.getAll("hu");
  const items = params.getAll("hi");

  const [result, setResult] = useState<SuggestResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const setList = (key: string, values: string[]) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete(key);
        values.forEach((v) => next.append(key, v));
        return next;
      },
      { replace: true },
    );

  const query = useMemo(() => {
    const q = new URLSearchParams(scope);
    units.forEach((u) => q.append("have_unit", u));
    items.forEach((i) => q.append("have_item", i));
    return q.toString();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, units.join(","), items.join(",")]);

  useEffect(() => {
    if (!options || (units.length === 0 && items.length === 0)) {
      setResult(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    getExploreSuggest(query)
      .then((r) => !cancelled && setResult(r))
      .catch((e: unknown) => !cancelled && setError(e instanceof ApiError ? e.message : "couldn't load suggestions"))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [query, options]); // eslint-disable-line react-hooks/exhaustive-deps

  // What you can hold, by field: components and completed items, emblems
  // and artifacts. All end up in one inventory (hi); the fields only keep the
  // pickers short. Without set data, only the completed items seen in matches.
  const groups = useMemo(() => {
    const optionsFor = (pred: (k: string) => boolean): PickerOption[] =>
      (setData?.items ?? [])
        .filter((i) => pred(i.kind))
        .sort((a, b) => (a.kind === b.kind ? a.name.localeCompare(b.name) : a.kind === "component" ? -1 : 1))
        .map((i) => ({ id: i.apiName, boards: 0, label: `${i.name}${i.kind === "component" ? " (component)" : ""}` }));
    const base = setData
      ? optionsFor((k) => k === "component" || k === "completed")
      : (options?.items ?? []).map((i) => ({ id: i.id, boards: 0 }));
    return [
      { key: "items", placeholder: "Add item…", options: base },
      { key: "emblems", placeholder: "Add emblem…", options: optionsFor((k) => k === "emblem") },
      { key: "artifacts", placeholder: "Add artifact…", options: optionsFor((k) => k === "artifact") },
    ].filter((g) => g.key === "items" || g.options.length > 0);
  }, [setData, options]);

  // One chip per distinct item with its copy count.
  const itemCounts = useMemo(() => {
    const m = new Map<string, number>();
    items.forEach((i) => m.set(i, (m.get(i) ?? 0) + 1));
    return [...m.entries()];
  }, [items.join(",")]); // eslint-disable-line react-hooks/exhaustive-deps

  const addItem = (id: string) => items.length < MAX_ITEMS && setList("hi", [...items, id]);
  const removeItem = (id: string) => {
    const i = items.indexOf(id);
    if (i >= 0) setList("hi", items.filter((_, j) => j !== i));
  };

  const clear = () =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("hu");
        next.delete("hi");
        return next;
      },
      { replace: true },
    );

  const itemChips = (ids: string[]) =>
    itemCounts
      .filter(([id]) => ids.includes(id))
      .map(([id, n]) => (
        <span key={id} className="chip">
          <GameIcon kind="items" id={id} size={22} fallbackSrc={names.icon(id)} fallbackName={names.name(id)} />
          {names.name(id)}
          <button type="button" className="chip-step" title="One fewer" onClick={() => removeItem(id)}>
            −
          </button>
          <strong>{n}</strong>
          <button type="button" className="chip-step" title="One more" onClick={() => addItem(id)}>
            +
          </button>
        </span>
      ));

  return (
    <div className="panel advisor">
      <h2>What can I build?</h2>
      <p className="muted">
        Add the units you have and what is in your inventory: items (whole or still components), emblems and
        artifacts. Units alone show their best builds; items alone show which units they fit.
      </p>

      <div className="advisor-pickers">
        <Picker
          exclude={units}
          placeholder="Add unit…"
          kind="champions"
          options={units.length < MAX_UNITS ? (options?.units ?? []) : []}
          names={names}
          onPick={(id) => setList("hu", [...units, id])}
        />
        {groups.map((g) => (
          <Picker
            key={g.key}
            placeholder={g.placeholder}
            kind="items"
            options={items.length < MAX_ITEMS ? g.options : []}
            names={names}
            onPick={addItem}
          />
        ))}
        {(units.length > 0 || items.length > 0) && (
          <button type="button" onClick={clear}>
            Clear
          </button>
        )}
      </div>

      {(units.length > 0 || items.length > 0) && (
        <div className="advisor-chosen">
          {units.map((u) => (
            <button
              type="button"
              key={u}
              className="chip"
              title={`Remove ${names.name(u)}`}
              onClick={() =>
                setList(
                  "hu",
                  units.filter((x) => x !== u),
                )
              }
            >
              <GameIcon kind="champions" id={u} size={22} fallbackSrc={names.icon(u)} fallbackName={names.name(u)} />
              {names.name(u)} ×
            </button>
          ))}
          {itemChips(itemCounts.map(([id]) => id))}
        </div>
      )}

      {error && <div className="error-box">{error}</div>}
      {units.length === 0 && items.length === 0 && (
        <p className="muted">Pick a unit, an item, an emblem or an artifact to get suggestions.</p>
      )}
      {result && (units.length > 0 || items.length > 0) && (
        <div className={loading ? "advisor-results stale" : "advisor-results"}>
          {units.length > 0 ? (
            <>
              <Plan result={result} names={names} hasItems={items.length > 0} />
              <Alternatives result={result} names={names} />
              <Comps comps={result.comps} names={names} ownedUnits={units} />
            </>
          ) : (
            <Candidates result={result} names={names} />
          )}
        </div>
      )}
    </div>
  );
}

/** One build: each item as ready (held), combined from components, or missing. */
function BuildItems({ build, names }: { build: SuggestBuild; names: Names }) {
  const made = new Map<string, SuggestStep[]>();
  build.steps.forEach((s) => made.set(s.item, [...(made.get(s.item) ?? []), s]));
  // Walk the build's items, consuming one step per item.
  const cells = build.items.map((id) => {
    const step = made.get(id)?.shift();
    return { id, step };
  });
  return (
    <span className="build-items">
      {cells.map(({ id, step }, i) => {
        const state = !step ? "missing" : step.from ? "combine" : "held";
        return (
          <span
            key={i}
            className={`build-item ${state}`}
            title={
              state === "missing"
                ? `${names.name(id)} — missing`
                : state === "combine"
                  ? `${names.name(id)} — combine ${step!.from!.map(names.name).join(" + ")}`
                  : `${names.name(id)} — you have it`
            }
          >
            <GameIcon kind="items" id={id} size={26} fallbackSrc={names.icon(id)} fallbackName={names.name(id)} />
            {state === "combine" && (
              <span className="muted build-from">
                {step!.from!.map((c, j) => (
                  <GameIcon
                    key={j}
                    kind="items"
                    id={c}
                    size={14}
                    fallbackSrc={names.icon(c)}
                    fallbackName={names.name(c)}
                  />
                ))}
              </span>
            )}
          </span>
        );
      })}
    </span>
  );
}

function BuildStatus({ build, aim }: { build: SuggestBuild; aim?: boolean }) {
  if (aim)
    return (
      <span className="muted" title="Nothing you hold builds toward this yet">
        to aim for
      </span>
    );
  if (build.ready) return <span className="good">ready</span>;
  return <span className="muted">missing {build.missing.length}</span>;
}

function BuildStats({ build }: { build: SuggestBuild }) {
  return (
    <span className="build-stats">
      <span className={placementTone(build)} title="Average placement">
        {avg(build.avgPlacement)}
      </span>{" "}
      <span className="muted" title="Boards that ran exactly this build on this unit">
        · {build.boards} boards · top 4 {pct(build.top4Rate)}
      </span>
    </span>
  );
}

function UnitHead({ id, names }: { id: string; names: Names }) {
  return (
    <span className="game-label">
      <GameIcon kind="champions" id={id} size={26} fallbackSrc={names.icon(id)} fallbackName={names.name(id)} />
      <strong>{names.name(id)}</strong>
    </span>
  );
}

function Plan({ result, names, hasItems }: { result: SuggestResult; names: Names; hasItems: boolean }) {
  return (
    <div className="advisor-section">
      <h3>{hasItems ? "Suggested plan" : "Best builds"}</h3>
      {result.plan.length === 0 ? (
        <p className="muted">No builds have been seen for these units in this queue and level range.</p>
      ) : (
        <div className="plan-list">
          {result.plan.map((p) => (
            <div className="plan-row" key={p.unit}>
              <UnitHead id={p.unit} names={names} />
              <BuildItems build={p.build} names={names} />
              <BuildStatus build={p.build} aim={p.aim} />
              <BuildStats build={p.build} />
            </div>
          ))}
        </div>
      )}
      {result.leftover.length > 0 && (
        <p className="muted leftover">
          Not used:{" "}
          {result.leftover.map((id, i) => (
            <GameIcon
              key={i}
              kind="items"
              id={id}
              size={18}
              fallbackSrc={names.icon(id)}
              fallbackName={names.name(id)}
            />
          ))}
        </p>
      )}
      <p className="muted legend">
        <span className="build-item held">held</span> you have it · <span className="build-item combine">combine</span>{" "}
        make it from the two small icons · <span className="build-item missing">missing</span> not possible yet
      </p>
    </div>
  );
}

/** Items only: the units whose builds those items fit best. */
function Candidates({ result, names }: { result: SuggestResult; names: Names }) {
  return (
    <div className="advisor-section">
      <h3>Units that fit your items</h3>
      {result.candidates.length === 0 ? (
        <p className="muted">No unit has a build seen with these items in this queue and level range.</p>
      ) : (
        <div className="plan-list">
          {result.candidates.map((c) => (
            <div className="plan-row" key={c.unit}>
              <UnitHead id={c.unit} names={names} />
              <BuildItems build={c.build} names={names} />
              <BuildStatus build={c.build} />
              <BuildStats build={c.build} />
            </div>
          ))}
        </div>
      )}
      <p className="muted">Add a unit above to see its other builds and the comps that fit.</p>
    </div>
  );
}

function Alternatives({ result, names }: { result: SuggestResult; names: Names }) {
  const withOptions = result.units.filter((u) => u.options.length > 0);
  if (withOptions.length === 0) return null;
  return (
    <div className="advisor-section">
      <h3>Other builds per unit</h3>
      <p className="muted">Each judged against your whole inventory, so one item can appear for several units.</p>
      {withOptions.map((u) => (
        <details key={u.id} className="advisor-unit">
          <summary>
            <UnitHead id={u.id} names={names} />
          </summary>
          {u.options.map((o) => (
            <div className="plan-row" key={o.items.join(",")}>
              <BuildItems build={o} names={names} />
              <BuildStatus build={o} />
              <BuildStats build={o} />
            </div>
          ))}
        </details>
      ))}
    </div>
  );
}

function Comps({ comps, names, ownedUnits }: { comps: SuggestComp[]; names: Names; ownedUnits: string[] }) {
  if (comps.length === 0) return null;
  const owned = new Set(ownedUnits);
  return (
    <div className="advisor-section">
      <h3>Comps that fit your units</h3>
      <div className="advisor-comps">
        {comps.map((c, i) => (
          <div className="advisor-comp" key={i}>
            <div className="advisor-comp-head">
              <CompName comp={c.comp} names={names} />
              <strong>
                {c.have.length} of {c.comp.board.length} units
              </strong>
              <span className={placementTone(c.comp)}>{avg(c.comp.avgPlacement)} avg</span>
              <span className="muted">
                {c.comp.boards} boards · top 4 {pct(c.comp.top4Rate)}
              </span>
            </div>
            <div className="advisor-comp-units">
              {c.comp.board.map((u) => (
                <span
                  key={u.id}
                  className={owned.has(u.id) ? "adv-unit have" : "adv-unit need"}
                  title={`${names.name(u.id)}${owned.has(u.id) ? " — you have it" : " — still needed"}`}
                >
                  <GameIcon
                    kind="champions"
                    id={u.id}
                    size={30}
                    fallbackSrc={names.icon(u.id)}
                    fallbackName={names.name(u.id)}
                  />
                </span>
              ))}
            </div>
            {c.fits.length > 0 && (
              <ul className="advisor-fits">
                {c.fits.map((f) => (
                  <li key={f.unit}>
                    {names.name(f.unit)}:{" "}
                    <span className={f.missing.length === 0 ? "good" : "muted"}>
                      {f.steps.length} of {f.items.length} items makeable
                    </span>
                    {f.missing.length > 0 && (
                      <span className="muted"> (missing {f.missing.map((m) => names.name(m)).join(", ")})</span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
