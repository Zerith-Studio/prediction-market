"use client";

import type { Book, BookLevel } from "@/lib/types";
import { shares } from "@/lib/format";

interface Row {
  price: number;
  size: number;
  total: number;
  depth: number; // 0..1
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
  flashSide: "yes" | "no";
}) {
  const bids = book.bids.slice(0, 4);
  const asks = book.asks.slice(0, 4);
  const maxTotal = Math.max(
    bids.reduce((a, b) => a + b.size, 0),
    asks.reduce((a, b) => a + b.size, 0)
  );
  const bidRows = withTotals(bids, maxTotal);
  // asks rendered best-price-nearest-the-spread => reverse for display (high on top)
  const askRows = withTotals(asks, maxTotal).reverse();

  const bestBid = bids[0]?.price ?? 0;
  const bestAsk = asks[0]?.price ?? 0;
  const spread = bestAsk && bestBid ? bestAsk - bestBid : 0;
  const mid = bestAsk && bestBid ? Math.round((bestAsk + bestBid) / 2) : 0;

  if (bids.length === 0 && asks.length === 0) {
    return (
      <div className="panel p-6">
        <BookHeader />
        <p className="py-10 text-center text-[13px] leading-relaxed text-muted">
          No resting orders yet.
          <br />
          <span className="text-dim">
            A signed limit order rests here until someone crosses it.
          </span>
        </p>
      </div>
    );
  }

  return (
    <div className="panel p-[18px] sm:p-5">
      <BookHeader />
      <div className="font-mono text-[12.5px]">
        <div className="grid grid-cols-3 px-1 pb-2 text-[10.5px] uppercase tracking-[0.08em] text-dim">
          <span>Price</span>
          <span className="text-right">Size</span>
          <span className="text-right">Total</span>
        </div>

        {askRows.map((r, i) => {
          const isBest = i === askRows.length - 1;
          return (
            <BookRow
              key={`ask-${r.price}`}
              row={r}
              side="ask"
              flash={isBest && flashSide === "no" ? flashId : 0}
            />
          );
        })}

        <div className="my-1.5 flex items-center justify-center gap-2 border-y border-dashed border-line py-2 text-[11px] tracking-[0.08em] text-dim tnum">
          SPREAD {spread}¢ · MID {mid}¢
        </div>

        {bidRows.map((r, i) => (
          <BookRow
            key={`bid-${r.price}`}
            row={r}
            side="bid"
            flash={i === 0 && flashSide === "yes" ? flashId : 0}
          />
        ))}
      </div>
    </div>
  );
}

function BookHeader() {
  return (
    <div className="mb-3.5 flex items-center justify-between">
      <h3 className="text-[13px] font-bold">Order book</h3>
      <span className="font-mono text-[11px] text-dim">YES side</span>
    </div>
  );
}

function BookRow({
  row,
  side,
  flash,
}: {
  row: Row;
  side: "bid" | "ask";
  flash: number;
}) {
  const isBid = side === "bid";
  return (
    <div
      // remount on flash id change to replay the fill-flash keyframe
      key={flash || undefined}
      className={`relative z-[1] grid grid-cols-3 rounded px-1 py-[5px] ${
        flash ? (isBid ? "animate-flash-yes" : "animate-flash-no") : ""
      }`}
    >
      <span
        className={`absolute inset-y-0 -z-[1] rounded ${
          isBid ? "left-0 bg-yes/[0.16]" : "right-0 bg-no/[0.16]"
        }`}
        style={{ width: `${Math.max(4, row.depth * 100)}%` }}
        aria-hidden
      />
      <span className={`font-bold ${isBid ? "text-yes" : "text-no"} tnum`}>
        {row.price}¢
      </span>
      <span className="text-right text-muted tnum">{shares(row.size)}</span>
      <span className="text-right text-muted tnum">{shares(row.total)}</span>
    </div>
  );
}
