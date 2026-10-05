import { useState } from "react";
import { AssetKind, GameIcon } from "../assets/tft";
import { Names } from "./stats";

/** Searchable dropdown over ids present in the match data, most common first. */
export interface PickerOption {
  id: string;
  boards: number;
  label?: string; // searchable text; defaults to the id's display name
  sublabel?: string; // shown instead of label on indented rows
  iconId?: string; // icon to show, if not the id's own
  indent?: boolean; // a sub-entry of the row above
  badge?: { text: string; style: number }; // shown instead of an icon
}

export function Picker({
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
              {o.boards > 0 && <span className="muted">{o.boards}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
