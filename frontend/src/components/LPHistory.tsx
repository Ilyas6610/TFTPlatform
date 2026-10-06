import { useEffect, useState } from "react";
import { RankHistory, getRankHistory } from "../api/client";
import { LPChart } from "./LPChart";

const QUEUES: [string, string][] = [
  ["RANKED_TFT", "Ranked"],
  ["RANKED_TFT_DOUBLE_UP", "Double Up"],
];

/**
 * The player's LP over time per ranked queue: recorded rank snapshots, and
 * for Ranked an estimate for the older stored games. reloadKey changes when
 * a sync may have recorded a new snapshot.
 */
export function LPHistory({ puuid, reloadKey }: { puuid: string; reloadKey: string }) {
  const [data, setData] = useState<RankHistory | null>(null);
  const [queue, setQueue] = useState("RANKED_TFT");

  useEffect(() => {
    let cancelled = false;
    getRankHistory(puuid)
      .then((d) => {
        if (cancelled) return;
        setData(d);
        const h = d.history;
        // Default to a queue that has data.
        setQueue((q) => (h[q]?.length ? q : (QUEUES.find(([k]) => h[k]?.length)?.[0] ?? q)));
      })
      .catch(() => !cancelled && setData(null));
    return () => {
      cancelled = true;
    };
  }, [puuid, reloadKey]);

  const available = QUEUES.filter(([k]) => data?.history[k]?.length);
  if (!data || available.length === 0) return null;
  const points = data.history[queue] ?? [];

  return (
    <div className="panel">
      <div className="history-head">
        <h3>LP history</h3>
        {available.length > 1 && (
          <div className="mode-tabs" role="tablist" aria-label="Ranked queue">
            {available.map(([k, label]) => (
              <button type="button" role="tab" key={k} aria-selected={queue === k} onClick={() => setQueue(k)}>
                {label}
              </button>
            ))}
          </div>
        )}
      </div>
      <LPChart points={points} estimated={queue === "RANKED_TFT" ? data.estimated : []} />
      <p className="muted lp-note">
        Recorded since {new Date(points[0].fetchedAt).toLocaleDateString()}: Riot keeps no past LP, so the line before
        that is estimated and new points are added whenever the profile updates.
      </p>
    </div>
  );
}
