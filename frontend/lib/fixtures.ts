import type { Book, Fill, Market, Match } from "./types";

// Demo fixture data — mirrors the shape the Go backend serves. Swap for live
// calls by setting NEXT_PUBLIC_API_URL (see api.ts). Match/market ids are the
// ones the demo-final fixture uses conceptually.

export const DEMO_MARKET_ID =
  "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90";

export const demoMatch: Match = {
  id: "m-bra-arg",
  fixture_id: "wc26-c-002",
  home: "Brazil",
  away: "Argentina",
  kickoff_at: "2026-07-15T19:00:00Z",
  status: "live",
  live_state: { minute: 67, period: "2H", home_score: 1, away_score: 0 },
};

export const demoMarket: Market = {
  id: "mk-bra-win",
  market_id: DEMO_MARKET_ID,
  match_id: "m-bra-arg",
  template_key: "match_winner",
  type: "binary",
  title: "Brazil to win",
  rule: "Resolves YES if Brazil wins in regulation. Signed TxODDS outcome settles on Solana.",
  status: "open",
};

export const demoBook: Book = {
  asks: [
    { price: 66, size: 400 },
    { price: 67, size: 300 },
    { price: 68, size: 420 },
    { price: 70, size: 250 },
  ],
  bids: [
    { price: 64, size: 560 },
    { price: 63, size: 380 },
    { price: 62, size: 610 },
    { price: 60, size: 300 },
  ],
};

export const demoFills: Fill[] = [
  { taker_hash: "7f3a9c22e1d4", maker_hash: "b2c40041aa9", price: 64, size: 120, match_type: "NORMAL", ts: Date.now() - 8_000 },
  { taker_hash: "1ae62390f0f", maker_hash: "9ec98780f0", price: 65, size: 300, match_type: "MINT", ts: Date.now() - 21_000 },
  { taker_hash: "4cf2bf10f0f", maker_hash: "553f3950f0", price: 63, size: 210, match_type: "NORMAL", ts: Date.now() - 44_000 },
];

export const demoOneliners: string[] = [
  "Argentina's press has cratered — 67 minutes in, YES on Brazil just went bid-heavy.",
  "Vinícius is running at a back line that's dropping deeper every minute.",
  "One goal separates them, but the book is pricing this like it's already over.",
  "Argentina need a spark. The order book isn't betting on one.",
];

export const demoBalanceMicro = 1_240_000_000; // $1,240.00 in the vault
