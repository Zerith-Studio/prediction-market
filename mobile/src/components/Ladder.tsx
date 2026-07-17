import { Text, View } from "react-native";
import type { Book } from "@/lib/types";
import { cents, shares } from "@/lib/format";

const DEPTH = 5;

function Row({ price, size, max, side }: { price: number; size: number; max: number; side: "bid" | "ask" }) {
  const pct = max > 0 ? Math.max(4, (size / max) * 100) : 0;
  const color = side === "bid" ? "bg-accent/15" : "bg-down/15";
  const text = side === "bid" ? "text-accent" : "text-down";
  return (
    <View className="flex-row items-center h-7 px-4">
      <View className={`absolute right-4 top-1 bottom-1 ${color}`} style={{ width: `${pct * 0.5}%` }} />
      <Text className={`${text} font-mono text-[13px] w-14`}>{cents(price)}</Text>
      <Text className="text-muted font-mono text-[13px] ml-auto">{shares(size)}</Text>
    </View>
  );
}

export function Ladder({ book }: { book: Book | null }) {
  if (!book) return null;
  const asks = book.asks.slice(0, DEPTH).reverse();
  const bids = book.bids.slice(0, DEPTH);
  const max = Math.max(1, ...[...asks, ...bids].map((l) => l.size));
  const spread =
    book.bids[0] && book.asks[0] ? book.asks[0].price - book.bids[0].price : null;
  return (
    <View className="py-2">
      <View className="flex-row justify-between px-4 pb-1">
        <Text className="text-dim text-[10px] uppercase">Price (YES)</Text>
        <Text className="text-dim text-[10px] uppercase">Size</Text>
      </View>
      {asks.map((l) => (
        <Row key={`a${l.price}`} {...l} max={max} side="ask" />
      ))}
      <View className="flex-row justify-center py-1 border-y border-line my-1">
        <Text className="text-dim text-[11px]">
          {spread !== null ? `spread ${spread}¢` : "empty book"}
        </Text>
      </View>
      {bids.map((l) => (
        <Row key={`b${l.price}`} {...l} max={max} side="bid" />
      ))}
    </View>
  );
}
