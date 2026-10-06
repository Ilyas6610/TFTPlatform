import { useEffect, useState } from "react";
import { RankPoint, getRankHistory } from "../api/client";
import { LPChart } from "./LPChart";

/**
 * A compact LP-over-time chart for one ranked queue, from recorded rank
 * snapshots; sits in the profile header under the rank badges. reloadKey
 * changes when a sync may have recorded a new snapshot.
 */
export function LPHistory({ puuid, queue, reloadKey }: { puuid: string; queue: string; reloadKey: string }) {
  const [history, setHistory] = useState<Record<string, RankPoint[]> | null>(null);

  useEffect(() => {
    let cancelled = false;
    getRankHistory(puuid)
      .then((h) => !cancelled && setHistory(h))
      .catch(() => !cancelled && setHistory(null));
    return () => {
      cancelled = true;
    };
  }, [puuid, reloadKey]);

  const points = history?.[queue] ?? [];
  if (points.length === 0) return null;
  return (
    <div className="lp-mini">
      <LPChart points={points} />
    </div>
  );
}
