import { Suspense, lazy, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ApiError, PlannerCodes, SetData, SetItem, getPlannerCodes, getSetData } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Picker, PickerOption } from "../components/Picker";
import { ItemGrid, UnitGrid } from "../components/PickTables";
import { Names, TIER_STYLE, buildNames } from "../components/stats";
import { CURRENT_TFT_SET } from "../config";
import { decodeTeamCode, encodeTeamCode, teamCodeSet } from "../planner/teamCode";
import { AUGMENT_SLOTS, augmentTier, availableAt } from "../planner/augmentStages";
import {
  Board,
  COLS,
  FormIndex,
  TraitChoices,
  MAX_ITEMS,
  MAX_LEVEL,
  MAX_TITLE,
  ROWS,
  addItemChecked,
  canHoldItem,
  dropRedundantEmblems,
  droppedSummary,
  teamSizeBonus,
  addUnit,
  augmentsByTier,
  buildFormIndex,
  slotsUsed,
  buildTraitChoices,
  boardCost,
  cleanTitle,
  clickCell,
  computeTraits,
  decodeBoard,
  emptyBoard,
  encodeBoard,
  holdableItems,
  moveUnit,
  normalizeForms,
  removeItem,
  removeUnit,
  sanitize,
  setAugment,
  setForm,
  setStar,
  toggleExtraTrait,
} from "../planner/board";

// Board planner. The whole board lives in the page URL (see planner/board.ts),
// so copying the address shares it: no account or server storage needed.

// The augment estimate and its data are a sizeable chunk only this page uses: load it with the page, not with every page.
const AugmentImpact = lazy(() => import("../components/AugmentImpact").then((m) => ({ default: m.AugmentImpact })));

const AUGMENT_TIERS: Record<number, string> = { 1: "Silver", 2: "Gold", 3: "Prismatic" };
const tierName = (tier: number) => AUGMENT_TIERS[tier] ?? "";

export default function PlannerPage() {
  const [params, setParams] = useSearchParams();
  const raw = useMemo(() => decodeBoard(params, CURRENT_TFT_SET), [params]);
  const set = raw.set;

  const [data, setData] = useState<SetData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<number | null>(null);
  const [copied, setCopied] = useState(false);
  const [plannerCodes, setPlannerCodes] = useState<PlannerCodes | null>(null);

  useEffect(() => {
    setData(null);
    setError(null);
    getSetData(set)
      .then(setData)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "couldn't load set data"));
  }, [set]);

  // The in-game planner's unit numbers; without them (set not published, or
  // the source is down) the code section is simply left out.
  useEffect(() => {
    setPlannerCodes(null);
    getPlannerCodes(set)
      .then(setPlannerCodes)
      .catch(() => setPlannerCodes(null));
  }, [set]);

  // Until set data arrives the board is shown as decoded; afterwards, with
  // anything the set doesn't know removed.
  const { board, dropped } = useMemo(
    () => (data ? sanitize(raw, data) : { board: raw, dropped: { units: 0, items: 0, augments: 0, traits: 0 } }),
    [raw, data],
  );
  const names = useMemo(() => buildNames(data), [data]);
  // Edits are applied to the URL's current board, not to the board this
  // render saw: two quick edits (clicking several units) would otherwise
  // each start from the same stale board and the second would undo the first.
  const update = (edit: (b: Board) => Board) =>
    setParams(
      (prev) => {
        const current = decodeBoard(prev, CURRENT_TFT_SET);
        const edited = edit(data ? sanitize(current, data).board : current);
        // All copies of a unit with forms (Lux) share one trait.
        if (!data) return encodeBoard(edited);
        // An emblem whose trait the unit now has some other way (a Lux's trait, an evolved one) goes.
        return encodeBoard(dropRedundantEmblems(normalizeForms(edited, buildFormIndex(data.units)), data).board);
      },
      { replace: true },
    );

  const traits = useMemo(() => (data ? computeTraits(board, data) : []), [board, data]);
  const cost = data ? boardCost(board, data) : 0;
  const unitAt = (pos: number) => board.units.find((u) => u.pos === pos);
  const selectedUnit = selected === null ? undefined : unitAt(selected);
  const unitInfo = (id: string) => data?.units.find((u) => u.apiName === id);

  const allItems = useMemo(() => (data ? holdableItems(data) : []), [data]);
  const traitChoices = useMemo(() => (data ? buildTraitChoices(data) : new Map<string, TraitChoices>()), [data]);
  const formIndex = useMemo(() => buildFormIndex(data?.units ?? []), [data]);
  // Units with a chosen trait show once (their base); the trait is a menu on the placed unit.
  const pickableUnits = useMemo(() => (data?.units ?? []).filter((u) => !formIndex.baseOf.has(u.apiName)), [data, formIndex]);
  const components = useMemo(() => (data?.items ?? []).filter((i) => i.kind === "component"), [data]);
  // Augments each slot can offer: tier order, not already chosen elsewhere.
  const augmentOptions = (slot: number): PickerOption[] =>
    data
      ? augmentsByTier(data)
          .filter((a) => availableAt(a, slot) && !board.augments.includes(a.apiName))
          .map((a) => ({
            id: a.apiName,
            boards: 0,
            label: tierName(augmentTier(a)) ? `${a.name} (${tierName(augmentTier(a))})` : a.name,
          }))
      : [];

  function onCellClick(pos: number) {
    const r = clickCell(board, selected, pos);
    setSelected(r.selected);
    if (r.move) {
      const { from, to } = r.move;
      update((b) => moveUnit(b, from, to));
    }
  }

  function share() {
    const url = window.location.href;
    const done = () => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    };
    navigator.clipboard?.writeText(url).then(done, () => window.prompt("Copy this link:", url));
    if (!navigator.clipboard) window.prompt("Copy this link:", url);
  }

  const size = data ? teamSizeBonus(board, data) : { bonus: 0, sources: [] as string[] };
  const slots = data ? slotsUsed(board, data) : { used: board.units.length, extra: [] };
  const maxUnits = board.level + size.bonus;
  const over = slots.used > maxUnits;

  return (
    <div className="planner">
      <div className="panel planner-head">
        <h2>Board planner</h2>
        <p className="muted">
          Place units, give them stars and items, pick augments, and share the board: the link holds everything.
        </p>
        <div className="planner-controls">
          <label className="muted">
            Name{" "}
            <input
              value={board.title}
              maxLength={MAX_TITLE}
              placeholder="My board"
              onChange={(e) => update((b) => ({ ...b, title: cleanTitle(e.target.value) }))}
            />
          </label>
          <label className="muted">
            Level{" "}
            <select value={board.level} onChange={(e) => update((b) => ({ ...b, level: Number(e.target.value) }))}>
              {Array.from({ length: MAX_LEVEL }, (_, i) => i + 1).map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <button type="button" onClick={share}>
            {copied ? "Link copied" : "Copy share link"}
          </button>
          {(board.units.length > 0 || board.augments.some(Boolean) || board.title) && (
            <button
              type="button"
              onClick={() => {
                update(() => emptyBoard(set));
                setSelected(null);
              }}
            >
              Clear board
            </button>
          )}
        </div>
        <div className="planner-summary muted">
          <span className={over ? "bad" : undefined}>
            Units {slots.used}/{maxUnits}
          </span>
          {slots.extra.length > 0 && (
            <span>({[...new Set(slots.extra.map((e) => `${e.unit} takes ${e.slots} slots`))].join(", ")})</span>
          )}
          {size.bonus > 0 && (
            <span title={size.sources.join(", ")}>
              +{size.bonus} team size ({[...new Set(size.sources)].join(", ")})
            </span>
          )}
          <span>Cost {cost}g</span>
        </div>
        {error && <div className="error-box">{error}</div>}
        {(dropped.units > 0 || dropped.items > 0 || dropped.augments > 0 || dropped.traits > 0) && (
          <p className="warning-box">
            Skipped from this link: {droppedSummary(dropped)} (not in Set {set}, or not allowed there).
          </p>
        )}
      </div>

      <div className="planner-layout">
        <div className="planner-main">
        <div className="panel planner-boardpanel">
          <div className="planner-board-wrap">
            <div className="planner-board" role="grid" aria-label="Board">
              {Array.from({ length: ROWS * COLS }, (_, pos) => {
                const u = unitAt(pos);
                const row = Math.floor(pos / COLS);
                const col = pos % COLS;
                const cost = u ? unitInfo(u.id)?.cost : undefined;
                return (
                  // Hexes sit on a 15-column grid, two columns wide; odd rows start one column in.
                  <div
                    key={pos}
                    className={`planner-slot${cost ? ` cost-${cost}` : ""}${selected === pos ? " selected" : ""}`}
                    style={{ gridColumn: `${1 + col * 2 + (row % 2)} / span 2`, gridRow: row + 1 }}
                  >
                    <button
                      type="button"
                      className={`planner-cell${u ? " filled" : ""}${selected === pos ? " selected" : ""}`}
                      aria-label={u ? `${names.name(u.id)}, ${u.star} star` : "Empty cell"}
                      draggable={!!u}
                      onDragStart={(e) => {
                        e.dataTransfer.setData("text/plain", String(pos));
                        e.dataTransfer.effectAllowed = "move";
                      }}
                      onDragOver={(e) => e.preventDefault()}
                      onDrop={(e) => {
                        e.preventDefault();
                        const from = Number(e.dataTransfer.getData("text/plain"));
                        if (Number.isInteger(from)) {
                          update((b) => moveUnit(b, from, pos));
                          setSelected(pos);
                        }
                      }}
                      onClick={() => onCellClick(pos)}
                      onContextMenu={(e) => {
                        // Right-click removes the unit; on an empty hex the browser's own menu stays.
                        if (!u) return;
                        e.preventDefault();
                        update((b) => removeUnit(b, pos));
                        if (selected === pos) setSelected(null);
                      }}
                    >
                      <span className="hex-frame" />
                      <span className="hex-art">
                        {u && (
                          <GameIcon
                            kind="champions"
                            id={u.id}
                            size={96}
                            fallbackSrc={names.icon(u.id)}
                            fallbackName={names.name(u.id)}
                            className="planner-hex-img"
                          />
                        )}
                      </span>
                    </button>
                    {u && (
                      <>
                        <span className="planner-stars" aria-hidden>
                          {"★".repeat(u.star)}
                        </span>
                        {u.items.length > 0 && (
                          <span className="planner-cell-items">
                            {u.items.map((it, i) => (
                              // Left-click selects the unit, right-click takes the item off.
                              <span
                                key={i}
                                className="planner-item"
                                title={`${names.name(it)} (right-click to remove)`}
                                onClick={() => onCellClick(pos)}
                                onContextMenu={(e) => {
                                  e.preventDefault();
                                  update((b) => removeItem(b, pos, i));
                                }}
                              >
                                <GameIcon
                                  kind="items"
                                  id={it}
                                  size={24}
                                  fallbackSrc={names.icon(it)}
                                  fallbackName={names.name(it)}
                                />
                              </span>
                            ))}
                          </span>
                        )}
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
          <p className="muted planner-hint">
            Click a unit to select it, then an empty cell to move it. Drag a unit onto another to swap. Right-click a unit, or one of its items, to remove it. Front line is at the top.
          </p>
        </div>

        {plannerCodes && (
          <TeamCodePanel
            board={board}
            codes={plannerCodes.codes}
            baseOf={formIndex.baseOf}
            names={names}
            onLoad={(units) => update((b) => units.reduce<Board>((acc, id) => addUnit(acc, id), { ...b, units: [] }))}
          />
        )}

        <div className="panel planner-units">
          <h3>Units</h3>
          {data ? (
            <UnitGrid
              units={pickableUnits}
              names={names}
              disabled={board.units.length >= ROWS * COLS}
              onPick={(id) => update((b) => addUnit(b, id))}
            />
          ) : (
            <p className="muted">Loading units…</p>
          )}
        </div>
        </div>

        <div className="planner-side">
          <div className="panel">
            {selectedUnit ? (
              <UnitEditor
                key={selectedUnit.pos}
                board={board}
                pos={selectedUnit.pos}
                names={names}
                data={data}
                items={allItems}
                components={components}
                formIndex={formIndex}
                traitChoices={traitChoices}
                update={update}
                onRemoved={() => setSelected(null)}
              />
            ) : (
              <p className="muted">Select a unit on the board to set its stars and items.</p>
            )}
          </div>

          <div className="panel planner-augments-panel">
            <h3>Augments</h3>
            <div className="planner-augments">
              {AUGMENT_SLOTS.map((slot, i) => {
                const id = board.augments[i];
                const aug = id ? data?.augments.find((a) => a.apiName === id) : undefined;
                return (
                  <div className="planner-augment" key={slot.stage}>
                    <div className="planner-augment-label">
                      <strong>{slot.label}</strong> <span className="muted">· stage {slot.stage}</span>
                    </div>
                    {id ? (
                      <div className="planner-augment-chosen">
                        <GameIcon
                          kind="augments"
                          id={id}
                          size={32}
                          fallbackSrc={names.icon(id)}
                          fallbackName={names.name(id)}
                        />
                        <span title={aug?.desc}>
                          <strong>{names.name(id)}</strong>
                          {aug && tierName(augmentTier(aug)) && <span className="muted"> · {tierName(augmentTier(aug))}</span>}
                        </span>
                        <button
                          type="button"
                          className="remove"
                          title="Remove"
                          onClick={() => update((b) => setAugment(b, i, null))}
                        >
                          ×
                        </button>
                      </div>
                    ) : (
                      <Picker
                        placeholder={`Pick for ${slot.stage}…`}
                        kind="augments"
                        options={augmentOptions(i)}
                        names={names}
                        onPick={(a) => update((b) => setAugment(b, i, a))}
                      />
                    )}
                  </div>
                );
              })}
            </div>
          </div>

          {data && (
            <Suspense fallback={<div className="panel augment-impact muted">Loading the augment estimate…</div>}>
              <AugmentImpact board={board} data={data} names={names} onPick={(slot, a) => update((b) => setAugment(b, slot, a))} />
            </Suspense>
          )}

          <div className="panel">
            <h3>Traits</h3>
            {traits.length === 0 ? (
              <p className="muted">Traits appear as you add units.</p>
            ) : (
              <ul className="planner-traits">
                {traits.map((t) => (
                  <li key={t.name} className={t.tier < 0 ? "inactive" : undefined} title={t.trait?.desc}>
                    <span className={`trait-badge trait-style-${TIER_STYLE[t.style] ?? 1}`}>{t.count}</span>
                    {t.apiName && (
                      <GameIcon
                        kind="traits"
                        id={t.apiName}
                        size={20}
                        fallbackSrc={names.icon(t.apiName)}
                        fallbackName={t.name}
                      />
                    )}
                    <span>{t.name}</span>
                    {t.next !== undefined && <span className="muted"> next at {t.next}</span>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function UnitEditor({
  board,
  pos,
  names,
  data,
  items,
  components,
  formIndex,
  traitChoices,
  update,
  onRemoved,
}: {
  board: Board;
  pos: number;
  names: Names;
  data: SetData | null;
  items: SetItem[];
  components: SetItem[];
  formIndex: FormIndex;
  traitChoices: Map<string, TraitChoices>;
  update: (edit: (b: Board) => Board) => void;
  onRemoved: () => void;
}) {
  const unit = board.units.find((u) => u.pos === pos);
  if (!unit) return null;
  const info = data?.units.find((u) => u.apiName === unit.id);
  const baseId = formIndex.baseOf.get(unit.id) ?? unit.id;
  const forms = formIndex.forms.get(baseId) ?? [];
  const evolve = traitChoices.get(unit.id);
  return (
    <div className="planner-editor">
      <h3 className="game-label">
        <GameIcon kind="champions" id={unit.id} size={32} fallbackSrc={names.icon(unit.id)} fallbackName={names.name(unit.id)} />
        {names.name(unit.id)}
        {info && <span className="muted"> · {info.cost}g</span>}
      </h3>
      {info && <p className="muted">{info.traits.join(" · ")}</p>}
      <div className="planner-stars-pick" role="group" aria-label="Star level">
        {([1, 2, 3] as const).map((s) => (
          <button
            type="button"
            key={s}
            className={unit.star === s ? "active" : undefined}
            aria-pressed={unit.star === s}
            onClick={() => update((b) => setStar(b, pos, s))}
          >
            {"★".repeat(s)}
          </button>
        ))}
      </div>
      {forms.length > 0 && (
        <label className="planner-form">
          <span className="muted">Trait</span>
          <select value={unit.id} aria-label="Trait" onChange={(e) => update((b) => setForm(b, pos, e.target.value, formIndex))}>
            <option value={baseId}>None chosen</option>
            {forms.map((f) => (
              <option key={f.id} value={f.id}>
                {f.trait}
              </option>
            ))}
          </select>
          <span className="muted">counts twice; all {names.name(baseId)} share it</span>
        </label>
      )}
      {evolve && (
        <fieldset className="planner-evolve">
          <legend className="muted">
            Evolved traits ({unit.extra.length}/{evolve.max})
          </legend>
          {evolve.traits.map((t) => {
            const on = unit.extra.includes(t.id);
            return (
              <label key={t.id} className={on ? "on" : undefined}>
                <input
                  type="checkbox"
                  checked={on}
                  disabled={!on && unit.extra.length >= evolve.max}
                  onChange={() => update((b) => toggleExtraTrait(b, pos, t.id, evolve))}
                />
                <GameIcon kind="traits" id={t.id} size={18} fallbackSrc={names.icon(t.id)} fallbackName={t.name} />
                {t.name}
              </label>
            );
          })}
        </fieldset>
      )}
      <div className="planner-items">
        {unit.items.map((it, i) => (
          <button
            type="button"
            key={i}
            className="chip"
            title={`Remove ${names.name(it)}`}
            onClick={() => update((b) => removeItem(b, pos, i))}
          >
            <GameIcon kind="items" id={it} size={22} fallbackSrc={names.icon(it)} fallbackName={names.name(it)} />
            {names.name(it)} ×
          </button>
        ))}
      </div>
      <ItemGrid
        items={items}
        components={components}
        names={names}
        disabled={unit.items.length >= MAX_ITEMS}
        onPick={(it) => update((b) => (data ? addItemChecked(b, pos, it, data) : b))}
        blocked={(i) => {
          if (!data || unit.items.length >= MAX_ITEMS) return null;
          const r = canHoldItem(board, pos, i.apiName, data);
          return r.ok ? null : r.reason;
        }}
      />
      <button
        type="button"
        onClick={() => {
          update((b) => removeUnit(b, pos));
          onRemoved();
        }}
      >
        Remove unit
      </button>
    </div>
  );
}

/** The in-game Team Planner code for the board's units, and loading units from one. */
function TeamCodePanel({
  board,
  codes,
  baseOf,
  names,
  onLoad,
}: {
  board: Board;
  codes: Record<string, number>;
  baseOf: Map<string, string>;
  names: Names;
  onLoad: (units: string[]) => void;
}) {
  const [pasted, setPasted] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  // Front line first, so if there are more than 10 units the back line drops.
  const ordered = [...board.units].sort((a, b) => a.pos - b.pos).map((u) => baseOf.get(u.id) ?? u.id); // the planner only knows a unit's base
  const team = encodeTeamCode(ordered, codes, board.set);

  function copy() {
    const done = () => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    };
    navigator.clipboard?.writeText(team.code).then(done, () => window.prompt("Copy this code:", team.code));
    if (!navigator.clipboard) window.prompt("Copy this code:", team.code);
  }

  function load() {
    // The set first: another set's unit numbers mean other units here.
    const set = teamCodeSet(pasted);
    if (set !== null && set !== board.set) return setMessage(`That code is for Set ${set}, this board is Set ${board.set}.`);
    const r = decodeTeamCode(pasted, codes);
    if (!r.ok) return setMessage(r.error);
    onLoad(r.units);
    setMessage(r.units.length === 0 ? "That code is empty." : `Loaded ${r.units.length} units. Their positions are yours to set.`);
    setPasted("");
  }

  const nameList = (ids: string[]) => ids.map((u) => names.name(u)).join(", ");
  return (
    <div className="panel planner-code">
      <h3>In-game Team Planner</h3>
      <p className="muted">
        Paste this code into the game&apos;s Team Planner. It holds up to 10 units only: positions, stars, items and
        augments stay here (use the share link for those).
      </p>
      <div className="planner-code-row">
        <input className="planner-code-box" readOnly value={team.code} aria-label="Team Planner code" onFocus={(e) => e.target.select()} />
        <button type="button" onClick={copy} disabled={team.included.length === 0}>
          {copied ? "Copied" : "Copy code"}
        </button>
      </div>
      {team.unknown.length > 0 && (
        <p className="muted">Not in the game&apos;s planner, left out: {nameList(team.unknown)}.</p>
      )}
      {team.overflow.length > 0 && (
        <p className="warning-box">The planner holds 10 units; left out: {nameList(team.overflow)}.</p>
      )}
      <div className="planner-code-row">
        <input
          value={pasted}
          placeholder="Paste a code to load its units…"
          aria-label="Team Planner code to load"
          onChange={(e) => setPasted(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && load()}
        />
        <button type="button" onClick={load} disabled={!pasted.trim()}>
          Load units
        </button>
      </div>
      {message && <p className="muted">{message}</p>}
    </div>
  );
}
