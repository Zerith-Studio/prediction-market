import { useCallback, useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import { api, ApiError, configured, mapBook, mapMatch, wsUrl, type WireBook } from "./api";
import type { Book, Fill, Market, Match } from "./types";

export interface PricePoint {
  t: number; // unix ms
  price: number; // yes price, cents
}

export interface LiveMarket {
  loading: boolean;
  errorStatus: number | null;
  market: Market | null;
  match: Match | null;
  book: Book | null;
  fills: Fill[];
  history: PricePoint[];
  oneliners: string[];
  onelinerIdx: number;
  yesPrice: number;
  priceDelta: number;
  lastFillId: number;
  lastFillSide: "up" | "down";
  balanceMicro: number;
  refreshBalance: () => void;
}

function midOf(book: Book, fallback: number): number {
  return book.bids[0] && book.asks[0]
    ? Math.round((book.bids[0].price + book.asks[0].price) / 2)
    : book.bids[0]?.price ?? book.asks[0]?.price ?? fallback;
}

/**
 * Loads a market and keeps it live from the exchange's /ws stream
 * (book_update, fill, oneliner, match_state). Real data only — no simulation.
 */
export function useLiveMarket(marketId: string, wallet: string | null = null): LiveMarket {
  const [state, setState] = useState<Omit<LiveMarket, "refreshBalance">>({
    loading: true,
    errorStatus: null,
    market: null,
    match: null,
    book: null,
    fills: [],
    history: [],
    oneliners: [],
    onelinerIdx: 0,
    yesPrice: 50,
    priceDelta: 0,
    lastFillId: 0,
    lastFillSide: "up",
    balanceMicro: 0,
  });
  const fillCounter = useRef(0);
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    const sub = AppState.addEventListener("change", (s) => {
      if (s === "active") setRefreshKey((k) => k + 1);
    });
    return () => sub.remove();
  }, []);

  const refreshBalance = useCallback(() => {
    api
      .getBalance(wallet)
      .then((balanceMicro) => setState((s) => ({ ...s, balanceMicro })))
      .catch(() => {});
  }, [wallet]);

  // initial load
  useEffect(() => {
    if (!configured()) {
      setState((s) => ({ ...s, loading: false, errorStatus: 0 }));
      return;
    }
    let alive = true;
    (async () => {
      try {
        const market = await api.getMarket(marketId);
        const [match, book, fills, oneliners, balanceMicro] = await Promise.all([
          api.getMatch(market.match_id),
          api.getBook(marketId),
          api.getFills(marketId),
          api.getOneliners(marketId),
          api.getBalance(wallet),
        ]);
        if (!alive) return;
        const mid = midOf(book, 50);
        setState((s) => ({
          ...s,
          loading: false,
          market,
          match,
          book,
          fills,
          oneliners,
          history: [
            { t: Date.now() - 1000, price: mid },
            { t: Date.now(), price: mid },
          ],
          yesPrice: mid,
          priceDelta: 0,
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
  }, [marketId, wallet, refreshKey]);

  // live WS stream
  useEffect(() => {
    if (!configured() || state.loading || state.errorStatus) return;
    let closed = false;
    let ws: WebSocket | null = null;
    let retry = 0;

    const handle = (ev: { type: string; market_id?: string; data: unknown }) => {
      switch (ev.type) {
        case "book_update": {
          if (ev.market_id !== marketId) return;
          const book = mapBook(ev.data as WireBook);
          setState((s) => {
            const mid = midOf(book, s.yesPrice);
            const history =
              mid !== s.history[s.history.length - 1]?.price
                ? [...s.history, { t: Date.now(), price: mid }].slice(-120)
                : s.history;
            return {
              ...s,
              book,
              yesPrice: mid,
              history,
              priceDelta: mid - (history[0]?.price ?? mid),
            };
          });
          break;
        }
        case "fill": {
          if (ev.market_id !== marketId) return;
          const d = ev.data as {
            taker_hash?: string;
            maker_hash?: string;
            price?: number;
            size?: number;
            match_type?: number;
          };
          if (!d.taker_hash) return; // settle-confirmation variant
          fillCounter.current += 1;
          const fill: Fill = {
            taker_hash: d.taker_hash,
            maker_hash: d.maker_hash ?? "",
            price: d.price ?? 0,
            size: d.size ?? 0,
            match_type: (["NORMAL", "MINT", "MERGE"] as const)[d.match_type ?? 0],
            ts: Date.now(),
          };
          setState((s) => ({
            ...s,
            fills: [fill, ...s.fills].slice(0, 12),
            lastFillId: fillCounter.current,
            lastFillSide: fill.price >= s.yesPrice ? "up" : "down",
          }));
          break;
        }
        case "oneliner": {
          if (ev.market_id !== marketId) return;
          const lines = (ev.data as { lines?: string[] }).lines ?? [];
          if (lines.length) setState((s) => ({ ...s, oneliners: lines, onelinerIdx: 0 }));
          break;
        }
        case "match_state": {
          const d = ev.data as {
            event?: string;
            payload?: { minute?: number; home_goals?: number; away_goals?: number };
          };
          setState((s) => {
            if (!s.match) return s;
            const finished = d.event === "full_time";
            return {
              ...s,
              match: mapMatch({
                id: s.match.id,
                fixture_id: s.match.fixture_id,
                home: s.match.home,
                away: s.match.away,
                kickoff_at: s.match.kickoff_at,
                status: finished ? "finished" : "live",
                live_state: d.payload ?? {},
              }),
            };
          });
          break;
        }
      }
    };

    const connect = () => {
      ws = new WebSocket(wsUrl());
      ws.onopen = () => {
        retry = 0;
      };
      ws.onmessage = (e) => {
        try {
          handle(JSON.parse(e.data as string));
        } catch {
          /* malformed frame — skip */
        }
      };
      ws.onclose = () => {
        if (!closed) setTimeout(connect, Math.min(8000, 1000 * 2 ** retry++));
      };
    };
    connect();
    return () => {
      closed = true;
      ws?.close();
    };
  }, [marketId, state.loading, state.errorStatus, refreshKey]);

  // one-liner rotation
  useEffect(() => {
    if (state.loading || state.errorStatus) return;
    const t = setInterval(() => {
      setState((s) =>
        s.oneliners.length
          ? { ...s, onelinerIdx: (s.onelinerIdx + 1) % s.oneliners.length }
          : s
      );
    }, 6500);
    return () => clearInterval(t);
  }, [state.loading, state.errorStatus]);

  return { ...state, refreshBalance };
}
