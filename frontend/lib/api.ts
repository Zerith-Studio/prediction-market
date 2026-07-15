import type { Book, Fill, Market, Match } from "./types";
import {
  DEMO_MARKET_ID,
  demoBalanceMicro,
  demoBook,
  demoFills,
  demoMarket,
  demoMatch,
  demoOneliners,
} from "./fixtures";

// Typed client for the Go REST surface. When NEXT_PUBLIC_API_URL is set it hits
// the real backend; otherwise it serves the demo fixtures so the UI renders with
// zero infrastructure. Every path/param matches backend/internal/api/api.go, so
// going live is a base-URL flip — not a rewrite.

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "";
const LIVE = BASE.length > 0;

async function get<T>(path: string, fallback: () => T): Promise<T> {
  if (!LIVE) {
    // Simulate a tiny network delay so skeleton states are real, not theatre.
    await new Promise((r) => setTimeout(r, 240));
    return fallback();
  }
  const res = await fetch(`${BASE}${path}`, { cache: "no-store" });
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

export const api = {
  live: LIVE,

  async getMarket(id: string): Promise<Market> {
    return get(`/markets/${id}`, () => {
      if (id !== DEMO_MARKET_ID && id !== demoMarket.id) throw new ApiError(404, "unknown market");
      return demoMarket;
    });
  },

  async getMatch(_marketMatchId: string): Promise<Match> {
    return get(`/matches`, () => demoMatch);
  },

  async getBook(id: string): Promise<Book> {
    return get(`/markets/${id}/book`, () => demoBook);
  },

  async getFills(id: string): Promise<Fill[]> {
    return get(`/markets/${id}/fills`, () => demoFills);
  },

  async getOneliners(id: string): Promise<string[]> {
    return get(`/markets/${id}/oneliners`, () => demoOneliners);
  },

  async getBalance(_wallet: string): Promise<number> {
    return get(`/balance`, () => demoBalanceMicro);
  },
};
