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

async function get<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
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

export interface PlayerBoardUnit {
  id: string;
  star: number;
  items: string[];
}

export interface PlayerActiveTrait {
  id: string;
  units: number;
  tier: number;
  style: number; // Riot's badge style: 1 bronze .. 4+ prismatic/unique
}

export interface PlayerMatchSummary {
  matchId: string;
  gameDatetime: string;
  tftSetNumber: number;
  queueId: number;
  placement: number;
  level: number;
  units: PlayerBoardUnit[];
  traits: PlayerActiveTrait[]; // active only, highest style first
  patch?: string; // "18.3", from the game's date for Set 18
  // LP gained/lost, when rank snapshots bracket the game; games > 1 means
  // the change covers that many games (shown on the newest of them).
  lp?: { delta: number; games: number };
  // Double Up only: the teammate and the team's placement (1-4).
  partner?: PlayerRef;
  team?: number;
  lobby?: LobbyStrength;
}

/** How strong a game's opponents were: their average Ranked standing (value on RankPoint's scale). */
export interface LobbyStrength {
  value: number;
  known: number; // opponents with a known rank
  current: number; // of those, ranked by today's ladder rather than near the game
  opponents: number;
}

/** A player by PUUID with the Riot ID we know (from Riot's match data when not resolved). */
export interface PlayerRef {
  puuid: string;
  gameName?: string;
  tagLine?: string;
}

/** A Double Up teammate and the team's results together. */
export interface DoubleUpPartner extends PlayerRef {
  games: number;
  avgTeamPlacement: number; // 1-4
  top2Rate: number;
  winRate: number;
  lastPlayed: string;
}

/** A rank snapshot with its place on one linear LP scale (100 per division, Master = 2800). */
export interface RankPoint extends RankEntry {
  value: number;
}

/** A whole-set history load (one at a time server-wide). */
export interface BackfillStatus {
  state: "idle" | "running" | "done" | "failed";
  found: number; // the player's games this set, per Riot (capped at 500)
  missing: number; // not stored when the load started
  fetched: number;
  since?: string;
  error?: string;
  finishedAt?: string;
}

export function getBackfill(puuid: string) {
  return get<BackfillStatus>(`/api/v1/players/${encodeURIComponent(puuid)}/backfill`);
}

/** Starts loading the player's whole current set; a 409 ApiError (code "busy") if another load is running. */
export function startBackfill(puuid: string, region: string) {
  return get<BackfillStatus>(
    `/api/v1/players/${encodeURIComponent(puuid)}/backfill?region=${encodeURIComponent(region)}`,
    { method: "POST" },
  );
}

/** An estimated Ranked standing right after an older game (before the first recorded rank). */
export interface EstimatedLP {
  matchId: string;
  gameDatetime: string;
  placement: number;
  value: number; // same linear scale as RankPoint.value
}

export interface RankHistory {
  history: Record<string, RankPoint[]>; // recorded rank changes per queue, oldest first
  estimated: EstimatedLP[]; // Ranked only, oldest first; typical LP per placement
}

export function getRankHistory(puuid: string) {
  return get<RankHistory>(`/api/v1/players/${encodeURIComponent(puuid)}/ranks`);
}

export interface RankEntry {
  queueType: string; // RANKED_TFT, RANKED_TFT_DOUBLE_UP
  tier: string;
  rank: string;
  leaguePoints: number;
  wins: number;
  losses: number;
  fetchedAt: string;
}

// Passing region lets the server sync the player's history from Riot in the
// background: the newest games when out of date (offset 0), or an older page
// that isn't fully stored. `refreshing` is true while that runs — re-fetch
// to pick up the games. `hasMore` means an older page may exist.
export interface PlayerMatches {
  matches: PlayerMatchSummary[];
  ranks?: RankEntry[]; // first page only: latest known rank per queue
  hasMore: boolean;
  syncedAt: string | null;
  refreshing: boolean;
  stale: boolean;
  staleReason?: string;
  /** Answer to refresh: it was synced moments ago, so nothing was started. */
  refreshTooSoon?: boolean;
}

/** refresh asks the server to sync the newest games now rather than waiting for the history to age (the Update button). */
export function getPlayerMatches(puuid: string, region: string, limit = 20, offset = 0, refresh = false) {
  const q = new URLSearchParams({ limit: String(limit), offset: String(offset), region });
  if (refresh) q.set("refresh", "1");
  return get<PlayerMatches>(`/api/v1/players/${encodeURIComponent(puuid)}/matches?${q}`);
}

export interface PlayerQueueStats extends PlacementStats {
  queueId: number;
}

/** A player's results in one set/queue scope, from their stored games. */
export interface PlayerStats {
  sets: number[]; // sets with stored games, newest first
  summary: PlacementStats & { placements: number[] };
  baseline: PlacementStats; // everyone in the same scope
  queues: PlayerQueueStats[];
  units: ExploreRow[];
  items: ExploreRow[];
  traits: ExploreRow[];
  comps: MetaComp[]; // the player's own comps (2+ games)
  // By patch, newest first; lp is the known change over lpGames ranked games.
  patches: (PlacementStats & { patch: string; lp: number; lpGames: number })[];
  // Double Up teammates in the set, most games first (none when the queue scope leaves out Double Up).
  partners: DoubleUpPartner[];
  sessions: PlayerSessions;
}

/** Play sessions (games less than 30 minutes apart) in the set and queue scope. */
export interface PlayerSessions {
  sessions: number;
  avgGames: number;
  longest: number;
  byPosition: (PlacementStats & { game: number })[]; // game 5 = 5th and later
  // The next game in the same session after a top 4, a bottom 4, and two bottom 4s in a row.
  afterTop4: PlacementStats;
  afterBottom4: PlacementStats;
  afterTwoBottom4: PlacementStats;
}

/** query: set and optional queue/level. */
export function getPlayerStats(puuid: string, query: string) {
  return get<PlayerStats>(`/api/v1/players/${encodeURIComponent(puuid)}/stats?${query}`);
}

/** A unit the player does notably worse or better with than expected (everyone's average shifted by their edge). */
export interface AdviceUnit {
  unit: string;
  games: number;
  avg: number;
  metaAvg: number;
  expected: number;
}

/** The player's usual build on a unit next to one everyone does clearly better with. */
export interface AdviceBuild {
  unit: string;
  theirs: MetaBuild; // the player's games
  theirsMeta: PlacementStats; // everyone's games with the player's build
  better: MetaBuild; // everyone's games
}

export interface PlayerAdvice {
  edge: number; // the player's average minus everyone's (negative = better)
  weak: AdviceUnit[];
  strong: AdviceUnit[];
  builds: AdviceBuild[];
}

/** query: set and optional queue/level, as for stats. */
export function getPlayerAdvice(puuid: string, query: string) {
  return get<PlayerAdvice>(`/api/v1/players/${encodeURIComponent(puuid)}/advice?${query}`);
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
  // Double Up team (Set 17 payloads); Set 18 dropped it, teams are then placement pairs.
  partner_group_id?: number;
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
    queue_id?: number;
    participants: MatchParticipant[];
  };
}

export function getMatch(matchId: string) {
  return get<MatchDetail>(`/api/v1/matches/${encodeURIComponent(matchId)}`);
}

/** A game's lobby strength: the average Ranked standing (RankPoint scale) of the participants whose rank is known. */
export interface MatchLobby {
  average: number; // 0 when none is known
  known: number;
  total: number;
}

export function getMatchLobby(matchId: string) {
  return get<MatchLobby>(`/api/v1/matches/${encodeURIComponent(matchId)}/lobby`);
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
  /** Season ranked games and top 4 share, from Riot's league entry (its wins are top 4s). */
  games: number;
  top4Rate: number | null;
  /** Only the ranked games we have stored; null when there are none. */
  stored: LeaderboardStoredStats | null;
}

export interface LeaderboardStoredStats {
  games: number;
  winRate: number;
  top4Rate: number;
  avgPlacement: number;
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

// Stats explorer — see internal/store/explore.go and handlers_explore.go.
export interface PlacementStats {
  boards: number;
  avgPlacement: number;
  top4Rate: number;
  winRate: number;
}

export interface ExploreRow extends PlacementStats {
  id: string;
  tier?: number; // traits: tier reached; unit stars: star level
}

export interface ExploreResult {
  summary: PlacementStats & { placements: number[] };
  baseline: PlacementStats;
  units: ExploreRow[];
  items: ExploreRow[];
  traits: ExploreRow[];
  unitItems: ExploreRow[][]; // per unit condition, in order
  unitStars: ExploreRow[][];
}

export interface ExploreOptions {
  boards: number;
  queues: { id: number; gameType: string; boards: number }[];
  units: { id: string; boards: number }[];
  items: { id: string; boards: number }[];
  traits: { id: string; boards: number; maxUnits: number; tiers: { tier: number; boards: number }[] }[];
  levels: [number, number];
}

export function getExploreOptions(set: number) {
  return get<ExploreOptions>(`/api/v1/explore/options?set=${set}`);
}

/** query is the explorer's URL search string (set, queue, level, unit, item, trait). */
export function explore(query: string) {
  return get<ExploreResult>(`/api/v1/explore?${query}`);
}

// Meta page — see internal/store/meta.go.
export interface MetaBuild extends PlacementStats {
  items: string[]; // exact 3-item build, sorted
}

export interface MetaUnit extends PlacementStats {
  id: string;
  pickRate: number;
  builds: MetaBuild[]; // most common exact builds (seen 3+ times)
  items: ExploreRow[]; // most-held items
}

export interface MetaResult {
  boards: number;
  units: MetaUnit[];
}

/** query: set and optional queue/level parameters, as for explore(). */
export function getMetaBuilds(query: string) {
  return get<MetaResult>(`/api/v1/meta/builds?${query}`);
}

// Team compositions — see internal/comps.
export interface CompUnit {
  id: string;
  star: number;
  items: string[];
  frequency: number; // share of the comp's boards fielding it
}

export interface CompVariant extends PlacementStats {
  add: string[];
  remove: string[];
}

export interface CompFlex extends PlacementStats {
  id: string;
  frequency: number;
}

export interface MetaComp extends PlacementStats {
  playRate: number;
  board: CompUnit[]; // the exact most-played board, carries first
  boardStats: PlacementStats; // boards that ran exactly it
  traits: { id: string; units: number; tier: number }[]; // active on the exact board
  variants: CompVariant[];
  flex: CompFlex[];
}

export function getMetaComps(query: string) {
  return get<{ boards: number; comps: MetaComp[] }>(`/api/v1/meta/comps?${query}`);
}

// Build suggestions from the units and items a player holds — see
// internal/suggest and handlers_suggest.go.
export interface SuggestStep {
  item: string;
  from?: string[]; // the two components to combine; absent = held as is
}

export interface SuggestBuild extends PlacementStats {
  items: string[]; // the full build, sorted
  steps: SuggestStep[]; // items makeable now
  missing: string[];
  ready: boolean;
}

export interface SuggestFit {
  unit: string;
  items: string[];
  steps: SuggestStep[];
  missing: string[];
  alt?: boolean; // not the board's usual build: one that uses your emblem or artifact
  enablers?: string[]; // the emblems/artifacts that build uses
}

/** Items only: an emblem (held or makeable) for a trait the board plays. */
export interface SuggestEmblemFit {
  item: string;
  trait: string;
  from?: string[];
}

export interface SuggestComp {
  comp: MetaComp;
  have: string[];
  need: string[];
  fits: SuggestFit[];
  emblems: SuggestEmblemFit[];
}

export interface SuggestResult {
  plan: { unit: string; build: SuggestBuild; aim: boolean }[]; // aim: a target, nothing builds toward it yet
  leftover: string[];
  units: { id: string; options: SuggestBuild[] }[];
  comps: SuggestComp[];
  // Only without units: the units the items fit best, real builds by their
  // items (whoever carried them), and what the held components make now.
  candidates: { unit: string; build: SuggestBuild }[];
  builds: SuggestItemBuild[];
  crafts: SuggestCraft[];
}

/** A unit that carried a build or item, and how it did. */
export interface SuggestUnitUse extends PlacementStats {
  id: string;
}

/** A real build judged by its items: stats combine every unit that carried it. */
export interface SuggestItemBuild extends SuggestBuild {
  units: SuggestUnitUse[]; // main carriers
}

/** A completed item held whole (no from) or makeable from two held components. */
export interface SuggestCraft extends PlacementStats {
  item: string;
  from?: string[];
  units: SuggestUnitUse[];
}

/** query: set and optional queue/level, plus have_unit and have_item (repeat per copy). */
export function getExploreSuggest(query: string) {
  return get<SuggestResult>(`/api/v1/explore/suggest?${query}`);
}

// The in-game Team Planner's number for each shop champion — see
// internal/setdata/plannercodes.go and frontend/src/planner/teamCode.ts.
export interface PlannerCodes {
  set: number;
  codes: Record<string, number>;
}

export function getPlannerCodes(set: number) {
  return get<PlannerCodes>(`/api/v1/sets/${set}/planner-codes`);
}
