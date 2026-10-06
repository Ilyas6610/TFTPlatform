import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  ExploreRow,
  MetaComp,
  PlayerMatchSummary,
  PlayerStats as Stats,
  getMetaBuilds,
  getPlayerStats,
} from "../api/client";
import { GameIcon } from "../assets/tft";
import { CompName } from "./CompName";
import { SetBackfill } from "./SetBackfill";
import { Names, avg, pct, placementTone, queueName } from "./stats";

// Breakdown rows from fewer games than this are shown but dimmed: one lucky
// game says little about a unit.
const MIN_GAMES = 3;

/**
 * A player's results from their stored games in one set: summary against
 * everyone, recent form, placement spread, queue split, the units, items
 * and traits they play most (units compared with the meta), and their comps.
 */
export function PlayerStats({
  puuid,
  refreshKey,
  set,
  onSet,
  names,
  costs,
  recent,
  region,
}: {
  puuid: string;
  /** Changes when the player's stored games change; stats reload. */
  refreshKey: number;
  set: number;
  onSet: (set: number) => void;
  names: Names;
  costs: Map<string, number>;
  /** Newest games across queues, for the recent form strip. */
  recent: PlayerMatchSummary[];
  /** The player's platform, for loading their whole set from Riot. */
  region: string;
}) {
  const navigate = useNavigate();
  const [queue, setQueue] = useState<number | "all">("all");
  const [stats, setStats] = useState<Stats | null>(null);
  const [metaAvg, setMetaAvg] = useState<Map<string, number>>(new Map());
  const [error, setError] = useState<string | null>(null);
  // Bumped as a whole-set load brings games in.
  const [reloads, setReloads] = useState(0);

  const scope = useMemo(() => {
    const q = new URLSearchParams({ set: String(set) });
    if (queue !== "all") q.set("queue", String(queue));
    return q.toString();
  }, [set, queue]);

  useEffect(() => setQueue("all"), [puuid, set]);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    getPlayerStats(puuid, scope)
      .then((s) => !cancelled && setStats(s))
      .catch(() => !cancelled && setError("couldn't load player stats"));
    // Everyone's per-unit average in the same scope, for "vs meta".
    getMetaBuilds(scope)
      .then((m) => !cancelled && setMetaAvg(new Map(m.units.map((u) => [u.id, u.avgPlacement]))))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [puuid, scope, refreshKey, reloads]);

  if (error) return <div className="error-box">{error}</div>;
  if (!stats) return <p className="muted">Loading stats…</p>;

  const s = stats.summary;
  const maxPlacements = Math.max(1, ...s.placements);
  const form = recent.slice(0, 20);
  const unitIcon = (id: string, size: number) => (
    <GameIcon
      kind="champions"
      id={id}
      size={size}
      fallbackSrc={names.icon(id)}
      fallbackName={names.name(id)}
      className={costs.get(id) ? `cost-${costs.get(id)}` : ""}
    />
  );

  return (
    <div className="player-stats">
      <div className="panel">
        <div className="history-head">
          <h3>Stats</h3>
          <div className="player-scope">
            {stats.sets.length > 1 && (
              <select value={set} onChange={(e) => onSet(Number(e.target.value))} aria-label="Set">
                {stats.sets.map((n) => (
                  <option key={n} value={n}>
                    Set {n}
                  </option>
                ))}
              </select>
            )}
            <select
              value={queue}
              onChange={(e) => setQueue(e.target.value === "all" ? "all" : Number(e.target.value))}
              aria-label="Queue"
            >
              <option value="all">All queues</option>
              {stats.queues.map((q) => (
                <option key={q.queueId} value={q.queueId}>
                  {queueName(q.queueId)} · {q.boards}
                </option>
              ))}
            </select>
          </div>
        </div>
        <p className="muted">
          From the {s.boards} stored games{queue === "all" ? "" : ` in ${queueName(queue)}`} in Set {set}.
        </p>
        <SetBackfill puuid={puuid} region={region} onGames={() => setReloads((n) => n + 1)} />

        {s.boards === 0 ? (
          <p className="muted">No stored games in this set and queue yet.</p>
        ) : (
          <>
            <div className="stat-tiles">
              <div className="stat-tile">
                <span className="stat-value">{s.boards}</span>
                <span className="muted">games</span>
              </div>
              <div className="stat-tile">
                <span className={`stat-value ${placementTone(s)}`}>{avg(s.avgPlacement)}</span>
                <span className="muted">avg place · everyone {avg(stats.baseline.avgPlacement)}</span>
              </div>
              <div className="stat-tile">
                <span className="stat-value">{pct(s.top4Rate)}</span>
                <span className="muted">top 4 · everyone {pct(stats.baseline.top4Rate)}</span>
              </div>
              <div className="stat-tile">
                <span className="stat-value">{pct(s.winRate)}</span>
                <span className="muted">wins · everyone {pct(stats.baseline.winRate)}</span>
              </div>
            </div>

            <div className="player-charts">
              <div>
                <div className="meta-section-title muted">Placements</div>
                <div className="placement-bars" aria-label="Placement distribution">
                  {s.placements.map((n, i) => (
                    <div key={i} className="placement-bar" title={`${n} games placed ${i + 1}`}>
                      <span className="bar-count">{n}</span>
                      <div className={`fill placement-${i + 1}`} style={{ height: `${(n / maxPlacements) * 100}%` }} />
                      <span className="muted">{i + 1}</span>
                    </div>
                  ))}
                </div>
              </div>
              {form.length > 0 && (
                <div>
                  <div className="meta-section-title muted">Recent form (all queues, newest first)</div>
                  <div className="form-strip">
                    {form.map((m) => (
                      <span
                        key={m.matchId}
                        className={`placement placement-${m.placement}`}
                        title={`${queueName(m.queueId)} · ${new Date(m.gameDatetime).toLocaleString()}`}
                        onClick={() => navigate(`/matches/${m.matchId}`)}
                      >
                        {m.placement}
                      </span>
                    ))}
                  </div>
                  <p className="muted">
                    Last {form.length}: {avg(form.reduce((n, m) => n + m.placement, 0) / form.length)} avg
                  </p>
                </div>
              )}
              {stats.patches.length > 0 && (
                <div>
                  <div className="meta-section-title muted">By patch</div>
                  <table className="compact">
                    <tbody>
                      {stats.patches.map((p) => (
                        <tr key={p.patch || "unknown"}>
                          <td>{p.patch ? `Patch ${p.patch}` : "Unknown"}</td>
                          <td className="num">{p.boards}</td>
                          <td className={`num ${placementTone(p)}`}>{avg(p.avgPlacement)}</td>
                          <td className="num">{pct(p.top4Rate)}</td>
                          <td
                            className={`num ${p.lpGames === 0 ? "muted" : p.lp >= 0 ? "good" : "bad"}`}
                            title={p.lpGames ? `Known LP change over ${p.lpGames} ranked games` : "No LP tracked yet"}
                          >
                            {p.lpGames ? `${p.lp > 0 ? "+" : ""}${p.lp} LP` : "—"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              {stats.queues.length > 1 && queue === "all" && (
                <div>
                  <div className="meta-section-title muted">By queue</div>
                  <table className="compact">
                    <tbody>
                      {stats.queues.map((q) => (
                        <tr key={q.queueId}>
                          <td>{queueName(q.queueId)}</td>
                          <td className="num">{q.boards}</td>
                          <td className={`num ${placementTone(q)}`}>{avg(q.avgPlacement)}</td>
                          <td className="num">{pct(q.top4Rate)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </>
        )}
      </div>

      {s.boards > 0 && (
        <div className="panel">
          <h3>What they play</h3>
          <div className="player-breakdowns">
            <BreakdownList
              title="Units"
              rows={stats.units}
              total={s.boards}
              label={(id) => (
                <span className="game-label">
                  {unitIcon(id, 22)}
                  {names.name(id)}
                </span>
              )}
              versus={metaAvg}
            />
            <BreakdownList
              title="Items"
              rows={stats.items}
              total={s.boards}
              label={(id) => (
                <span className="game-label">
                  <GameIcon kind="items" id={id} size={20} fallbackSrc={names.icon(id)} fallbackName={names.name(id)} />
                  {names.name(id)}
                </span>
              )}
            />
            <BreakdownList
              title="Traits"
              rows={byTrait(stats.traits)}
              total={s.boards}
              label={(id) => (
                <span className="game-label">
                  <GameIcon
                    kind="traits"
                    id={id}
                    size={20}
                    fallbackSrc={names.icon(id)}
                    fallbackName={names.name(id)}
                  />
                  {names.name(id)}
                </span>
              )}
              onPick={(id) => {
                const q = new URLSearchParams(scope);
                q.append("trait", id);
                navigate(`/explore?${q}`);
              }}
            />
          </div>
        </div>
      )}

      {stats.comps.length > 0 && (
        <div className="panel">
          <h3>Their comps</h3>
          <p className="muted">Level 8+ boards grouped like the Meta page; comps played at least twice.</p>
          <div className="player-comps">
            {stats.comps.map((c, i) => (
              <PlayerComp key={i} comp={c} names={names} unitIcon={unitIcon} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// Rows shown before "Show all".
const PREVIEW_ROWS = 6;

/** Trait tier rows merged into one row per trait (a board has one tier per trait). */
function byTrait(rows: ExploreRow[]): ExploreRow[] {
  const merged = new Map<string, ExploreRow>();
  for (const r of rows) {
    const m = merged.get(r.id);
    if (!m) {
      merged.set(r.id, { ...r, tier: undefined });
      continue;
    }
    const n = m.boards + r.boards;
    const mix = (a: number, b: number) => (a * m.boards + b * r.boards) / n;
    merged.set(r.id, {
      ...m,
      boards: n,
      avgPlacement: mix(m.avgPlacement, r.avgPlacement),
      top4Rate: mix(m.top4Rate, r.top4Rate),
      winRate: mix(m.winRate, r.winRate),
    });
  }
  return [...merged.values()].sort((a, b) => b.boards - a.boards);
}

/** A short ranked list: top rows with games and average, the rest behind "Show all". */
function BreakdownList({
  title,
  rows,
  total,
  label,
  versus,
  onPick,
}: {
  title: string;
  rows: ExploreRow[];
  total: number;
  label: (id: string) => JSX.Element;
  /** Everyone's average placement per id, for a "vs meta" column. */
  versus?: Map<string, number>;
  onPick?: (id: string) => void;
}) {
  const [all, setAll] = useState(false);
  const shown = all ? rows : rows.slice(0, PREVIEW_ROWS);
  return (
    <div className="breakdown-list">
      <div className="meta-section-title muted">{title}</div>
      {rows.length === 0 ? (
        <p className="muted">None yet.</p>
      ) : (
        <table className="compact">
          <thead>
            <tr>
              <th />
              <th className="num" title="Games (share of all their games)">
                Games
              </th>
              <th className="num">Avg</th>
              {versus && (
                <th className="num" title="Their average minus everyone's with this unit: negative is better">
                  vs meta
                </th>
              )}
            </tr>
          </thead>
          <tbody>
            {shown.map((r) => {
              const meta = versus?.get(r.id);
              const delta = meta === undefined ? undefined : r.avgPlacement - meta;
              return (
                <tr
                  key={r.id}
                  className={`${r.boards < MIN_GAMES ? "dim" : ""} ${onPick ? "clickable" : ""}`}
                  title={`${r.boards} games (${pct(r.boards / Math.max(1, total))}) · top 4 ${pct(r.top4Rate)} · ${pct(r.winRate)} wins`}
                  onClick={onPick && (() => onPick(r.id))}
                >
                  <td>{label(r.id)}</td>
                  <td className="num">{r.boards}</td>
                  <td className={`num ${placementTone(r)}`}>{avg(r.avgPlacement)}</td>
                  {versus && (
                    <td
                      className={`num ${delta === undefined ? "muted" : delta <= -0.25 ? "good" : delta >= 0.25 ? "bad" : ""}`}
                    >
                      {delta === undefined ? "—" : `${delta > 0 ? "+" : ""}${delta.toFixed(2)}`}
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {rows.length > PREVIEW_ROWS && (
        <button type="button" className="show-more" onClick={() => setAll(!all)}>
          {all ? "Show less" : `Show all ${rows.length}`}
        </button>
      )}
    </div>
  );
}

function PlayerComp({
  comp: c,
  names,
  unitIcon,
}: {
  comp: MetaComp;
  names: Names;
  unitIcon: (id: string, size: number) => JSX.Element;
}) {
  return (
    <div className="player-comp">
      <div className="advisor-comp-head">
        <CompName comp={c} names={names} />
        <span className={placementTone(c)}>{avg(c.avgPlacement)} avg</span>
        <span className="muted">
          {c.boards} games · top 4 {pct(c.top4Rate)} · {pct(c.winRate)} wins
        </span>
      </div>
      <div className="history-units">
        {c.board.map((u) => (
          <span
            key={u.id}
            className="history-unit"
            title={`${names.name(u.id)} — in ${pct(u.frequency)} of these games`}
          >
            <span className={`unit-stars star-${u.star}`}>{"★".repeat(u.star || 1)}</span>
            {unitIcon(u.id, 34)}
            <span className="unit-items">
              {u.items.map((it, j) => (
                <GameIcon
                  key={j}
                  kind="items"
                  id={it}
                  size={12}
                  fallbackSrc={names.icon(it)}
                  fallbackName={names.name(it)}
                />
              ))}
            </span>
          </span>
        ))}
      </div>
    </div>
  );
}
