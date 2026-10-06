import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ApiError, SetData, SetItem, getSetData } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Picker, PickerOption } from "../components/Picker";
import { ItemTable, UnitGrid } from "../components/PickTables";
import { Names, TIER_STYLE, buildNames } from "../components/stats";
import { CURRENT_TFT_SET } from "../config";
import { AUGMENT_SLOTS, augmentTier, availableAt } from "../planner/augmentStages";
import {
  Board,
  COLS,
  MAX_ITEMS,
  MAX_LEVEL,
  MAX_TITLE,
  ROWS,
  addItem,
  addUnit,
  augmentsByTier,
  boardCost,
  cleanTitle,
  computeTraits,
  decodeBoard,
  emptyBoard,
  encodeBoard,
  holdableItems,
  moveUnit,
  removeItem,
  removeUnit,
  sanitize,
  setAugment,
  setStar,
} from "../planner/board";

// Board planner. The whole board lives in the page URL (see planner/board.ts),
// so copying the address shares it: no account or server storage needed.

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

  useEffect(() => {
    setData(null);
    setError(null);
    getSetData(set)
      .then(setData)
      .catch((e: unknown) => setError(e instanceof ApiError ? e.message : "couldn't load set data"));
  }, [set]);

  // Until set data arrives the board is shown as decoded; afterwards, with
  // anything the set doesn't know removed.
  const { board, dropped } = useMemo(
    () => (data ? sanitize(raw, data) : { board: raw, dropped: { units: 0, items: 0, augments: 0 } }),
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
        return encodeBoard(edit(data ? sanitize(current, data).board : current));
      },
      { replace: true },
    );

  const traits = useMemo(() => (data ? computeTraits(board, data) : []), [board, data]);
  const cost = data ? boardCost(board, data) : 0;
  const unitAt = (pos: number) => board.units.find((u) => u.pos === pos);
  const selectedUnit = selected === null ? undefined : unitAt(selected);
  const unitInfo = (id: string) => data?.units.find((u) => u.apiName === id);

  const allItems = useMemo(() => (data ? holdableItems(data) : []), [data]);
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

  function clickCell(pos: number) {
    const here = unitAt(pos);
    if (selected !== null && selected !== pos && unitAt(selected)) {
      // Move (or swap) the selected unit here; the selection follows it.
      update((b) => moveUnit(b, selected, pos));
      setSelected(pos);
    } else {
      setSelected(here && selected !== pos ? pos : null);
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

  const over = board.units.length > board.level;

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
            Units {board.units.length}/{board.level}
          </span>
          <span>Cost {cost}g</span>
        </div>
        {error && <div className="error-box">{error}</div>}
        {(dropped.units > 0 || dropped.items > 0 || dropped.augments > 0) && (
          <p className="warning-box">
            This link had {dropped.units} units, {dropped.items} items and {dropped.augments} augments that aren&apos;t
            in Set {set}; they were skipped.
          </p>
        )}
      </div>

      <div className="planner-layout">
        <div className="planner-main">
        <div className="panel planner-boardpanel">
          <div className="planner-board" role="grid" aria-label="Board">
            {Array.from({ length: ROWS * COLS }, (_, pos) => {
              const u = unitAt(pos);
              const row = Math.floor(pos / COLS);
              return (
                <button
                  type="button"
                  key={pos}
                  className={`planner-cell${row % 2 ? " odd" : ""}${u ? " filled" : ""}${selected === pos ? " selected" : ""}`}
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
                  onClick={() => clickCell(pos)}
                >
                  {u && (
                    <>
                      <span className="planner-stars" aria-hidden>
                        {"★".repeat(u.star)}
                      </span>
                      <GameIcon
                        kind="champions"
                        id={u.id}
                        size={44}
                        fallbackSrc={names.icon(u.id)}
                        fallbackName={names.name(u.id)}
                        className={unitInfo(u.id) ? `cost-${unitInfo(u.id)!.cost}` : ""}
                      />
                      <span className="planner-cell-items">
                        {u.items.map((it, i) => (
                          <GameIcon
                            key={i}
                            kind="items"
                            id={it}
                            size={14}
                            fallbackSrc={names.icon(it)}
                            fallbackName={names.name(it)}
                          />
                        ))}
                      </span>
                    </>
                  )}
                </button>
              );
            })}
          </div>
          <p className="muted planner-hint">
            Drag a unit, or select it and click another cell, to move or swap. Front line is at the top.
          </p>
        </div>

        <div className="panel planner-units">
          <h3>Units</h3>
          {data ? (
            <UnitGrid
              units={data.units}
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
  update,
  onRemoved,
}: {
  board: Board;
  pos: number;
  names: Names;
  data: SetData | null;
  items: SetItem[];
  update: (edit: (b: Board) => Board) => void;
  onRemoved: () => void;
}) {
  const unit = board.units.find((u) => u.pos === pos);
  if (!unit) return null;
  const info = data?.units.find((u) => u.apiName === unit.id);
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
      <ItemTable
        items={items}
        names={names}
        disabled={unit.items.length >= MAX_ITEMS}
        onPick={(it) => update((b) => addItem(b, pos, it))}
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
