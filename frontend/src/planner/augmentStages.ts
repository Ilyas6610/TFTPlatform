// Augments are offered three times a game (stages 2-1, 3-2 and 4-2), and an
// augment can't necessarily be offered at all three. Riot's export and our set
// data don't say which, so this reads `augmentStages.data.json`: per augment
// its tier and the stages it can be offered at, as listed by tactics.tools
// (see scripts/fetch-augment-stages.mjs; refresh with `npm run augment-stages`
// after a patch). The planner's pickers and shared-link cleaning both go
// through `availableAt`.
//
// An augment missing from the data (newer than the last refresh) is treated
// as offerable everywhere rather than hidden. One listed with no stage at all
// can't be offered normally (it is granted by something else), so no slot
// lists it.

import { SetAugment } from "../api/client";
import raw from "./augmentStages.data.json";

export interface AugmentSlot {
  /** Game stage the choice is offered at. */
  stage: string;
  label: string;
}

export const AUGMENT_SLOTS: AugmentSlot[] = [
  { stage: "2-1", label: "First augment" },
  { stage: "3-2", label: "Second augment" },
  { stage: "4-2", label: "Third augment" },
];

export interface StageEntry {
  tier: string;
  /** One flag per slot: 1 if it can be offered there. */
  stages: number[];
}

export const STAGE_DATA = raw.augments as Record<string, StageEntry>;

export function availableAt(aug: Pick<SetAugment, "apiName">, slot: number, table: Record<string, StageEntry> = STAGE_DATA): boolean {
  if (slot < 0 || slot >= AUGMENT_SLOTS.length) return false;
  const entry = table[aug.apiName];
  return entry ? entry.stages[slot] === 1 : true;
}

const TIER_NUMBER: Record<string, number> = { silver: 1, gold: 2, prismatic: 3 };

/** The augment's tier (1 silver, 2 gold, 3 prismatic; 0 unknown): our set data's, else the stage data's. */
export function augmentTier(aug: Pick<SetAugment, "apiName" | "tier">, table: Record<string, StageEntry> = STAGE_DATA): number {
  if (aug.tier > 0) return aug.tier;
  return TIER_NUMBER[table[aug.apiName]?.tier ?? ""] ?? 0;
}

/** Stages (like "3-2") the augment can be offered at, or null if unknown. */
export function stagesOf(aug: Pick<SetAugment, "apiName">, table: Record<string, StageEntry> = STAGE_DATA): string[] | null {
  const entry = table[aug.apiName];
  return entry ? AUGMENT_SLOTS.filter((_, i) => entry.stages[i] === 1).map((s) => s.stage) : null;
}
