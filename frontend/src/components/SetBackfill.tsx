import { useEffect, useRef, useState } from "react";
import { ApiError, BackfillStatus, getBackfill, staleSuffix, startBackfill } from "../api/client";

const POLL_MS = 3000;
// Reload stats every this many newly fetched games while loading.
const RELOAD_EVERY = 20;

/**
 * "Load whole set": asks the server to fetch every game of the player's
 * current set from Riot, then shows progress and calls onGames as games
 * arrive (so stats can reload).
 */
export function SetBackfill({ puuid, region, onGames }: { puuid: string; region: string; onGames: () => void }) {
  const [status, setStatus] = useState<BackfillStatus | null>(null);
  // Why the server refused to start a load (busy, cooldown, unknown player).
  const [refused, setRefused] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const reported = useRef(0);

  function track(st: BackfillStatus) {
    setStatus(st);
    if (st.fetched - reported.current >= RELOAD_EVERY || (st.state !== "running" && st.fetched > reported.current)) {
      reported.current = st.fetched;
      onGames();
    }
    if (st.state === "running") {
      timer.current = setTimeout(
        () =>
          getBackfill(puuid)
            .then(track)
            .catch(() => {}),
        POLL_MS,
      );
    }
  }

  useEffect(() => {
    reported.current = 0;
    getBackfill(puuid)
      .then((st) => {
        reported.current = st.fetched;
        track(st);
      })
      .catch(() => {});
    return () => clearTimeout(timer.current);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [puuid]);

  function start() {
    setRefused(null);
    startBackfill(puuid, region)
      .then(track)
      .catch((e: unknown) => {
        if (e instanceof ApiError && (e.status === 409 || e.status === 404)) {
          setRefused(e.message.charAt(0).toUpperCase() + e.message.slice(1) + ".");
        } else {
          setRefused("Couldn't start loading the set.");
        }
      });
  }

  if (status?.state === "running") {
    const total = status.missing || 1;
    return (
      <div className="backfill">
        <span className="muted">
          {status.found === 0
            ? "Listing this set's games…"
            : `Loading this set's games from Riot: ${status.fetched} of ${status.missing} new (${status.found} in the set)`}
        </span>
        <progress value={status.fetched} max={total} />
      </div>
    );
  }
  return (
    <div className="backfill">
      {status?.state === "done" && (
        <span className="muted">
          Whole set loaded: {status.found} games{status.found >= 500 ? " (the 500 most recent)" : ""}, {status.missing}{" "}
          new.
        </span>
      )}
      {status?.state === "failed" && (
        <span className="bad">
          Couldn't finish loading the set{staleSuffix(status.error)}
          {status.fetched > 0 ? ` (${status.fetched} games loaded)` : ""}.
        </span>
      )}
      {refused && <span className="muted">{refused}</span>}
      {status?.state !== "done" && (
        <button type="button" className="show-more" onClick={start} title="Fetch every game of this set from Riot">
          Load whole set
        </button>
      )}
    </div>
  );
}
