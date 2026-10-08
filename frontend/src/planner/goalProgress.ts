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
}

export const STAGE_PROGRESS: Record<string, StageProgress> = {
  "2-1": { level: 4, maxStar: 1, items: 0.5 },
  "3-2": { level: 6, maxStar: 2, items: 2 },
  "4-2": { level: 7, maxStar: 2, items: 4 },
};

/**
 * Where a player aiming for `goal` typically is at `stage`: the same units in the same order (so a unit's goal is
 * at the same index), stars capped, the goal's items handed out to its most-itemized units first, a lower level.
 */
export function boardAtStage(goal: Board, stage: string, data: SetData): Board {
  const p = STAGE_PROGRESS[stage];
  if (!p) return goal;
  const known = new Set(data.units.map((u) => u.apiName));
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
    units: goal.units.map((u, i) => ({ ...u, star: Math.min(u.star, p.maxStar) as 1 | 2 | 3, items: items[i] })),
  };
}
