"use client";

import type { PricePoint } from "@/lib/useLiveMarket";

const W = 1000;
const H = 260;
const PAD_Y = 22;

export function PriceChart({
  data,
  up,
}: {
  data: PricePoint[];
  up: boolean;
}) {
  if (data.length < 2) return <div className="h-[260px]" />;

  const prices = data.map((d) => d.price);
  let lo = Math.min(...prices);
  let hi = Math.max(...prices);
  if (hi - lo < 6) {
    const mid = (hi + lo) / 2;
    lo = mid - 3;
    hi = mid + 3;
  }
  const x = (i: number) => (i / (data.length - 1)) * W;
  const y = (p: number) =>
    H - PAD_Y - ((p - lo) / (hi - lo)) * (H - PAD_Y * 2);

  const line = data.map((d, i) => `${x(i).toFixed(2)},${y(d.price).toFixed(2)}`).join(" ");
  const stroke = up ? "#34d399" : "#f2637e";
  const last = data[data.length - 1];
  const lx = x(data.length - 1);
  const ly = y(last.price);
  const baseY = y(data[0].price);

  return (
    <div className="relative w-full select-none">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="h-[190px] w-full sm:h-[240px]"
        role="img"
        aria-label={`YES price history, currently ${last.price} cents`}
      >
        {/* reference baseline at the window's opening price */}
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
        {/* current marker */}
        <circle cx={lx} cy={ly} r="8" fill={stroke} opacity="0.16" />
        <circle
          cx={lx}
          cy={ly}
          r="3"
          fill={stroke}
          vectorEffect="non-scaling-stroke"
        />
      </svg>

      {/* faint hi/lo guides */}
      <div className="pointer-events-none absolute right-0 top-0 font-mono text-[10px] text-dim tnum">
        {hi}¢
      </div>
      <div className="pointer-events-none absolute bottom-0 right-0 font-mono text-[10px] text-dim tnum">
        {lo}¢
      </div>
    </div>
  );
}
