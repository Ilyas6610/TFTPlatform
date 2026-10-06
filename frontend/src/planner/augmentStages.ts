// Augments are offered three times a game, and which augments can be offered
// depends on the stage. This module is the one place that knows the rule.
//
// Current data: tft.tools and tactics.tools list every Set 18 augment under
// all of "2-1, 3-2, 4-2", so every tier can be picked in every slot, and the
// slots differ only in label. Our own set data has no stage information
// (internal/setdata), and 29 augments there have tier 0 (tier unknown); they
// are real picks (Hedge Fund, Prismatic Ticket, Portable Forge...), so they
// stay selectable. If a rule turns up — a tier that can't appear in a slot,
// or a single augment tied to some stages — put it in OFFERED_TIERS or
// SLOT_EXCEPTIONS below; the planner's pickers, links and tests all go
// through `availableAt`.

import { SetAugment } from "../api/client";

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

/** Tiers (0 = unknown, 1 silver, 2 gold, 3 prismatic) that can be offered in each slot. */
export const OFFERED_TIERS: number[][] = [
  [0, 1, 2, 3],
  [0, 1, 2, 3],
  [0, 1, 2, 3],
];

/** Augments restricted to some slots, by apiName -> slot indexes. */
export const SLOT_EXCEPTIONS: Record<string, number[]> = {};

export function availableAt(aug: Pick<SetAugment, "apiName" | "tier">, slot: number): boolean {
  const only = SLOT_EXCEPTIONS[aug.apiName];
  if (only) return only.includes(slot);
  return OFFERED_TIERS[slot]?.includes(aug.tier) ?? false;
}
