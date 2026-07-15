"use client";

import type { Book, BookLevel } from "@/lib/types";
import { shares } from "@/lib/format";

interface Row {
  price: number;
  size: number;
  total: number;
  depth: number;
}

function withTotals(levels: BookLevel[], max: number): Row[] {
  let run = 0;
  return levels.map((l) => {
    run += l.size;
    return { price: l.price, size: l.size, total: run, depth: max ? run / max : 0 };
  });
}

export function OrderBook({
  book,
  flashId,
  flashSide,
}: {
  book: Book;
  flashId: number;
  flashSide: "up" | "down";
}) {
  const bids = book.bids.slice(0, 4);
  const asks = book.asks.slice(0, 4);
  const maxTotal = Math.max(
    bids.reduce((a, b) => a + b.size, 0),
    asks.reduce((a, b) => a + b.size, 0)
  );
  const bidRows = withTotals(bids, maxTotal);
  const askRows = withTotals(asks, maxTotal).reverse();

  const bestBid = bids[0]?.price ?? 0;
  const bestAsk = asks[0]?.price ?? 0;
  const spread = bestAsk && bestBid ? bestAsk - bestBid : 0;
  const mid = bestAsk && bestBid ? Math.round((bestAsk + bestBid) / 2) : 0;

  if (!bids.length && !asks.length) {
    return (
      <div>
        <Head />
        <p className="py-8 text-[13px] leading-relaxed text-muted">
          No resting orders yet.
          <br />
          <span className="text-dim">A signed limit order rests here until crossed.</span>
        </p>
      </div>
    );
  }

  return (
    <div>
      <Head />
      <div className="font-mono text-[12.5px]">
        <div className="grid grid-cols-3 pb-2.5 eyebrow">
          <span>Price</span>
          <span className="text-right">Size</span>
          <span className="text-right">Total</span>
        </div>
        {askRows.map((r, i) => (
          <BookRow
            key={`ask-${r.price}`}
            row={r}
            side="ask"
            flash={i === askRows.length - 1 && flashSide === "down" ? flashId : 0}
          />
        ))}
        <div className="my-1 flex items-center gap-3 py-1.5 text-[11px] text-dim tnum">
          <span className="tracking-[0.06em]">MID {mid}¢</span>
          <span className="h-px flex-1 bg-line" />
          <span className="tracking-[0.06em]">SPREAD {spread}¢</span>
        </div>
        {bidRows.map((r, i) => (
          <BookRow
            key={`bid-${r.price}`}
            row={r}
            side="bid"
            flash={i === 0 && flashSide === "up" ? flashId : 0}
          />
        ))}
      </div>
    </div>
  );
}

function Head() {
  return (
    <div className="mb-4 flex items-baseline justify-between">
      <h2 className="text-[13px] font-semibold text-ink">Order book</h2>
      <span className="font-mono text-[11px] text-dim">YES</span>
    </div>
  );
}

function BookRow({ row, side, flash }: { row: Row; side: "bid" | "ask"; flash: number }) {
  const isBid = side === "bid";
  return (
    <div
      key={flash || undefined}
      className={`relative grid grid-cols-3 py-[5px] ${
        flash ? (isBid ? "animate-flash-up" : "animate-flash-down") : ""
      }`}
    >
      <span
        className={`absolute inset-y-0 -z-0 ${isBid ? "left-0 bg-accent/[0.07]" : "right-0 bg-down/[0.07]"}`}
        style={{ width: `${Math.max(3, row.depth * 100)}%` }}
        aria-hidden
      />
      <span className={`relative z-10 ${isBid ? "text-accent" : "text-down"} tnum`}>
        {row.price}¢
      </span>
      <span className="relative z-10 text-right text-muted tnum">{shares(row.size)}</span>
      <span className="relative z-10 text-right text-dim tnum">{shares(row.total)}</span>
    </div>
  );
}
