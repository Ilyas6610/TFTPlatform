import { useMemo, useState } from "react";
import { SetItem, SetUnit } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Names } from "./stats";

// Tables to pick from in the board planner: every unit, every holdable item.
// Click a row to choose it. Both filter by text and by cost or kind, and
// scroll inside their own box so a long list doesn't push the page down.

export function UnitTable({
  units,
  names,
  onPick,
  disabled,
}: {
  units: SetUnit[];
  names: Names;
  onPick: (id: string) => void;
  /** Board full: rows stay visible but can't be picked. */
  disabled?: boolean;
}) {
  const [query, setQuery] = useState("");
  const [cost, setCost] = useState(0); // 0 = all
  const costs = useMemo(() => [...new Set(units.map((u) => u.cost))].sort((a, b) => a - b), [units]);
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return [...units]
      .filter((u) => (cost === 0 || u.cost === cost) && (!q || matches(q, u.name, ...u.traits)))
      .sort((a, b) => a.cost - b.cost || a.name.localeCompare(b.name));
  }, [units, query, cost]);

  return (
    <div className="pick-table">
      <div className="pick-table-controls">
        <input
          value={query}
          placeholder="Search units or traits…"
          aria-label="Search units"
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="pick-filters" role="group" aria-label="Cost">
          <FilterButton active={cost === 0} onClick={() => setCost(0)}>
            All
          </FilterButton>
          {costs.map((c) => (
            <FilterButton key={c} active={cost === c} onClick={() => setCost(c)}>
              {c}g
            </FilterButton>
          ))}
        </div>
      </div>
      <div className="pick-table-wrap">
        <table className="pick-grid">
          <thead>
            <tr>
              <th>Unit</th>
              <th>Cost</th>
              <th>Traits</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((u) => (
              <tr
                key={u.apiName}
                className={disabled ? undefined : "clickable"}
                title={disabled ? "The board is full" : `Add ${u.name}`}
                onClick={() => !disabled && onPick(u.apiName)}
              >
                <td>
                  <span className="pick-name">
                    <GameIcon
                      kind="champions"
                      id={u.apiName}
                      size={26}
                      fallbackSrc={u.icon}
                      fallbackName={u.name}
                      className={`cost-${u.cost}`}
                    />
                    <span>{names.name(u.apiName)}</span>
                  </span>
                </td>
                <td className="num">{u.cost}g</td>
                <td className="muted pick-traits">{u.traits.join(" · ")}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={3} className="muted">
                  No units match.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

const KIND_LABELS: [string, string][] = [
  ["completed", "Completed"],
  ["component", "Components"],
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
