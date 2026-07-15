import type {
  Book,
  Fill,
  Market,
  Match,
  Portfolio,
  Settlement,
} from "./types";

// Real devnet transaction signatures from the proven end-to-end run
// (docs/HANDOFF.md §1) — these resolve on Solana explorer for real.
export const PROGRAM_ID = "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs";
const TX = {
  deploy: "5Ayf6cLmSpqFue5odVvTVSBQSPMyJjyV6ndhp9FPu6F46CYDSkJucuDyPTpKMQvbpfv4XzC33v4bnfnaj4xXgVqa",
  settle: "3zNVPQJqLZhAuRpEmCzGxVfA9aqQe3mm3qT1yFzcN34rrNqM1Eu2oyuagxvdcT51xTjW86ggzjNGhrbvYoKzvdXS",
  resolve: "5oNcWKQBin6atteQcvAAtEkdivE5q9hXKmYXWeNiKzrXrS7X2VJN2SvSe7pxQ8oCMvjrSjBMr2T9i1uWtVJfXiK8",
  redeem: "4qKCYL4G1VzsPighcWLQ6wgEfYBggHnFCkpHfomXBkWdCfVzrEHv4Ju3dLAwtKHNx62WEyV7Tvi2VqxeRSMWkMku",
};

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

export const demoSettlement: Settlement = {
  market_id: DEMO_MARKET_ID,
  title: "Brazil to win",
  scoreline: "Brazil 2 – 0 Argentina",
  status: "settled",
  winner: "YES",
  resolved_by: "TxODDS signed outcome · tier-a",
  program_id: PROGRAM_ID,
  deploy_tx: TX.deploy,
  your_shares: 500,
  your_payout_micro: 500_000_000,
  timeline: [
    { label: "Order signed", detail: "taker + maker · ed25519, off-chain", tx: null },
    { label: "Matched & settled", detail: "MINT · settle_match, ed25519 verified on-chain", tx: TX.settle },
    { label: "Market resolved", detail: "resolve_market · signed TxODDS outcome", tx: TX.resolve },
    { label: "Redeemed 1:1", detail: "500 YES burned → $500.00 from pool", tx: TX.redeem },
  ],
};

const MARKET_2 = "b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1";

export const demoPortfolio: Portfolio = {
  balance_micro: demoBalanceMicro,
  positions: [
    { market_id: DEMO_MARKET_ID, title: "Brazil to win", yes: 500, no: 0, avg_cost: 62, current: 66 },
    { market_id: MARKET_2, title: "Over 2.5 goals · BRA–ARG", yes: 0, no: 300, avg_cost: 64, current: 41 },
  ],
  orders: [
    { order_hash: "7f3a9c22e1d4b2c40041aa9fe0", title: "Brazil to win", outcome: "YES", side: "buy", price: 63, size: 400, remaining: 400, status: "live" },
    { order_hash: "1ae62390f0f9ec98780f04cf2b", title: "France to win", outcome: "YES", side: "buy", price: 55, size: 200, remaining: 120, status: "live" },
  ],
  history: [
    { title: "Brazil to win", side: "buy", outcome: "YES", price: 62, size: 500, ts: Date.now() - 320_000, tx: TX.settle },
    { title: "Over 2.5 goals · BRA–ARG", side: "sell", outcome: "NO", price: 45, size: 300, ts: Date.now() - 900_000, tx: TX.settle },
  ],
};
