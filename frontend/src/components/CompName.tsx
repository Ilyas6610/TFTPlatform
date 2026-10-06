import { MetaComp } from "../api/client";
import { GameIcon } from "../assets/tft";
import { Names } from "./stats";

/**
 * A comp's name: the traits it invests in (a higher tier or 3+ units), then
 * its itemized carries. Flexible boards with only 2-unit traits go by their
 * carries alone ("Aphelios & Nidalee").
 */
export function CompName({ comp: c, names }: { comp: MetaComp; names: Names }) {
  const mainTraits = c.traits
    .filter((t) => t.tier >= 2 || t.units >= 3)
    .sort((a, b) => b.tier - a.tier || b.units - a.units)
    .slice(0, 2);
  const carries = c.board.filter((u) => u.items.length > 0).slice(0, 2);
  return (
    <div className="comp-name">
      {mainTraits.map((t) => (
        <span key={t.id} className="game-label">
          <GameIcon kind="traits" id={t.id} size={18} fallbackSrc={names.icon(t.id)} fallbackName={names.name(t.id)} />
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
  );
}
