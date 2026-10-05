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
