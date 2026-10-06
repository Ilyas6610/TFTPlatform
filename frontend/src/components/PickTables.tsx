import { ReactNode, useMemo, useState } from "react";
import { SetItem, SetUnit } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Names } from "./stats";

// Pickers for the board planner: a compact grid of unit icons, and the item
// grid (components along both axes, the item each pair makes in the cell,
// everything that can't be built in sections below). Both show details in a
// card on hover or keyboard focus; click to choose.

const STAT_LABELS: [string, string][] = [
  ["hp", "HP"],
  ["damage", "AD"],
  ["attackSpeed", "AS"],
  ["armor", "Armor"],
  ["magicResist", "MR"],
  ["initialMana", "Start mana"],
  ["mana", "Mana"],
  ["range", "Range"],
];

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2).replace(/0+$/, ""));

/** Game text without the "[[Label]]" markers for values the game computes. */
const plain = (s: string) => s.replace(/\[\[([^\]]*)\]\]/g, "$1").replace(/\s+/g, " ").trim();

const clip = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + "…" : s);

// ---- Hover card ------------------------------------------------------------

const TIP_WIDTH = 300;
const TIP_HEIGHT_GUESS = 230;

interface Tip {
  x: number;
  y: number;
  above: boolean;
  node: ReactNode;
}

/** Shows `node` in a card next to the element, flipping above it near the screen bottom. */
function useHoverTip() {
  const [tip, setTip] = useState<Tip | null>(null);
  const show = (el: HTMLElement, node: ReactNode) => {
    const r = el.getBoundingClientRect();
    const above = r.bottom + TIP_HEIGHT_GUESS > window.innerHeight && r.top > TIP_HEIGHT_GUESS;
    setTip({
      x: Math.max(8, Math.min(r.left + r.width / 2 - TIP_WIDTH / 2, window.innerWidth - TIP_WIDTH - 8)),
      y: above ? r.top - 8 : r.bottom + 8,
      above,
      node,
    });
  };
  const hide = () => setTip(null);
  const view = tip && (
    <div
      className={`unit-tip${tip.above ? " above" : ""}`}
      style={{ left: tip.x, top: tip.y, width: TIP_WIDTH }}
      role="tooltip"
    >
      {tip.node}
    </div>
  );
  return { show, hide, view };
}

// ---- Units ------------------------------------------------------------------

export function UnitGrid({
  units,
  names,
  onPick,
  disabled,
}: {
  units: SetUnit[];
  names: Names;
  onPick: (id: string) => void;
  /** Board full: icons stay visible but can't be picked. */
  disabled?: boolean;
}) {
  const { show, hide, view } = useHoverTip();
  const byCost = useMemo(() => {
    const m = new Map<number, SetUnit[]>();
    for (const u of [...units].sort((a, b) => a.name.localeCompare(b.name))) m.set(u.cost, [...(m.get(u.cost) ?? []), u]);
    return [...m.entries()].sort((a, b) => a[0] - b[0]);
  }, [units]);

  return (
    <div className="unit-pick" onMouseLeave={hide}>
      {byCost.map(([cost, list]) => (
        <div className="unit-pick-row" key={cost}>
          <span className="unit-pick-cost muted">{cost}g</span>
          <div className="unit-pick-icons">
            {list.map((u) => (
              <button
                type="button"
                key={u.apiName}
                className="unit-pick-btn"
                aria-label={`Add ${u.name}`}
                disabled={disabled}
                onClick={() => onPick(u.apiName)}
                onMouseEnter={(e) => show(e.currentTarget, <UnitCard unit={u} names={names} />)}
                onFocus={(e) => show(e.currentTarget, <UnitCard unit={u} names={names} />)}
                onBlur={hide}
              >
                <GameIcon
                  kind="champions"
                  id={u.apiName}
                  size={34}
                  fallbackSrc={u.icon}
                  fallbackName={u.name}
                  className={`cost-${u.cost}`}
                />
              </button>
            ))}
          </div>
        </div>
      ))}
      {view}
    </div>
  );
}

function UnitCard({ unit: u, names }: { unit: SetUnit; names: Names }) {
  const stats = STAT_LABELS.filter(([k]) => u.stats[k] !== undefined);
  const desc = plain(u.ability.desc);
  return (
    <>
      <div className="unit-tip-head">
        <GameIcon
          kind="champions"
          id={u.apiName}
          size={36}
          fallbackSrc={u.icon}
          fallbackName={u.name}
          className={`cost-${u.cost}`}
        />
        <div>
          <strong>{names.name(u.apiName)}</strong> <span className="muted">· {u.cost}g</span>
          <div className="muted">{u.traits.join(" · ")}</div>
        </div>
      </div>
      {stats.length > 0 && (
        <div className="unit-tip-stats">
          {stats.map(([k, label]) => (
            <span key={k}>
              <span className="muted">{label}</span> {fmt(u.stats[k])}
            </span>
          ))}
        </div>
      )}
      {u.ability.name && (
        <div className="unit-tip-ability">
          <strong>{u.ability.name}</strong>
          {desc && <p>{clip(desc, 260)}</p>}
        </div>
      )}
    </>
  );
}

// ---- Items ------------------------------------------------------------------

const KIND_NAMES: Record<string, string> = {
  completed: "Completed item",
  emblem: "Emblem",
  artifact: "Artifact",
  radiant: "Radiant item",
  component: "Component",
};

/** Items that aren't built from two components, by section. */
const OTHER_SECTIONS: [string, (i: SetItem) => boolean][] = [
  ["Artifacts", (i) => i.kind === "artifact"],
  ["Radiant items", (i) => i.kind === "radiant"],
  ["Emblems (not built from components)", (i) => i.kind === "emblem"],
];

/**
 * Components along both axes; the cell where two meet holds the item they
 * make (one for each order, as in the in-game chart). Items with no
 * component recipe — artifacts, radiants, emblems from other sources — sit in
 * their own sections below.
 */
export function ItemGrid({
  items,
  components,
  names,
  onPick,
  disabled,
  blocked,
}: {
  /** Everything a unit can hold. */
  items: SetItem[];
  components: SetItem[];
  names: Names;
  onPick: (id: string) => void;
  /** The unit already holds three items. */
  disabled?: boolean;
  /** Why an item can't go on this unit (an emblem for a trait it has), or null. */
  blocked?: (item: SetItem) => string | null;
}) {
  const { show, hide, view } = useHoverTip();
  const { axis, made, others } = useMemo(() => {
    const axis = [...components].sort((a, b) => a.name.localeCompare(b.name));
    const ids = new Set(axis.map((c) => c.apiName));
    const made = new Map<string, SetItem>(); // "a|b" -> item, both orders
    const built = new Set<string>();
    for (const i of items) {
      const [a, b] = i.composition ?? [];
      if (i.composition?.length === 2 && ids.has(a) && ids.has(b)) {
        made.set(`${a}|${b}`, i);
        made.set(`${b}|${a}`, i);
        built.add(i.apiName);
      }
    }
    const rest = items.filter((i) => !built.has(i.apiName));
    const others = OTHER_SECTIONS.map(([title, pred]) => ({
      title,
      list: rest.filter(pred).sort((a, b) => a.name.localeCompare(b.name)),
    })).filter((s) => s.list.length > 0);
    return { axis, made, others };
  }, [items, components]);

  const btn = (i: SetItem, size: number) => {
    const why = blocked?.(i) ?? null;
    const card = <ItemCard item={i} names={names} note={why} />;
    return (
      <button
        type="button"
        key={i.apiName}
        className={`item-pick-btn${why ? " blocked" : ""}`}
        aria-label={`Add ${i.name}`}
        aria-disabled={why ? true : undefined}
        disabled={disabled}
        onClick={() => !why && onPick(i.apiName)}
        onMouseEnter={(e) => show(e.currentTarget, card)}
        onFocus={(e) => show(e.currentTarget, card)}
        onBlur={hide}
      >
        <GameIcon kind="items" id={i.apiName} size={size} fallbackSrc={i.icon} fallbackName={i.name} />
      </button>
    );
  };
  const head = (c: SetItem) => (
    <span
      className="item-pick-head"
      tabIndex={0}
      aria-label={c.name}
      onMouseEnter={(e) => show(e.currentTarget, <ItemCard item={c} names={names} />)}
      onFocus={(e) => show(e.currentTarget, <ItemCard item={c} names={names} />)}
      onBlur={hide}
    >
      <GameIcon kind="items" id={c.apiName} size={26} fallbackSrc={c.icon} fallbackName={c.name} />
    </span>
  );

  return (
    <div className="item-pick" onMouseLeave={hide}>
      {axis.length > 0 && (
        <div className="item-pick-matrix" style={{ gridTemplateColumns: `repeat(${axis.length + 1}, minmax(0, 1fr))`, maxWidth: (axis.length + 1) * 40 }} role="grid">
          <span />
          {axis.map((c) => (
            <span key={c.apiName}>{head(c)}</span>
          ))}
          {axis.map((row) => (
            <ItemRow key={row.apiName}>
              <span>{head(row)}</span>
              {axis.map((col) => {
                const item = made.get(`${row.apiName}|${col.apiName}`);
                return <span key={col.apiName}>{item ? btn(item, 28) : <span className="item-pick-empty" />}</span>;
              })}
            </ItemRow>
          ))}
        </div>
      )}
      {others.map((s) => (
        <div className="item-pick-section" key={s.title}>
          <h4 className="muted">{s.title}</h4>
          <div className="item-pick-icons">{s.list.map((i) => btn(i, 28))}</div>
        </div>
      ))}
      {view}
    </div>
  );
}

/** Groups a row's cells without adding a box of its own (the matrix is one CSS grid). */
const ItemRow = ({ children }: { children: ReactNode }) => <>{children}</>;

function ItemCard({ item: i, names, note }: { item: SetItem; names: Names; note?: string | null }) {
  const desc = plain(i.desc ?? "");
  return (
    <>
      <div className="unit-tip-head">
        <GameIcon kind="items" id={i.apiName} size={36} fallbackSrc={i.icon} fallbackName={i.name} />
        <div>
          <strong>{i.name}</strong>
          <div className="muted">{KIND_NAMES[i.kind] ?? i.kind}</div>
        </div>
      </div>
      {i.composition && i.composition.length > 0 && (
        <div className="unit-tip-stats">
          {i.composition.map((c, j) => (
            <span key={j} className="item-tip-part">
              <GameIcon kind="items" id={c} size={18} fallbackSrc={names.icon(c)} fallbackName={names.name(c)} />
              {names.name(c)}
            </span>
          ))}
        </div>
      )}
      {note && <div className="item-tip-note">{note}</div>}
      {desc && (
        <div className="unit-tip-ability">
          <p>{clip(desc, 300)}</p>
        </div>
      )}
    </>
  );
}
