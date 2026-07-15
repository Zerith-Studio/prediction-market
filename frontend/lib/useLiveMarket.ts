"use client";

import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "./api";
import type { Book, Fill, Market, Match } from "./types";

export interface LiveMarket {
  loading: boolean;
  errorStatus: number | null;
  market: Market | null;
  match: Match | null;
  book: Book | null;
  fills: Fill[];
  oneliners: string[];
  onelinerIdx: number;
  yesPrice: number; // cents, mid/last
  priceDelta: number; // signed cents over last tick
  lastFillId: number; // increments on each new fill (drives flash)
  lastFillSide: "yes" | "no";
  balanceMicro: number;
}

const MIN = 55;
const MAX = 72;
const clamp = (n: number) => Math.max(MIN, Math.min(MAX, n));

/**
 * Loads a market and simulates the WS stream (book_update + fill) so the page is
 * alive without a backend. When NEXT_PUBLIC_API_URL is set, api.ts hits the real
 * server; wiring a real /ws socket in place of this timer is the only change.
 */
export function useLiveMarket(marketId: string): LiveMarket {
  const [state, setState] = useState<LiveMarket>({
    loading: true,
    errorStatus: null,
    market: null,
    match: null,
    book: null,
    fills: [],
    oneliners: [],
    onelinerIdx: 0,
    yesPrice: 64,
    priceDelta: 0,
    lastFillId: 0,
    lastFillSide: "yes",
    balanceMicro: 0,
  });
  const fillCounter = useRef(0);

  // initial load
  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const market = await api.getMarket(marketId);
        const [match, book, fills, oneliners, balanceMicro] = await Promise.all([
          api.getMatch(market.match_id),
          api.getBook(marketId),
          api.getFills(marketId),
          api.getOneliners(marketId),
          api.getBalance("demo"),
        ]);
        if (!alive) return;
        const mid = book.bids[0] && book.asks[0]
          ? Math.round((book.bids[0].price + book.asks[0].price) / 2)
          : 64;
        setState((s) => ({
          ...s,
          loading: false,
          market,
          match,
          book,
          fills,
          oneliners,
          yesPrice: mid,
          balanceMicro,
        }));
      } catch (e) {
        if (!alive) return;
        const status = e instanceof ApiError ? e.status : 500;
        setState((s) => ({ ...s, loading: false, errorStatus: status }));
      }
    })();
    return () => {
      alive = false;
    };
  }, [marketId]);

  // simulated live fills + book/odds drift
  useEffect(() => {
    if (state.loading || state.errorStatus) return;
    const fillTimer = setInterval(() => {
      setState((s) => {
        if (!s.book) return s;
        const drift = Math.round((Math.random() - 0.45) * 3);
        const nextPrice = clamp(s.yesPrice + drift);
        const side: "yes" | "no" = drift >= 0 ? "yes" : "no";
        const size = 40 + Math.floor(Math.random() * 480);
        fillCounter.current += 1;
        const fill: Fill = {
          taker_hash: randHash(),
          maker_hash: randHash(),
          price: nextPrice,
          size,
          match_type: Math.random() > 0.7 ? "MINT" : "NORMAL",
          ts: Date.now(),
        };
        // nudge the book toward the new price so depth visibly breathes
        const book = jitterBook(s.book, nextPrice);
        return {
          ...s,
          book,
          fills: [fill, ...s.fills].slice(0, 12),
          yesPrice: nextPrice,
          priceDelta: nextPrice - s.yesPrice,
          lastFillId: fillCounter.current,
          lastFillSide: side,
        };
      });
    }, 3200);

    const linerTimer = setInterval(() => {
      setState((s) =>
        s.oneliners.length
          ? { ...s, onelinerIdx: (s.onelinerIdx + 1) % s.oneliners.length }
          : s
      );
    }, 6000);

    return () => {
      clearInterval(fillTimer);
      clearInterval(linerTimer);
    };
  }, [state.loading, state.errorStatus]);

  return state;
}

function jitterBook(book: Book, mid: number): Book {
  const j = (lv: { price: number; size: number }) => ({
    price: lv.price,
    size: Math.max(60, lv.size + Math.round((Math.random() - 0.5) * 120)),
  });
  return {
    asks: book.asks.map(j).sort((a, b) => a.price - b.price),
    bids: book.bids.map(j).sort((a, b) => b.price - a.price),
  };
}

function randHash(): string {
  return Array.from({ length: 12 }, () =>
    "0123456789abcdef"[Math.floor(Math.random() * 16)]
  ).join("");
}
