"use client";

import { useState } from "react";
import type { PricePoint } from "@/lib/useLiveMarket";

const W = 1000;
const H = 260;
const PAD_Y = 22;

export function PriceChart({ data, up }: { data: PricePoint[]; up: boolean }) {
  const [hover, setHover] = useState<number | null>(null);
  if (data.length < 2) return <div className="h-[190px] sm:h-[240px]" />;

  const prices = data.map((d) => d.price);
  let lo = Math.min(...prices);
  let hi = Math.max(...prices);
  if (hi - lo < 6) {
    const mid = (hi + lo) / 2;
    lo = mid - 3;
    hi = mid + 3;
  }
  const x = (i: number) => (i / (data.length - 1)) * W;
  const y = (p: number) => H - PAD_Y - ((p - lo) / (hi - lo)) * (H - PAD_Y * 2);

  const line = data.map((d, i) => `${x(i).toFixed(2)},${y(d.price).toFixed(2)}`).join(" ");
  const stroke = up ? "#34d399" : "#f2637e";
  const last = data.length - 1;
  const baseY = y(data[0].price);

  const active = hover ?? last;
  const ax = x(active);
  const ay = y(data[active].price);
  const frac = active / (data.length - 1);
  const minsAgo = Math.round((data[last].t - data[active].t) / 60000);

  function onMove(e: React.MouseEvent<HTMLDivElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const f = (e.clientX - rect.left) / rect.width;
    setHover(Math.max(0, Math.min(data.length - 1, Math.round(f * (data.length - 1)))));
  }

  return (
    <div
      className="relative w-full select-none"
      onMouseMove={onMove}
      onMouseLeave={() => setHover(null)}
    >
      {/* hover readout */}
      {hover !== null && (
        <div
          className="pointer-events-none absolute -top-1 z-10 -translate-x-1/2 whitespace-nowrap font-mono text-[11px] tnum"
          style={{ left: `${frac * 100}%` }}
        >
          <span className="text-ink">{data[active].price}¢</span>
          <span className="ml-2 text-dim">{minsAgo === 0 ? "now" : `−${minsAgo}m`}</span>
        </div>
      )}

      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="h-[190px] w-full sm:h-[240px]"
        role="img"
        aria-label={`YES price history, currently ${data[last].price} cents`}
      >
        <line
          x1="0"
          x2={W}
          y1={baseY}
          y2={baseY}
          stroke="#565b63"
          strokeWidth="1"
          strokeDasharray="2 5"
          vectorEffect="non-scaling-stroke"
          opacity="0.5"
        />
        <polyline
          points={line}
          fill="none"
          stroke={stroke}
          strokeWidth="1.75"
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />

        {/* crosshair */}
        {hover !== null && (
          <line
            x1={ax}
            x2={ax}
            y1="0"
            y2={H}
            stroke="#565b63"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
            opacity="0.7"
          />
        )}

        <circle cx={ax} cy={ay} r="8" fill={stroke} opacity="0.16" />
        <circle cx={ax} cy={ay} r="3" fill={stroke} vectorEffect="non-scaling-stroke" />
      </svg>

      <div className="pointer-events-none absolute right-0 top-0 font-mono text-[10px] text-dim tnum">
        {hi}¢
      </div>
      <div className="pointer-events-none absolute bottom-0 right-0 font-mono text-[10px] text-dim tnum">
        {lo}¢
      </div>
    </div>
  );
}
