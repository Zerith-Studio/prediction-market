"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, configured, mapBook, wsUrl, type WireBook } from "./api";
import { applyMatchState } from "./matchState";
import type { Book, Fill, Lineups, Market, Match, TeamMatchStats } from "./types";

// The score-tick shape carried by a match_state WS event's payload (mirrors the
// backend scorePayload); a "lineup" event instead carries a Lineups object.
type ScoreTick = {
  minute?: number;
  period?: string;
  home_goals?: number;
  away_goals?: number;
  possession?: { home: number; away: number };
  stats?: { home: TeamMatchStats; away: TeamMatchStats };
};

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
 * Reconstructs the initial chart series from the market's real fills so the
 * graph survives a reload instead of collapsing to a flat line at the current
 * price. Each fill is a YES-cents price at a timestamp; the backend returns
 * them newest-first, but the chart reads left→right (oldest→newest) and pins
 * the live mid as the trailing "now" point. With no fills we fall back to a
 * flat two-point line at the mid (the chart needs ≥2 points to render).
 */
function seedHistory(fills: Fill[], mid: number): PricePoint[] {
  const pts = fills
    .map((f) => ({ t: f.ts, price: f.price }))
    .sort((a, b) => a.t - b.t);
  pts.push({ t: Date.now(), price: mid });
  if (pts.length < 2) {
    return [
      { t: Date.now() - 60_000, price: mid },
      { t: Date.now(), price: mid },
    ];
  }
  return pts.slice(-120);
}

// The backend has no mid-price time-series — /fills only records trades, not
// the resting-order book moves that also shift the YES mid (and drive the live
// chart). So we persist the series the user is actually watching, keyed per
// market, and restore it on reload. Fills still seed a first-ever visit.
const HISTORY_CAP = 120;
const historyKey = (marketId: string) => `pm:chart:${marketId}`;

function loadStoredHistory(marketId: string): PricePoint[] | null {
  try {
    const raw = window.localStorage.getItem(historyKey(marketId));
    if (!raw) return null;
    const pts = JSON.parse(raw);
    if (!Array.isArray(pts)) return null;
    const clean = pts.filter(
      (p): p is PricePoint =>
        p && typeof p.t === "number" && typeof p.price === "number"
    );
    return clean.length ? clean : null;
  } catch {
    return null;
  }
}

function saveStoredHistory(marketId: string, pts: PricePoint[]): void {
  try {
    window.localStorage.setItem(
      historyKey(marketId),
      JSON.stringify(pts.slice(-HISTORY_CAP))
    );
  } catch {
    /* private mode / quota exceeded — the chart still works in-memory */
  }
}

/**
 * Restores the chart series on load: the user's own persisted series wins (it
 * captures book-driven mid moves, not just trades), pinned to the current mid
 * as the trailing point; otherwise we reconstruct from real fills.
 */
function restoreHistory(marketId: string, fills: Fill[], mid: number): PricePoint[] {
  const stored = loadStoredHistory(marketId);
  if (stored && stored.length >= 2) {
    const last = stored[stored.length - 1];
    const pts = last.price === mid ? stored : [...stored, { t: Date.now(), price: mid }];
    return pts.slice(-HISTORY_CAP);
  }
  return seedHistory(fills, mid);
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
        const subject = market.subject_id ? titleCase(market.subject_id) : "";
        const syntheticMatch: Match = {
          id: "",
          fixture_id: market.competition_id ? `competition-${market.competition_id}` : "global",
          home: subject || market.title,
          away: market.scope === "player" ? "World Cup field" : "World Cup",
          kickoff_at: new Date().toISOString(),
          status: "scheduled",
          live_state: {},
          lineups: null,
        };
        const [match, book, fills, oneliners, balanceMicro] = await Promise.all([
          market.match_id ? api.getMatch(market.match_id) : Promise.resolve(syntheticMatch),
          api.getBook(marketId),
          api.getFills(marketId),
          api.getOneliners(marketId),
          api.getBalance(wallet),
        ]);
        if (!alive) return;
        const mid = midOf(book, 50);
        const history = restoreHistory(marketId, fills, mid);
        setState((s) => ({
          ...s,
          loading: false,
          market,
          match,
          book,
          fills,
          oneliners,
          history,
          yesPrice: mid,
          priceDelta: mid - history[0].price,
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
  }, [marketId, wallet]);

  // live WS stream
  useEffect(() => {
    if (!configured() || state.loading || state.errorStatus) return;
    let closed = false;
    let ws: WebSocket | null = null;
    let retry = 0;

    const handle = (ev: {
      type: string;
      market_id?: string;
      fixture_id?: string;
      data: unknown;
    }) => {
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
          setState((s) => {
            if (!s.match) return s;
            // applyMatchState enforces the fixture guard and only lets real
            // lifecycle transitions (kickoff/full_time) change status — so a
            // stray odds/score tick can never fabricate a false "LIVE".
            const next = applyMatchState(s.match, {
              fixture_id: ev.fixture_id,
              data: ev.data as { event?: string; payload?: ScoreTick | Lineups },
            });
            return next === s.match ? s : { ...s, match: next };
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
  }, [marketId, state.loading, state.errorStatus]);

  // Re-poll the authoritative match status/state on an interval so the header
  // reflects reality on its own — scheduled → live → finished — even when no
  // WS tick lands for this fixture while the page is open. The WS stream drives
  // instant updates; this is the backstop that keeps status honest (and heals a
  // stale status the moment the backend corrects it). Only fixture-backed
  // markets have a match to refresh.
  const matchId = state.market?.match_id;
  useEffect(() => {
    if (!configured() || state.loading || state.errorStatus || !matchId) return;
    const t = setInterval(() => {
      api
        .getMatch(matchId)
        .then((m) => setState((s) => (s.match ? { ...s, match: m } : s)))
        .catch(() => {});
    }, 15_000);
    return () => clearInterval(t);
  }, [matchId, state.loading, state.errorStatus]);

  // persist the price series so a reload restores the graph the user was
  // watching (mid moves from resting orders never touch /fills, so this is the
  // only record of them client-side).
  useEffect(() => {
    if (state.loading || state.errorStatus) return;
    saveStoredHistory(marketId, state.history);
  }, [marketId, state.history, state.loading, state.errorStatus]);

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

function titleCase(s: string): string {
  return s
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
