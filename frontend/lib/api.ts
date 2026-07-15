import type {
  Book,
  BookLevel,
  Fill,
  Market,
  Match,
  Portfolio,
  Settlement,
  Side,
} from "./types";
import {
  DEMO_MARKET_ID,
  demoBalanceMicro,
  demoBook,
  demoFills,
  demoMarket,
  demoMatch,
  demoOneliners,
  demoPortfolio,
  demoSettlement,
} from "./fixtures";

export const explorerTx = (sig: string) =>
  `https://explorer.solana.com/tx/${sig}?cluster=devnet`;
export const explorerAddr = (addr: string) =>
  `https://explorer.solana.com/address/${addr}?cluster=devnet`;

export const PROGRAM_ID = "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs";
export const DEPLOY_TX =
  "5Ayf6cLmSpqFue5odVvTVSBQSPMyJjyV6ndhp9FPu6F46CYDSkJucuDyPTpKMQvbpfv4XzC33v4bnfnaj4xXgVqa";

// Typed client for the Go REST surface (backend/internal/api/api.go). When
// NEXT_PUBLIC_API_URL is set it hits the real backend and maps the Go wire
// shapes to the view types; otherwise it serves demo fixtures so the UI
// renders with zero infrastructure.

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "";
const LIVE = BASE.length > 0;

export function wsUrl(): string {
  return BASE.replace(/^http/, "ws") + "/ws";
}

async function get<T, W = T>(
  path: string,
  fallback: () => T,
  map?: (wire: W) => T
): Promise<T> {
  if (!LIVE) {
    // Simulate a tiny network delay so skeleton states are real, not theatre.
    await new Promise((r) => setTimeout(r, 240));
    return fallback();
  }
  const res = await fetch(`${BASE}${path}`, { cache: "no-store" });
  if (!res.ok) throw new ApiError(res.status, await safeText(res));
  const wire = (await res.json()) as W;
  return map ? map(wire) : (wire as unknown as T);
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new ApiError(res.status, await safeText(res));
  return (await res.json()) as T;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

async function safeText(res: Response): Promise<string> {
  try {
    const j = await res.json();
    return j.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}

// ---- wire → view mappings ---------------------------------------------------

interface WireMarket {
  id: string;
  market_id: string;
  match_id: string;
  template_key: string;
  type: "binary" | "precision";
  title: string;
  rule: string;
  status: Market["status"];
  outcome?: { result?: string; score?: string; actual?: number };
  chain_tx?: string;
}

function mapMarket(w: WireMarket): Market {
  return {
    id: w.id,
    market_id: w.market_id,
    match_id: w.match_id,
    template_key: w.template_key,
    type: w.type,
    title: w.title,
    rule: w.rule,
    status: w.status,
    outcome: w.outcome?.result
      ? { winner: w.outcome.result === "yes" ? "YES" : "NO" }
      : w.outcome?.actual !== undefined
        ? { value: w.outcome.actual }
        : null,
    chain_tx: w.chain_tx,
  };
}

interface WireMatch {
  id: string;
  fixture_id: string;
  home: string;
  away: string;
  kickoff_at: string;
  status: "scheduled" | "live" | "finished";
  live_state: { minute?: number; home_goals?: number; away_goals?: number };
}

export function mapMatch(w: WireMatch): Match {
  return {
    id: w.id,
    fixture_id: w.fixture_id,
    home: w.home,
    away: w.away,
    kickoff_at: w.kickoff_at,
    status: w.status === "finished" ? "ft" : w.status,
    live_state: {
      minute: w.live_state?.minute,
      period: w.status === "finished" ? "FT" : undefined,
      home_score: w.live_state?.home_goals ?? 0,
      away_score: w.live_state?.away_goals ?? 0,
    },
  };
}

// The Go book is outcome-indexed ([0]=NO, [1]=YES) with four resting
// populations. The view is a single YES ladder (ADR 0002 unified ladder):
//   YES asks = SELL YES ∪ complement(BUY NO)   (a NO bid at p asks YES at 100−p)
//   YES bids = BUY YES  ∪ complement(SELL NO)
export interface WireBook {
  bids: [BookLevel[], BookLevel[]];
  asks: [BookLevel[], BookLevel[]];
}

export function mapBook(w: WireBook): Book {
  const complement = (levels: BookLevel[] = []) =>
    levels.map((l) => ({ price: 100 - l.price, size: l.size }));
  const merge = (a: BookLevel[], b: BookLevel[]): BookLevel[] => {
    const bySize = new Map<number, number>();
    for (const l of [...a, ...b]) bySize.set(l.price, (bySize.get(l.price) ?? 0) + l.size);
    return Array.from(bySize, ([price, size]) => ({ price, size }));
  };
  const bids = merge(w.bids?.[1] ?? [], complement(w.asks?.[0])).sort(
    (x, y) => y.price - x.price
  );
  const asks = merge(w.asks?.[1] ?? [], complement(w.bids?.[0])).sort(
    (x, y) => x.price - y.price
  );
  return { bids, asks };
}

interface WireFill {
  taker_hash: string;
  maker_hash: string;
  price: number;
  size: number;
  match_type: Fill["match_type"];
  settle_tx?: string;
  ts: string;
}

function mapFill(w: WireFill): Fill {
  return {
    taker_hash: w.taker_hash,
    maker_hash: w.maker_hash,
    price: w.price,
    size: w.size,
    match_type: w.match_type,
    ts: Date.parse(w.ts) || Date.now(),
  };
}

// ---- client -----------------------------------------------------------------

export const api = {
  live: LIVE,

  async getMarket(id: string): Promise<Market> {
    return get<Market, WireMarket>(
      `/markets/${id}`,
      () => {
        if (id !== DEMO_MARKET_ID && id !== demoMarket.id)
          throw new ApiError(404, "unknown market");
        return demoMarket;
      },
      mapMarket
    );
  },

  async getMatch(matchId: string): Promise<Match> {
    return get<Match, { matches: WireMatch[] }>(
      `/matches`,
      () => demoMatch,
      (w) => {
        const m = w.matches?.find((x) => x.id === matchId) ?? w.matches?.[0];
        if (!m) throw new ApiError(404, "match not found");
        return mapMatch(m);
      }
    );
  },

  async getBook(id: string): Promise<Book> {
    return get<Book, WireBook>(`/markets/${id}/book`, () => demoBook, mapBook);
  },

  async getFills(id: string): Promise<Fill[]> {
    return get<Fill[], { fills: WireFill[] | null }>(
      `/markets/${id}/fills`,
      () => demoFills,
      (w) => (w.fills ?? []).map(mapFill)
    );
  },

  async getOneliners(id: string): Promise<string[]> {
    return get<string[], { lines: string[] | null }>(
      `/markets/${id}/oneliners`,
      () => demoOneliners,
      (w) => w.lines ?? []
    );
  },

  async getBalance(wallet: string | null): Promise<number> {
    if (!wallet) return LIVE ? 0 : demoBalanceMicro;
    return get<number, { usdc_available: number }>(
      `/balance?wallet=${encodeURIComponent(wallet)}`,
      () => demoBalanceMicro,
      (w) => w.usdc_available
    );
  },

  async getSettlement(id: string): Promise<Settlement> {
    return get<
      Settlement,
      {
        market_id: string;
        title: string;
        status: Market["status"];
        outcome?: { result?: string; score?: string };
        chain_tx?: string;
      }
    >(
      `/markets/${id}/settlement`,
      () => demoSettlement,
      (w) => ({
        market_id: w.market_id,
        title: w.title,
        scoreline: w.outcome?.score ?? "",
        status: w.status,
        winner: w.outcome?.result === "yes" ? "YES" : "NO",
        resolved_by: "Operator key (oracle tier-a) — TxODDS-signed is tier-d",
        program_id: PROGRAM_ID,
        deploy_tx: DEPLOY_TX,
        your_shares: 0,
        your_payout_micro: 0,
        timeline: [
          {
            label: "Market resolved on-chain",
            detail: "resolve_market (tier-a)",
            tx: w.chain_tx ?? null,
          },
          {
            label: "Program deployed on devnet",
            detail: "pitchmarket @ pinned ID",
            tx: DEPLOY_TX,
          },
        ],
      })
    );
  },

  async getPortfolio(wallet: string | null): Promise<Portfolio> {
    if (!wallet) return LIVE ? emptyPortfolio() : demoPortfolio;
    return get<
      Portfolio,
      {
        balance: { usdc_available: number };
        positions: {
          market_id: string;
          yes: number;
          no: number;
          avg_cost: number;
        }[] | null;
        orders: {
          order_hash: string;
          market_id: string;
          outcome: number;
          side: number;
          price: number;
          size: number;
          remaining: number;
          status: string;
        }[] | null;
        fills: WireFill[] | null;
      }
    >(
      `/portfolio?wallet=${encodeURIComponent(wallet)}`,
      () => demoPortfolio,
      (w) => ({
        balance_micro: w.balance?.usdc_available ?? 0,
        positions: (w.positions ?? []).map((p) => ({
          market_id: p.market_id,
          title: shortId(p.market_id),
          yes: p.yes,
          no: p.no,
          avg_cost: p.avg_cost,
          current: p.avg_cost, // live mid joined client-side later
        })),
        orders: (w.orders ?? []).map((o) => ({
          order_hash: o.order_hash,
          title: shortId(o.market_id),
          outcome: o.outcome === 1 ? "YES" : "NO",
          side: (o.side === 0 ? "buy" : "sell") as Side,
          price: o.price,
          size: o.size,
          remaining: o.remaining,
          status: o.status,
        })),
        history: (w.fills ?? []).map((f) => ({
          title: shortId(f.taker_hash),
          side: "buy" as Side,
          outcome: "YES" as const,
          price: f.price,
          size: f.size,
          ts: Date.parse(f.ts) || Date.now(),
          tx: f.settle_tx ?? "",
        })),
      })
    );
  },

  /** Submit a signed order. Throws ApiError: 401 bad sig, 402 funds, 409 replay. */
  async postOrder(order: {
    maker: string;
    market_id: string;
    outcome: number;
    side: number;
    price: number;
    size: number;
    fee_bps: number;
    expiry: number;
    salt: number;
    sig: string;
  }): Promise<{ order_hash: string; fills: { match_type: string }[] }> {
    if (!LIVE) {
      await new Promise((r) => setTimeout(r, 500));
      return { order_hash: "demo", fills: [] };
    }
    return post(`/orders`, order);
  },

  /** Demo faucet: mirrors an on-chain vault deposit (micro-USDC). */
  async deposit(wallet: string, amountMicro: number): Promise<void> {
    if (!LIVE) return;
    await post(`/wallet/deposit`, { wallet, amount: amountMicro });
  },
};

function emptyPortfolio(): Portfolio {
  return { balance_micro: 0, positions: [], orders: [], history: [] };
}

function shortId(hex: string): string {
  return `${hex.slice(0, 6)}…${hex.slice(-4)}`;
}
