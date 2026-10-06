import { useMemo, useState } from "react";
import { SetItem, SetUnit } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Names } from "./stats";

// Pickers for the board planner: a compact grid of unit icons (details on
// hover) and a table of items. Click to choose.

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

/** Ability text without the "[[Label]]" markers for values the game computes. */
const plain = (s: string) => s.replace(/\[\[([^\]]*)\]\]/g, "$1").replace(/\s+/g, " ").trim();

interface Hover {
  unit: SetUnit;
  x: number;
  y: number;
  above: boolean;
}

const TIP_WIDTH = 300;

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
  const [hover, setHover] = useState<Hover | null>(null);
  const byCost = useMemo(() => {
    const m = new Map<number, SetUnit[]>();
    for (const u of [...units].sort((a, b) => a.name.localeCompare(b.name))) m.set(u.cost, [...(m.get(u.cost) ?? []), u]);
    return [...m.entries()].sort((a, b) => a[0] - b[0]);
  }, [units]);

  function show(u: SetUnit, el: HTMLElement) {
    const r = el.getBoundingClientRect();
    const above = r.bottom + 230 > window.innerHeight && r.top > 230;
    setHover({
      unit: u,
      x: Math.max(8, Math.min(r.left + r.width / 2 - TIP_WIDTH / 2, window.innerWidth - TIP_WIDTH - 8)),
      y: above ? r.top - 8 : r.bottom + 8,
      above,
    });
  }

  return (
    <div className="unit-pick" onMouseLeave={() => setHover(null)}>
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
                onMouseEnter={(e) => show(u, e.currentTarget)}
                onFocus={(e) => show(u, e.currentTarget)}
                onBlur={() => setHover(null)}
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
      {hover && <UnitTip hover={hover} names={names} />}
    </div>
  );
}

function UnitTip({ hover, names }: { hover: Hover; names: Names }) {
  const { unit: u } = hover;
  const stats = STAT_LABELS.filter(([k]) => u.stats[k] !== undefined);
  const desc = plain(u.ability.desc);
  return (
    <div
      className={`unit-tip${hover.above ? " above" : ""}`}
      style={{ left: hover.x, top: hover.y, width: TIP_WIDTH }}
      role="tooltip"
    >
      <div className="unit-tip-head">
        <GameIcon kind="champions" id={u.apiName} size={36} fallbackSrc={u.icon} fallbackName={u.name} className={`cost-${u.cost}`} />
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
          {desc && <p>{desc.length > 260 ? desc.slice(0, 257) + "…" : desc}</p>}
        </div>
      )}
    </div>
  );
}

const KIND_LABELS: [string, string][] = [
  ["completed", "Completed"],
  ["emblem", "Emblems"],
  ["artifact", "Artifacts"],
  ["radiant", "Radiant"],
];

export function ItemTable({
  items,
  names,
  onPick,
  disabled,
}: {
  items: SetItem[];
  names: Names;
  onPick: (id: string) => void;
  /** The unit already holds three items. */
  disabled?: boolean;
}) {
  const kinds = useMemo(() => KIND_LABELS.filter(([k]) => items.some((i) => i.kind === k)), [items]);
  const [kind, setKind] = useState(kinds[0]?.[0] ?? "completed");
  const [query, setQuery] = useState("");
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    // Searching looks across every kind; otherwise the chosen kind's items.
    return items
      .filter((i) => (q ? matches(q, i.name) : i.kind === kind))
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [items, kind, query]);

  return (
    <div className="pick-table">
      <div className="pick-table-controls">
        <input
          value={query}
          placeholder="Search items…"
          aria-label="Search items"
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="pick-filters" role="group" aria-label="Item kind">
          {kinds.map(([k, label]) => (
            <FilterButton key={k} active={!query && kind === k} onClick={() => (setQuery(""), setKind(k))}>
              {label}
            </FilterButton>
          ))}
        </div>
      </div>
      <div className="pick-table-wrap">
        <table className="pick-grid">
          <thead>
            <tr>
              <th>Item</th>
              <th>Made from</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((i) => (
              <tr
                key={i.apiName}
                className={disabled ? undefined : "clickable"}
                title={disabled ? "This unit holds 3 items" : `${i.name}: ${i.desc}`}
                onClick={() => !disabled && onPick(i.apiName)}
              >
                <td>
                  <span className="pick-name">
                    <GameIcon kind="items" id={i.apiName} size={24} fallbackSrc={i.icon} fallbackName={i.name} />
                    <span>{i.name}</span>
                  </span>
                </td>
                <td className="pick-recipe">
                  {i.composition && i.composition.length > 0 ? (
                    i.composition.map((c, j) => (
                      <GameIcon
                        key={j}
                        kind="items"
                        id={c}
                        size={20}
                        fallbackSrc={names.icon(c)}
                        fallbackName={names.name(c)}
                      />
                    ))
                  ) : (
                    <span className="muted">{i.kind === "component" ? "component" : "—"}</span>
                  )}
                </td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={2} className="muted">
                  No items match.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function FilterButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button type="button" className={active ? "active" : undefined} aria-pressed={active} onClick={onClick}>
      {children}
    </button>
  );
}

const matches = (q: string, ...fields: string[]) => fields.some((f) => f.toLowerCase().includes(q));
