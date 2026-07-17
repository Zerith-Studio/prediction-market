// YES-price history: recent fills (server) seeded first, then live WS mids
// appended by useLiveMarket — same data the web app's chart draws from.
import { useState } from "react";
import { Text, View } from "react-native";
import Svg, { Line, Polyline } from "react-native-svg";
import type { Fill } from "@/lib/types";
import type { PricePoint } from "@/lib/useLiveMarket";

const H = 110;
const ACCENT = "#34d399";
const DOWN = "#f2637e";
const DIM = "#565b63";

export function PriceChart({
  fills,
  history,
  up,
}: {
  fills: Fill[];
  history: PricePoint[];
  up: boolean;
}) {
  const [w, setW] = useState(0);

  // Fills arrive newest-first; chart wants chronological, live mids on top.
  const pts: PricePoint[] = [
    ...[...fills].sort((a, b) => a.ts - b.ts).map((f) => ({ t: f.ts, price: f.price })),
    ...history,
  ];

  if (pts.length < 2) {
    return (
      <View className="mx-4 my-3 h-[110px] items-center justify-center border border-line">
        <Text className="text-dim text-[11px]">Chart appears after the first fills.</Text>
      </View>
    );
  }

  const lo = Math.max(0, Math.min(...pts.map((p) => p.price)) - 3);
  const hi = Math.min(100, Math.max(...pts.map((p) => p.price)) + 3);
  const span = Math.max(hi - lo, 1);
  const y = (price: number) => H - ((price - lo) / span) * H;
  const points = pts
    .map((p, i) => `${((i / (pts.length - 1)) * w).toFixed(1)},${y(p.price).toFixed(1)}`)
    .join(" ");
  const last = pts[pts.length - 1].price;

  return (
    <View
      className="px-4 py-3"
      onLayout={(e) => setW(Math.max(0, e.nativeEvent.layout.width - 32))}
    >
      {w > 0 && (
        <Svg width={w} height={H}>
          <Line
            x1={0}
            y1={y(last)}
            x2={w}
            y2={y(last)}
            stroke={DIM}
            strokeWidth={1}
            strokeDasharray="3,3"
          />
          <Polyline
            points={points}
            fill="none"
            stroke={up ? ACCENT : DOWN}
            strokeWidth={1.5}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        </Svg>
      )}
    </View>
  );
}
