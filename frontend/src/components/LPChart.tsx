import { useEffect, useMemo, useRef, useState } from "react";
import { RankPoint } from "../api/client";

const H = 220;

const TIERS = ["IRON", "BRONZE", "SILVER", "GOLD", "PLATINUM", "EMERALD", "DIAMOND"];
const DIVISIONS = ["IV", "III", "II", "I"];
const APEX = new Set(["MASTER", "GRANDMASTER", "CHALLENGER"]);

const title = (tier: string) => tier.charAt(0) + tier.slice(1).toLowerCase();

/** "Diamond II 40 LP" / "Challenger 667 LP" */
export function rankText(p: { tier: string; rank: string; leaguePoints: number }) {
  return `${title(p.tier)}${APEX.has(p.tier) ? "" : ` ${p.rank}`} ${p.leaguePoints} LP`;
}

/** The division boundary a linear LP value sits on (multiples of 100 below Master). */
function boundaryLabel(value: number): string | null {
  if (value === 2800) return "Master";
  if (value > 2800 || value % 100 !== 0) return null;
  const tier = TIERS[Math.floor(value / 400)];
  const div = DIVISIONS[(value % 400) / 100];
  return tier ? `${title(tier)} ${div}` : null;
}

const fmtDate = (d: Date) => d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
const fmtDateTime = (d: Date) =>
  d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

/**
 * LP over time for one ranked queue, as a step line: LP holds between rank
 * snapshots (changes in between aren't seen), so steps are honest where a
 * sloped line would invent a gradual climb. Hover shows the snapshot in
 * effect at that moment.
 */
export function LPChart({ points }: { points: RankPoint[] }) {
  // Laid out at the container's real width, so text stays its CSS size on
  // phones instead of shrinking with a scaled viewBox.
  const box = useRef<HTMLDivElement>(null);
  const [W, setW] = useState(720);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.max(280, Math.round(e.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const PAD = { top: 12, right: 16, bottom: 26, left: W < 500 ? 64 : 92 };
  const [hover, setHover] = useState<number | null>(null);

  const geo = useMemo(() => {
    const times = points.map((p) => new Date(p.fetchedAt).getTime());
    const t0 = times[0];
    const t1 = Math.max(Date.now(), times[times.length - 1] + 1);
    const values = points.map((p) => p.value);
    let lo = Math.min(...values);
    let hi = Math.max(...values);
    const span = Math.max(hi - lo, 60);
    lo = Math.floor((lo - span * 0.15) / 10) * 10;
    hi = Math.ceil((hi + span * 0.15) / 10) * 10;
    const x = (t: number) => PAD.left + ((t - t0) / (t1 - t0)) * (W - PAD.left - PAD.right);
    const y = (v: number) => PAD.top + (1 - (v - lo) / (hi - lo)) * (H - PAD.top - PAD.bottom);

    // Step-after path, extended to now (the last rank still holds).
    let d = `M${x(times[0])},${y(values[0])}`;
    for (let i = 1; i < points.length; i++) d += `H${x(times[i])}V${y(values[i])}`;
    d += `H${x(t1)}`;

    // Gridlines: division boundaries in range, else a few round LP values.
    let grid: { v: number; label: string }[] = [];
    for (let v = Math.ceil(lo / 100) * 100; v <= hi; v += 100) {
      const label = boundaryLabel(v);
      if (label) grid.push({ v, label });
    }
    if (grid.length < 2) {
      const step = Math.max(10, Math.ceil((hi - lo) / 4 / 10) * 10);
      grid = [];
      for (let v = Math.ceil(lo / step) * step; v <= hi; v += step) {
        grid.push({ v, label: v >= 2800 ? `${v - 2800} LP` : (boundaryLabel(v) ?? `${v % 100} LP`) });
      }
    }
    return { times, t0, t1, x, y, d, grid };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [points, W]);

  function onMove(e: React.MouseEvent<SVGRectElement>) {
    const box = (e.currentTarget.ownerSVGElement as SVGSVGElement).getBoundingClientRect();
    const t =
      geo.t0 + ((((e.clientX - box.left) / box.width) * W - PAD.left) / (W - PAD.left - PAD.right)) * (geo.t1 - geo.t0);
    // The snapshot in effect at t: the last one at or before it.
    let i = 0;
    while (i + 1 < geo.times.length && geo.times[i + 1] <= t) i++;
    setHover(i);
  }

  const h = hover === null ? null : points[hover];
  const prev = hover !== null && hover > 0 ? points[hover - 1] : null;
  const hx = h ? geo.x(geo.times[hover!]) : 0;

  return (
    <div className="lp-chart" ref={box}>
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label="LP over time">
        {geo.grid.map((g) => (
          <g key={g.v}>
            <line className="lp-grid" x1={PAD.left} x2={W - PAD.right} y1={geo.y(g.v)} y2={geo.y(g.v)} />
            <text className="lp-axis" x={PAD.left - 8} y={geo.y(g.v)} textAnchor="end" dominantBaseline="middle">
              {g.label}
            </text>
          </g>
        ))}
        <text className="lp-axis" x={PAD.left} y={H - 6}>
          {fmtDate(new Date(geo.t0))}
        </text>
        <text className="lp-axis" x={W - PAD.right} y={H - 6} textAnchor="end">
          now
        </text>
        <path className="lp-line" d={geo.d} />
        {points.length <= 40 &&
          points.map((p, i) => (
            <circle key={i} className="lp-dot" cx={geo.x(geo.times[i])} cy={geo.y(p.value)} r={3} />
          ))}
        {h && (
          <>
            <line className="lp-crosshair" x1={hx} x2={hx} y1={PAD.top} y2={H - PAD.bottom} />
            <circle className="lp-dot lp-dot-active" cx={hx} cy={geo.y(h.value)} r={5} />
          </>
        )}
        <rect
          x={PAD.left}
          y={PAD.top}
          width={W - PAD.left - PAD.right}
          height={H - PAD.top - PAD.bottom}
          fill="transparent"
          onMouseMove={onMove}
          onMouseLeave={() => setHover(null)}
        />
      </svg>
      {h && (
        <div className="lp-tooltip" style={{ left: `${Math.min(80, Math.max(20, (hx / W) * 100))}%` }}>
          <strong>{rankText(h)}</strong>
          <span className="muted">{fmtDateTime(new Date(h.fetchedAt))}</span>
          {prev && (
            <span className={h.value >= prev.value ? "good" : "bad"}>
              {h.value >= prev.value ? "+" : ""}
              {h.value - prev.value} LP over {h.wins + h.losses - (prev.wins + prev.losses)}{" "}
              {h.wins + h.losses - (prev.wins + prev.losses) === 1 ? "game" : "games"}
            </span>
          )}
        </div>
      )}
      <details className="lp-table">
        <summary className="muted">Show as table</summary>
        <table className="compact">
          <tbody>
            {[...points].reverse().map((p, i) => (
              <tr key={i}>
                <td className="muted">{fmtDateTime(new Date(p.fetchedAt))}</td>
                <td>{rankText(p)}</td>
                <td className="num muted">{p.wins + p.losses} games</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
    </div>
  );
}
