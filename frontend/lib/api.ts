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

export const explorerTx = (sig: string) =>
  `https://explorer.solana.com/tx/${sig}?cluster=devnet`;
export const explorerAddr = (addr: string) =>
  `https://explorer.solana.com/address/${addr}?cluster=devnet`;

export const PROGRAM_ID = "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs";
export const DEPLOY_TX =
  "5Ayf6cLmSpqFue5odVvTVSBQSPMyJjyV6ndhp9FPu6F46CYDSkJucuDyPTpKMQvbpfv4XzC33v4bnfnaj4xXgVqa";

// Typed client for the Go REST surface (backend/internal/api/api.go). All data
// is REAL — TxLINE-driven markets, devnet settlement. NEXT_PUBLIC_API_URL must
// point at the exchange (configured() gates the UI's connect state).

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

export function configured(): boolean {
  return BASE.length > 0;
}

export function wsUrl(): string {
  return BASE.replace(/^http/, "ws") + "/ws";
}

async function get<T, W = T>(path: string, map?: (wire: W) => T): Promise<T> {
  if (!BASE) throw new ApiError(0, "NEXT_PUBLIC_API_URL is not configured");
  const res = await fetch(`${BASE}${path}`, { cache: "no-store" });
  if (!res.ok) throw new ApiError(res.status, await safeText(res));
  const wire = (await res.json()) as W;
  return map ? map(wire) : (wire as unknown as T);
}

async function post<T>(path: string, body: unknown): Promise<T> {
  if (!BASE) throw new ApiError(0, "NEXT_PUBLIC_API_URL is not configured");
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
    outcome:
      w.outcome?.result === "void"
        ? { void: true }
        : w.outcome?.result
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

// The Go book is outcome-indexed ([0]=NO, [1]=YES). The view is a single YES
// ladder (ADR 0002): YES asks = SELL YES ∪ complement(BUY NO); bids mirrored.
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
    settle_tx: w.settle_tx || undefined,
    ts: Date.parse(w.ts) || Date.now(),
  };
}

export interface PrecisionEntry {
  user: string;
  guess: number;
  stake: number;
  score?: number;
  payout?: number;
  ts: string;
}

export interface RFQQuote {
  quote_hash: string;
  maker: string;
  stake: number;
  payout: number;
  expiry: string;
  status: string;
}

export interface RFQ {
  id: string;
  taker: string;
  legs: { market_id: string; outcome: number }[];
  stake: number;
  status: string;
}

// ---- client -----------------------------------------------------------------

export const api = {
  configured: configured(),

  async listMatches(): Promise<Match[]> {
    return get<Match[], { matches: WireMatch[] | null }>(`/matches`, (w) =>
      (w.matches ?? []).map(mapMatch)
    );
  },

  async listMarkets(status = ""): Promise<Market[]> {
    const q = status ? `?status=${status}` : "";
    return get<Market[], { markets: WireMarket[] | null }>(`/markets${q}`, (w) =>
      (w.markets ?? []).map(mapMarket)
    );
  },

  async getMarket(id: string): Promise<Market> {
    return get<Market, WireMarket>(`/markets/${id}`, mapMarket);
  },

  async getMatch(matchId: string): Promise<Match> {
    return get<Match, { matches: WireMatch[] | null }>(`/matches`, (w) => {
      const m = (w.matches ?? []).find((x) => x.id === matchId);
      if (!m) throw new ApiError(404, "match not found");
      return mapMatch(m);
    });
  },

  async getBook(id: string): Promise<Book> {
    return get<Book, WireBook>(`/markets/${id}/book`, mapBook);
  },

  async getFills(id: string): Promise<Fill[]> {
    return get<Fill[], { fills: WireFill[] | null }>(`/markets/${id}/fills`, (w) =>
      (w.fills ?? []).map(mapFill)
    );
  },

  async getOneliners(id: string): Promise<string[]> {
    return get<string[], { lines: string[] | null }>(
      `/markets/${id}/oneliners`,
      (w) => w.lines ?? []
    );
  },

  async getBalance(wallet: string | null): Promise<number> {
    if (!wallet) return 0;
    return get<number, { usdc_available: number }>(
      `/balance?wallet=${encodeURIComponent(wallet)}`,
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
    >(`/markets/${id}/settlement`, (w) => ({
      market_id: w.market_id,
      title: w.title,
      scoreline: w.outcome?.score ?? "",
      status: w.status,
      winner:
        w.outcome?.result === "void" ? "VOID" : w.outcome?.result === "yes" ? "YES" : "NO",
      resolved_by: "Operator key (oracle tier-a); TxLINE data-driven",
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
    }));
  },

  async getPortfolio(wallet: string | null): Promise<Portfolio> {
    if (!wallet)
      return {
        balance_micro: 0,
        positions: [],
        orders: [],
        history: [],
        precision: [],
        combos: [],
      };
    // Titles come from the markets list — one extra call, joined client-side.
    const titles = new Map<string, string>();
    try {
      for (const m of await this.listMarkets()) titles.set(m.market_id, m.title);
    } catch {
      /* portfolio still renders with ids */
    }
    return get<
      Portfolio,
      {
        balance: { usdc_available: number };
        positions:
          | {
              market_id: string;
              yes: number;
              no: number;
              yes_locked: number;
              no_locked: number;
              avg_cost: number;
              realized: number;
              best_bid: number;
            }[]
          | null;
        orders:
          | {
              order_hash: string;
              market_id: string;
              outcome: number;
              side: number;
              price: number;
              size: number;
              remaining: number;
              status: string;
            }[]
          | null;
        fills: WireFill[] | null;
        precision:
          | {
              market_id: string;
              title: string;
              status: Market["status"];
              guess: number;
              stake: number;
              score?: number;
              payout?: number;
            }[]
          | null;
        combos:
          | {
              quote_hash: string;
              status: "accepted" | "won" | "lost" | "void";
              legs: number;
              stake: number;
              payout: number;
              resolve_tx?: string;
            }[]
          | null;
      }
    >(`/portfolio?wallet=${encodeURIComponent(wallet)}`, (w) => ({
      balance_micro: w.balance?.usdc_available ?? 0,
      positions: (w.positions ?? [])
        .filter((p) => p.yes > 0 || p.no > 0 || p.realized !== 0)
        .map((p) => ({
          market_id: p.market_id,
          title: titles.get(p.market_id) ?? shortId(p.market_id),
          yes: p.yes,
          no: p.no,
          avg_cost: p.avg_cost,
          current: p.best_bid, // BBP mark — the price the position exits at NOW
          realized: p.realized,
        })),
      orders: (w.orders ?? [])
        .filter((o) => o.status === "live")
        .map((o) => ({
          order_hash: o.order_hash,
          market_id: o.market_id,
          title: titles.get(o.market_id) ?? shortId(o.market_id),
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
      precision: (w.precision ?? []).map((p) => ({
        market_id: p.market_id,
        title: p.title,
        status: p.status,
        guess: p.guess,
        stake_micro: p.stake,
        score: p.score,
        payout_micro: p.payout,
      })),
      combos: (w.combos ?? []).map((c) => ({
        quote_hash: c.quote_hash,
        status: c.status,
        legs: c.legs,
        stake_micro: c.stake,
        payout_micro: c.payout,
        resolve_tx: c.resolve_tx,
      })),
    }));
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
    return post(`/orders`, order);
  },

  /** Cancel a live order (releases the soft-lock; book + WS update). */
  async cancelOrder(orderHash: string, maker: string): Promise<void> {
    if (!BASE) throw new ApiError(0, "NEXT_PUBLIC_API_URL is not configured");
    const res = await fetch(
      `${BASE}/orders/${orderHash}?maker=${encodeURIComponent(maker)}`,
      { method: "DELETE" }
    );
    if (!res.ok) throw new ApiError(res.status, await safeText(res));
  },

  // ---- real on-chain deposit (two-step; falls back to the mirror faucet
  // when the server runs off-chain) ----
  async depositInit(
    wallet: string,
    amountMicro: number
  ): Promise<{ deposit_id: string; message_b64: string } | null> {
    try {
      return await post(`/wallet/deposit-init`, { wallet, amount: amountMicro });
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) return null; // mirror mode
      throw e;
    }
  },

  async depositComplete(
    depositID: string,
    wallet: string,
    amountMicro: number,
    sigHex: string
  ): Promise<{ tx: string }> {
    return post(`/wallet/deposit-complete`, {
      deposit_id: depositID,
      wallet,
      amount: amountMicro,
      sig: sigHex,
    });
  },

  async depositMirror(wallet: string, amountMicro: number): Promise<void> {
    await post(`/wallet/deposit`, { wallet, amount: amountMicro });
  },

  // ---- precision ----
  async enterPrecision(marketId: string, wallet: string, guess: number, stake: number) {
    return post<{ entry_id: string }>(`/markets/${marketId}/precision`, {
      wallet,
      guess,
      stake,
    });
  },

  async leaderboard(marketId: string): Promise<{ entries: PrecisionEntry[]; status: string }> {
    // An empty pool comes back as {entries: null} (Go nil slice → JSON null);
    // coerce to [] so consumers can .reduce/.some/.map safely.
    const r = await get<{ entries: PrecisionEntry[] | null; status: string }>(
      `/markets/${marketId}/precision/leaderboard`,
    );
    return { entries: r.entries ?? [], status: r.status };
  },

  // ---- combos (RFQ) ----
  async createRFQ(taker: string, legs: { market_id: string; outcome: number }[], stake: number) {
    return post<{ rfq_id: string }>(`/combos`, { taker, legs, stake });
  },

  async getRFQ(id: string): Promise<{ rfq: RFQ; quotes: RFQQuote[] }> {
    return get(`/combos/${id}`);
  },

  async acceptQuote(rfqId: string, quoteHash: string, taker: string) {
    return post<{ accepted: string; accept_tx: string }>(`/combos/${rfqId}/accept`, {
      quote_hash: quoteHash,
      taker,
    });
  },
};

function shortId(hex: string): string {
  return `${hex.slice(0, 6)}…${hex.slice(-4)}`;
}
