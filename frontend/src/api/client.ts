// Typed client for the Go API (see internal/apiserver). Every response DTO
// here mirrors the corresponding struct in internal/apiserver/dto.go and the
// handlers_*.go response types.

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: "unknown", message: res.statusText }));
    throw new ApiError(res.status, body.error ?? "unknown", body.message ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export interface PlayerProfile {
  puuid: string;
  gameName: string;
  tagLine: string;
  platformRegion: string;
  summonerLevel: number;
  profileIconId: number;
  source: "cache" | "live";
}

export function getPlayerProfile(region: string, gameName: string, tagLine: string) {
  return get<PlayerProfile>(
    `/api/v1/players/${encodeURIComponent(region)}/${encodeURIComponent(gameName)}/${encodeURIComponent(tagLine)}`,
  );
}

export interface PlayerMatchSummary {
  matchId: string;
  gameDatetime: string;
  tftSetNumber: number;
  placement: number;
  level: number;
}

// Passing region lets the server sync the player's history from Riot in the
// background when it's out of date; `refreshing` is true while that runs —
// re-fetch to pick up new matches.
export interface PlayerMatches {
  matches: PlayerMatchSummary[];
  syncedAt: string | null;
  refreshing: boolean;
  stale: boolean;
  staleReason?: string;
}

export function getPlayerMatches(puuid: string, region: string, limit = 20) {
  return get<PlayerMatches>(
    `/api/v1/players/${encodeURIComponent(puuid)}/matches?limit=${limit}&region=${encodeURIComponent(region)}`,
  );
}

// Match detail is the raw Riot TFT match payload (see
// internal/apiserver/handlers_match.go) plus a "source" marker; only the
// fields the UI actually renders are typed here, everything else in the
// payload is ignored rather than modeled.
export interface MatchParticipant {
  puuid: string;
  // Riot ID at the time of the match; absent in some older payloads.
  riotIdGameName?: string;
  riotIdTagline?: string;
  placement: number;
  level: number;
  last_round: number;
  total_damage_to_players: number;
  units: { character_id: string; tier: number; itemNames: string[] }[];
  traits: { name: string; num_units: number; style: number; tier_current: number; tier_total: number }[];
  augments?: string[];
}

export interface MatchDetail {
  source: "cache" | "live";
  metadata: { match_id: string };
  info: {
    game_datetime: number;
    game_length: number;
    game_version: string;
    tft_set_number: number;
    tft_game_type: string;
    participants: MatchParticipant[];
  };
}

export function getMatch(matchId: string) {
  return get<MatchDetail>(`/api/v1/matches/${encodeURIComponent(matchId)}`);
}

export interface LeaderboardEntry {
  puuid: string;
  gameName: string | null;
  tagLine: string | null;
  tier: string;
  rank: string | null;
  leaguePoints: number;
  wins: number;
  losses: number;
  fetchedAt: string;
}

// The server refreshes the snapshot from Riot when it's more than a couple
// of minutes old, and resolves missing Riot IDs in the background while
// `resolving` is true — re-fetch to pick them up.
export interface Leaderboard {
  platform: string;
  fetchedAt: string | null;
  stale: boolean;
  staleReason?: string;
  resolving: boolean;
  entries: LeaderboardEntry[];
}

export function getLeaderboard(platform: string, limit = 100) {
  return get<Leaderboard>(`/api/v1/leaderboard/${encodeURIComponent(platform)}?limit=${limit}`);
}

export interface UnitStat {
  characterId: string;
  gamesPlayed: number;
  avgPlacement: number;
  top4Rate: number;
  winRate: number;
  pickRate: number;
}

export function getMetaUnits(set: number) {
  return get<UnitStat[]>(`/api/v1/meta/units?set=${set}`);
}

export interface TraitStat {
  traitName: string;
  traitTier: number;
  gamesPlayed: number;
  avgPlacement: number;
  top4Rate: number;
  winRate: number;
}

export function getMetaTraits(set: number) {
  return get<TraitStat[]>(`/api/v1/meta/traits?set=${set}`);
}

export interface AugmentStat {
  augmentId: string;
  gamesPlayed: number;
  avgPlacement: number;
  top4Rate: number;
  winRate: number;
}

export function getMetaAugments(set: number) {
  return get<AugmentStat[]>(`/api/v1/meta/augments?set=${set}`);
}

const STALE_REASONS: Record<string, string> = {
  riot_api_key_expired: "the Riot API key needs rotation",
  riot_api_rate_limited: "Riot API rate limit reached",
  riot_api_timeout: "Riot API is busy",
};

/** " (reason)" for a known staleReason, or "" — for "couldn't update" notices. */
export function staleSuffix(reason: string | undefined): string {
  const text = reason && STALE_REASONS[reason];
  return text ? ` (${text})` : "";
}

// Set data (units, traits, augments, items) as of one patch — see
// internal/setdata. Descriptions are pre-rendered plain text with "\n"
// line breaks; "[[Label]]" marks a value the game computes at runtime that
// isn't in the published data.
export interface SetUnit {
  apiName: string;
  name: string;
  icon?: string;
  cost: number;
  traits: string[];
  stats: Record<string, number>;
  ability: { name: string; desc: string; values: Record<string, number[]>; descSource?: DescSource };
}

// Set when a description came from a third-party source because Riot's
// exported data leaves its numbers out — see internal/setdata/overrides.go.
export interface DescSource {
  name: string;
  url?: string;
}

export interface SetTraitBreakpoint {
  minUnits: number;
  maxUnits: number;
  style: number;
  text?: string;
  values: Record<string, number>;
}

export interface SetTrait {
  apiName: string;
  name: string;
  icon?: string;
  desc: string;
  breakpoints: SetTraitBreakpoint[];
}

export interface RewardTable {
  title: string;
  columns: string[];
  rows: string[][];
  notes?: string[];
}

// Hand-curated reward tables, cited to their community source — see
// internal/setdata/rewards.
export interface AugmentRewards {
  sourceName: string;
  sourceUrl: string;
  updated: string;
  tables: RewardTable[];
}

export interface SetAugment {
  apiName: string;
  name: string;
  icon?: string;
  desc: string;
  tier: number;
  traits?: string[];
  values: Record<string, number>;
  rewards?: AugmentRewards;
  descSource?: DescSource;
}

export interface SetItem {
  apiName: string;
  name: string;
  icon?: string;
  desc: string;
  kind: string;
  variant?: string;
  composition?: string[];
  values: Record<string, number>;
  descSource?: DescSource;
}

export interface SetVersion {
  version: string;
  patch: string;
  fetchedAt: string;
}

export interface SetData {
  setNumber: number;
  version: string;
  patch: string;
  fetchedAt: string;
  versions: SetVersion[];
  units: SetUnit[];
  traits: SetTrait[];
  augments: SetAugment[];
  items: SetItem[];
  // Set 18's set-mechanic items, kept apart from regular items.
  wisps: SetItem[];
}

export function getSetData(set: number, version?: string) {
  const q = version ? `?version=${encodeURIComponent(version)}` : "";
  return get<SetData>(`/api/v1/sets/${set}/data${q}`);
}

export interface PatchChange {
  category: "unit" | "trait" | "augment" | "item" | "wisp";
  apiName: string;
  name: string;
  kind: "changed" | "added" | "removed" | "renamed" | "text";
  // For "renamed": the previous name. For "changed": the value's name.
  field?: string;
  old?: number;
  new?: number;
}

export interface PatchNotes {
  patch: string;
  tftPatch?: string;
  officialNotesUrl?: string;
  version: string;
  previousPatch: string;
  previousVersion: string;
  fetchedAt: string;
  changes: PatchChange[];
}

export function getSetPatches(set: number) {
  return get<PatchNotes[]>(`/api/v1/sets/${set}/patches`);
}
