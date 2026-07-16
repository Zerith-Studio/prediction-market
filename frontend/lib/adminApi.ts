// Typed client for the operator-gated /admin surface (backend/internal/api/
// admin.go). Auth is an operator-wallet signature exchanged for an in-memory
// session token, kept in localStorage and sent as X-Admin-Session on every
// call. A 401 clears the token so the page falls back to the sign-in gate.

import { ApiError } from "./api";

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "";
const TOKEN_KEY = "pm_admin_session";

export function adminConfigured(): boolean {
  return BASE.length > 0;
}

function token(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

export function setAdminToken(t: string | null) {
  if (typeof window === "undefined") return;
  if (t) window.localStorage.setItem(TOKEN_KEY, t);
  else window.localStorage.removeItem(TOKEN_KEY);
}

export function hasAdminSession(): boolean {
  return !!token();
}

async function safeText(res: Response): Promise<string> {
  try {
    const j = await res.json();
    return j.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}

async function req<T>(
  method: "GET" | "POST",
  path: string,
  body?: unknown,
): Promise<T> {
  if (!BASE) throw new ApiError(0, "NEXT_PUBLIC_API_URL is not configured");
  const headers: Record<string, string> = {};
  const t = token();
  if (t) headers["X-Admin-Session"] = t;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    cache: "no-store",
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const msg = await safeText(res);
    if (res.status === 401) setAdminToken(null); // stale session → force re-login
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// ---- wire types -------------------------------------------------------------

export interface Challenge {
  nonce: string;
  message: string; // the exact UTF-8 string the wallet signs
  expires: string;
}

export interface AdminFixture {
  id: string;
  home: string;
  away: string;
  kickoff: string;
  competition: string;
  live: boolean;
  registered: boolean;
}

export interface AdminMarketBook {
  yes_bid?: number;
  yes_ask?: number;
  bid_levels: number;
  ask_levels: number;
}

export interface AdminMarket {
  id: string;
  market_id: string; // 64-hex — the path id for resolve/close/cancel
  match_id: string;
  template_key: string;
  type: "binary" | "precision";
  title: string;
  rule: string;
  status: string;
  outcome?: { result?: string; actual?: number; score?: string } | null;
  chain_tx?: string;
  created_at: string;
  book?: AdminMarketBook;
}

export interface AdminOps {
  chain_enabled: boolean;
  admin_pubkey?: string;
  operator?: string;
  operator_sol?: number;
  txline_expires?: string;
  txline_valid?: boolean;
  markets_by_status: Record<string, number>;
}

export interface FinalScore {
  home_goals: number;
  away_goals: number;
  ht_home_goals?: number;
  ht_away_goals?: number;
  total_passes?: number;
  minute?: number;
  abandoned?: boolean;
}

// ---- client -----------------------------------------------------------------

export const admin = {
  configured: adminConfigured(),

  challenge: () => req<Challenge>("GET", "/admin/challenge"),

  session: (pubkey: string, nonce: string, sig: string) =>
    req<{ token: string; expires: string }>("POST", "/admin/session", {
      pubkey,
      nonce,
      sig,
    }),

  fixtures: (competition = 72) =>
    req<{ fixtures: AdminFixture[] }>(
      "GET",
      `/admin/fixtures?competition=${competition}`,
    ).then((r) => r.fixtures ?? []),

  odds: (fixtureId: string) =>
    req<{ fixture_id: string; odds: Record<string, number> }>(
      "GET",
      `/admin/fixtures/${fixtureId}/odds`,
    ).then((r) => r.odds ?? {}),

  createMarkets: (
    fixtureId: string,
    home: string,
    away: string,
    kickoff: string,
  ) =>
    req<{ fixture_id: string; markets: AdminMarket[] }>(
      "POST",
      `/admin/fixtures/${fixtureId}/markets`,
      { home, away, kickoff },
    ).then((r) => r.markets ?? []),

  markets: (status = "") =>
    req<{ markets: AdminMarket[] }>(
      "GET",
      `/admin/markets${status ? `?status=${status}` : ""}`,
    ).then((r) => r.markets ?? []),

  resolveMarket: (marketId: string, outcome: string, value?: number) =>
    req<{ market_id: string; tx: string }>(
      "POST",
      `/admin/markets/${marketId}/resolve`,
      { outcome, value },
    ),

  closeMarket: (marketId: string) =>
    req<{ market_id: string; status: string }>(
      "POST",
      `/admin/markets/${marketId}/close`,
      {},
    ),

  cancelOrders: (marketId: string) =>
    req<{ market_id: string; cancelled: number }>(
      "POST",
      `/admin/markets/${marketId}/cancel-orders`,
      {},
    ),

  resolveFixture: (fixtureId: string, score: FinalScore) =>
    req<{ fixture_id: string; markets: AdminMarket[] }>(
      "POST",
      `/admin/fixtures/${fixtureId}/resolve`,
      score,
    ).then((r) => r.markets ?? []),

  ops: () => req<AdminOps>("GET", "/admin/ops"),
};
