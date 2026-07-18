import type {
  Book,
  BookLevel,
  Comment,
  Fill,
  Lineups,
  Market,
  Match,
  NewsItem,
  Portfolio,
  Settlement,
  Side,
  TeamMatchStats,
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
  featured_rank?: number | null;
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
    featured_rank: w.featured_rank ?? null,
  };
}

interface WireMatch {
  id: string;
  fixture_id: string;
  home: string;
  away: string;
  kickoff_at: string;
  status: "scheduled" | "live" | "finished";
  live_state: {
    minute?: number;
    period?: string;
    home_goals?: number;
    away_goals?: number;
    possession?: { home: number; away: number };
    stats?: { home: TeamMatchStats; away: TeamMatchStats };
  };
  lineups?: Lineups | null;
}

export function mapMatch(w: WireMatch): Match {
  const ls = w.live_state ?? {};
  return {
    id: w.id,
    fixture_id: w.fixture_id,
    home: w.home,
    away: w.away,
    kickoff_at: w.kickoff_at,
    status: w.status === "finished" ? "ft" : w.status,
    live_state: {
      minute: ls.minute,
      period: ls.period ?? (w.status === "finished" ? "FT" : undefined),
      home_score: ls.home_goals ?? 0,
      away_score: ls.away_goals ?? 0,
      possession: ls.possession,
      stats: ls.stats,
    },
    // 'null' JSONB deserializes to null; normalize to undefined-ish for the UI.
    lineups: w.lineups ?? null,
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

export interface OpenRFQLeg {
  market_id: string;
  outcome: number; // 0 = NO, 1 = YES
  title: string;
}
export interface OpenRFQ {
  id: string;
  taker: string;
  stake: number; // micro-USDC the taker staked
  legs: OpenRFQLeg[];
  created_at: string;
}
interface WireOpenRFQ {
  id: string;
  taker: string;
  stake: number;
  legs: { market_id: string; outcome: number }[];
  created_at: string;
}

// ---- client -----------------------------------------------------------------

export const api = {
  configured: configured(),

  async listMatches(): Promise<Match[]> {
    return get<Match[], { matches: WireMatch[] | null }>(`/matches`, (w) =>
      (w.matches ?? []).map(mapMatch)
    );
  },

  // Latest hourly Breaking News (real Exa articles + real Yes%/delta). The wire
  // shape already matches NewsItem; degrade to [] when the panel is empty.
  async getBreakingNews(): Promise<NewsItem[]> {
    return get<NewsItem[], { items: NewsItem[] | null }>(`/news`, (w) => w.items ?? []);
  },

  // Per-market comments (unsigned — wallet is a claim). Pass the viewer wallet
  // to get per-comment `liked` flags.
  async getComments(marketId: string, wallet: string | null): Promise<Comment[]> {
    const q = wallet ? `?wallet=${encodeURIComponent(wallet)}` : "";
    return get<Comment[], { comments: Comment[] | null }>(
      `/markets/${marketId}/comments${q}`,
      (w) => w.comments ?? []
    );
  },
  async postComment(
    marketId: string,
    body: { wallet: string; body: string; parent_id?: string }
  ): Promise<Comment> {
    return post<Comment>(`/markets/${marketId}/comments`, body);
  },
  async likeComment(commentId: string, wallet: string): Promise<{ liked: boolean; like_count: number }> {
    return post<{ liked: boolean; like_count: number }>(`/comments/${commentId}/like`, { wallet });
  },
  async editComment(commentId: string, wallet: string, body: string): Promise<{ id: string; body: string }> {
    return post<{ id: string; body: string }>(`/comments/${commentId}/edit`, { wallet, body });
  },
  async deleteComment(commentId: string, wallet: string): Promise<{ deleted: string }> {
    return post<{ deleted: string }>(`/comments/${commentId}/delete`, { wallet });
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
    // Full detail (live_state + team sheets) from the dedicated route. Fall back
    // to the list endpoint if the backend predates that route (404) — the market
    // page still loads, just without lineups/stats until the backend redeploys.
    try {
      return await get<Match, WireMatch>(`/matches/${matchId}`, mapMatch);
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 404) throw e;
      return get<Match, { matches: WireMatch[] | null }>(`/matches`, (w) => {
        const m = (w.matches ?? []).find((x) => x.id === matchId);
        if (!m) throw new ApiError(404, "match not found");
        return mapMatch(m);
      });
    }
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
              leg_details?: { market_id: string; outcome: number }[];
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
        legDetails: (c.leg_details ?? []).map((l) => ({
          outcome: l.outcome,
          title: titles.get(l.market_id) ?? shortId(l.market_id),
        })),
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

  // Market-maker view: open RFQs awaiting a quote (titles joined client-side).
  async listOpenRFQs(): Promise<OpenRFQ[]> {
    const titles = new Map<string, string>();
    try {
      for (const m of await this.listMarkets()) titles.set(m.market_id, m.title);
    } catch {
      /* still renders with ids */
    }
    const r = await get<{ rfqs: WireOpenRFQ[] | null }>(`/combos`);
    return (r.rfqs ?? []).map((rq) => ({
      id: rq.id,
      taker: rq.taker,
      stake: rq.stake,
      created_at: rq.created_at,
      legs: (rq.legs ?? []).map((l) => ({
        market_id: l.market_id,
        outcome: l.outcome,
        title: titles.get(l.market_id) ?? shortId(l.market_id),
      })),
    }));
  },

  // Submit a signed ComboQuote as a market maker (sig over borshComboQuote).
  async submitQuote(
    rfqId: string,
    q: {
      maker: string;
      legs: { market_id: string; outcome: number }[];
      stake: number;
      payout: number;
      expiry: number;
      salt: number;
      sig: string;
    },
  ): Promise<{ quote_hash: string }> {
    return post(`/combos/${rfqId}/quotes`, q);
  },
};

function shortId(hex: string): string {
  return `${hex.slice(0, 6)}…${hex.slice(-4)}`;
}
