import { useEffect, useState } from "react";
import { RankPoint, getRankHistory } from "../api/client";
import { LPChart, rankText } from "./LPChart";

const QUEUES: [string, string][] = [
  ["RANKED_TFT", "Ranked"],
  ["RANKED_TFT_DOUBLE_UP", "Double Up"],
];

/**
 * The player's LP over time per ranked queue, from recorded rank snapshots.
 * reloadKey changes when a sync may have recorded a new snapshot.
 */
export function LPHistory({ puuid, reloadKey }: { puuid: string; reloadKey: string }) {
  const [history, setHistory] = useState<Record<string, RankPoint[]> | null>(null);
  const [queue, setQueue] = useState("RANKED_TFT");

  useEffect(() => {
    let cancelled = false;
    getRankHistory(puuid)
      .then((h) => {
        if (cancelled) return;
        setHistory(h);
        // Default to a queue that has data.
        setQueue((q) => (h[q]?.length ? q : (QUEUES.find(([k]) => h[k]?.length)?.[0] ?? q)));
      })
      .catch(() => !cancelled && setHistory(null));
    return () => {
      cancelled = true;
    };
  }, [puuid, reloadKey]);

  const available = QUEUES.filter(([k]) => history?.[k]?.length);
  if (!history || available.length === 0) return null;
  const points = history[queue] ?? [];

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
      {points.length < 2 ? (
        <p className="muted">
          Tracking since {new Date(points[0].fetchedAt).toLocaleString()} at {rankText(points[0])}. The graph fills in
          as their LP changes (checked whenever the profile updates).
        </p>
      ) : (
        <LPChart points={points} />
      )}
    </div>
  );
}
