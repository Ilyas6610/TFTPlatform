// The planner's "my goal" mode: the board is what the player is aiming for, so an augment picked at a stage is
// priced from where they'd typically be by then (fewer stars and items, a lower level) towards that board.
// These progressions are assumptions about a typical game, not measured: Riot's match data only has final boards.

import { SetData } from "../api/client";
import { Board } from "./board";

export interface StageProgress {
  /** Player level at the stage. */
  level: number;
  /** Stars a unit has at most by then (never more than its goal star). */
  maxStar: 1 | 2 | 3;
  /** Completed items held by then (a component is half), spread over the goal's carries first. */
  items: number;
  /**
   * Units the goal has at 3 stars and that cost this much or less are already 3-star by then: a reroll comp rolls for
   * its cheap carries before the 4-2 augment (1-costs around 3-1, 2-costs around 3-2), so by 4-2 it no longer wants their
   * copies. 3-costs roll at 4-1 on level 7 and often still want copies at 4-2 (TFT Academy's guides differ on it).
   */
  threeStarUpToCost: number;
}

export const STAGE_PROGRESS: Record<string, StageProgress> = {
  "2-1": { level: 4, maxStar: 1, items: 0.5, threeStarUpToCost: 0 },
  "3-2": { level: 6, maxStar: 2, items: 2, threeStarUpToCost: 0 },
  "4-2": { level: 7, maxStar: 2, items: 4, threeStarUpToCost: 2 },
};

/**
 * Where a player aiming for `goal` typically is at `stage`: the same units in the same order (so a unit's goal is
 * at the same index), stars capped, the goal's items handed out to its most-itemized units first, a lower level.
 */
export function boardAtStage(goal: Board, stage: string, data: SetData): Board {
  const p = STAGE_PROGRESS[stage];
  if (!p) return goal;
  const cost = new Map(data.units.map((u) => [u.apiName, u.cost]));
  const known = new Set(cost.keys());
  const order = goal.units
    .map((u, i) => ({ u, i }))
    .filter((x) => known.has(x.u.id) && x.u.items.length > 0)
    .sort((a, b) => b.u.items.length - a.u.items.length || a.i - b.i);
  const items: string[][] = goal.units.map(() => []);
  let left = Math.floor(p.items);
  for (const { u, i } of order) {
    for (const it of u.items) {
      if (left <= 0) break;
      items[i].push(it);
      left--;
    }
  }
  return {
    ...goal,
    level: Math.min(goal.level, p.level),
    units: goal.units.map((u, i) => {
      const done = u.star === 3 && (cost.get(u.id) ?? 99) <= p.threeStarUpToCost;
      return { ...u, star: (done ? 3 : Math.min(u.star, p.maxStar)) as 1 | 2 | 3, items: items[i] };
    }),
  };
}
