// Shared pieces for stats pages (Explorer, Meta): display names and trait
// tiers from set data, placement stat cells, and the tier-grouped traits
// table.

import { useEffect, useMemo, useState } from "react";
import { ExploreRow, PlacementStats, SetData } from "../api/client";
import { GameIcon } from "../assets/tft";

export const pct = (n: number) => `${Math.round(n * 100)}%`;

export const avg = (n: number) => n.toFixed(2);

export const QUEUE_NAMES: Record<number, string> = {
  1090: "Normal",
  1100: "Ranked",
  1130: "Hyper Roll",
  1160: "Double Up",
};

export const queueName = (id: number) => QUEUE_NAMES[id] ?? `Queue ${id}`;

export function StatCells({ row: r, total }: { row: PlacementStats; total: number }) {
  return (
    <>
      <td className="num">
        {r.boards} <span className="muted share">{pct(r.boards / Math.max(1, total))}</span>
      </td>
      <td className={`num ${placementTone(r)}`}>{avg(r.avgPlacement)}</td>
      <td className="num">{pct(r.top4Rate)}</td>
    </>
  );
}

// Trait style -> the bronze/silver/gold/prismatic badge classes (as in

export const TIER_STYLE: Record<number, number> = { 1: 1, 2: 2, 3: 2, 4: 3, 5: 3, 6: 4 };

/**
 * Traits grouped by trait: a summary row (any active tier) followed by one
 * row per tier reached. Clicking a tier filters to exactly that tier.
 */

export function TraitsBreakdown({
  rows,
  names,
  total,
  onPick,
}: {
  rows: ExploreRow[];
  names: Names;
  total: number;
  onPick: (id: string, tier?: TraitTier) => void;
}) {
  const [all, setAll] = useState(false);
  useEffect(() => setAll(false), [rows]);
  const groups = useMemo(() => {
    const byTrait = new Map<string, ExploreRow[]>();
    for (const r of rows) byTrait.set(r.id, [...(byTrait.get(r.id) ?? []), r]);
    return [...byTrait.entries()]
      .map(([id, tiers]) => {
        // A board has one tier per trait, so tier rows add up exactly.
        const boards = tiers.reduce((n, t) => n + t.boards, 0);
        const weighted = (k: "avgPlacement" | "top4Rate" | "winRate") =>
          tiers.reduce((n, t) => n + t[k] * t.boards, 0) / Math.max(1, boards);
        const summary: PlacementStats = {
          boards,
          avgPlacement: weighted("avgPlacement"),
          top4Rate: weighted("top4Rate"),
          winRate: weighted("winRate"),
        };
        return { id, summary, tiers: [...tiers].sort((a, b) => (a.tier ?? 0) - (b.tier ?? 0)) };
      })
      .sort((a, b) => b.summary.boards - a.summary.boards);
  }, [rows]);
  if (groups.length === 0) return <p className="muted">Nothing to show.</p>;
  const shown = all ? groups : groups.slice(0, 8);
  return (
    <>
      <table className="breakdown traits-breakdown">
        <thead>
          <tr>
            <th></th>
            <th>Boards</th>
            <th>Avg</th>
            <th>Top 4</th>
          </tr>
        </thead>
        {shown.map((g) => (
          <tbody key={g.id} className="trait-group">
            <tr
              className="clickable explore-trait-row"
              title={`${names.name(g.id)}, any tier — add as a condition`}
              onClick={() => onPick(g.id)}
            >
              <td className="name-cell">
                <GameIcon
                  kind="traits"
                  id={g.id}
                  size={20}
                  fallbackSrc={names.icon(g.id)}
                  fallbackName={names.name(g.id)}
                />
                <span>{names.name(g.id)}</span>
              </td>
              <StatCells row={g.summary} total={total} />
            </tr>
            {g.tiers.map((r) => {
              const tier = names.tier(g.id, r.tier ?? 0);
              const label = tier ? tierLabel(tier) : `tier ${r.tier}`;
              return (
                <tr
                  key={r.tier}
                  className="clickable explore-tier-row"
                  title={`${names.name(g.id)} ${label} — add as a condition`}
                  onClick={() => onPick(g.id, tier)}
                >
                  <td className="name-cell">
                    <span className={`trait-badge trait-style-${TIER_STYLE[tier?.style ?? 1] ?? 1}`}>{label}</span>
                  </td>
                  <StatCells row={r} total={total} />
                </tr>
              );
            })}
          </tbody>
        ))}
      </table>
      {groups.length > 8 && (
        <button type="button" className="show-more" onClick={() => setAll(!all)}>
          {all ? "Show less" : `Show all ${groups.length} traits`}
        </button>
      )}
    </>
  );
}

export const tierLabel = (t: TraitTier) =>
  t.maxUnits === 0 ? `${t.minUnits}+` : t.minUnits === t.maxUnits ? `${t.minUnits}` : `${t.minUnits}–${t.maxUnits}`;

export function placementTone(r: PlacementStats): string {
  if (r.boards < 5) return "muted";
  return r.avgPlacement <= 4 ? "good" : r.avgPlacement >= 5 ? "bad" : "";
}

export interface TraitTier {
  index: number; // 1-based, as tier_current
  minUnits: number;
  maxUnits: number; // 0 for the top tier (no upper bound)
  style: number;
}

export interface Names {
  name: (id: string) => string;
  icon: (id: string) => string | undefined;
  tiers: (traitId: string) => TraitTier[];
  /** The breakpoint for tier_current = tier (1-based). */
  tier: (traitId: string, tier: number) => TraitTier | undefined;
}

/** Display names, icons and trait tiers from the set data. */

export function buildNames(d: SetData | null): Names {
  const byId = new Map<string, { name: string; icon?: string }>();
  const traitTiers = new Map<string, TraitTier[]>();
  if (d) {
    for (const x of [...d.units, ...d.items, ...(d.wisps ?? []), ...d.augments]) byId.set(x.apiName, x);
    for (const t of d.traits) {
      byId.set(t.apiName, t);
      const bps = t.breakpoints.filter((b) => b.minUnits > 0);
      traitTiers.set(
        t.apiName,
        bps.map((b, i) => ({
          index: i + 1,
          minUnits: b.minUnits,
          // A tier runs up to the next breakpoint; the last one is open.
          // Some traits repeat a breakpoint (Rival: 1, 1, 2), so a tier
          // never ends below where it starts.
          maxUnits: i + 1 < bps.length ? Math.max(b.minUnits, bps[i + 1].minUnits - 1) : 0,
          style: b.style,
        })),
      );
    }
  }
  return {
    name: (id) => byId.get(id)?.name ?? id.replace(/^(DA_18_|DA_|TFT\d*_Item_|TFT\d*_)/, ""),
    icon: (id) => byId.get(id)?.icon,
    tiers: (id) => traitTiers.get(id) ?? [],
    tier: (id, tier) => traitTiers.get(id)?.[tier - 1],
  };
}
