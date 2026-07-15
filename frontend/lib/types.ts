// Wire types mirror the Go REST/WS surface (backend/internal/api/api.go).
// Money is integer micro-USDC; prices are integer cents 1..99; ids/hashes are hex.

export type MarketStatus =
  | "draft"
  | "open"
  | "closed"
  | "resolving"
  | "settled"
  | "void";

export interface Match {
  id: string;
  fixture_id: string;
  home: string;
  away: string;
  kickoff_at: string;
  status: "scheduled" | "live" | "ht" | "ft";
  live_state: {
    minute?: number;
    period?: string; // "1H" | "HT" | "2H" | "FT"
    home_score?: number;
    away_score?: number;
  };
}

export interface Market {
  id: string;
  market_id: string; // 64-hex
  match_id: string;
  template_key: string;
  type: "binary" | "precision";
  title: string;
  rule: string;
  status: MarketStatus;
  outcome?: { winner?: "YES" | "NO"; value?: number; void?: boolean } | null;
  chain_tx?: string;
}

export interface BookLevel {
  price: number; // cents 1..99, for the YES outcome
  size: number; // shares
}

export interface Book {
  // Levels for the YES outcome. NO is the mirror (100 - price).
  bids: BookLevel[]; // descending price
  asks: BookLevel[]; // ascending price
}

export interface Fill {
  taker_hash: string;
  maker_hash: string;
  price: number; // cents
  size: number; // shares
  match_type: "NORMAL" | "MINT" | "MERGE";
  settle_tx?: string; // devnet signature once the crank confirms
  ts: number; // unix ms
}

export type Side = "buy" | "sell";

export interface SettleStep {
  label: string;
  detail: string;
  tx: string | null; // devnet signature, or null for off-chain steps
}

export interface Settlement {
  market_id: string;
  title: string;
  scoreline: string; // "Brazil 2 – 0 Argentina"
  status: MarketStatus;
  winner: "YES" | "NO" | "VOID";
  resolved_by: string; // "TxODDS signed outcome (tier-a)"
  program_id: string;
  deploy_tx: string;
  your_shares: number;
  your_payout_micro: number;
  timeline: SettleStep[];
}

export interface Position {
  market_id: string;
  title: string;
  yes: number;
  no: number;
  avg_cost: number; // cents
  current: number; // cents — best bid (BBP): the price the position exits at now
  realized: number; // micro-USDC, booked on sells
}

export interface OpenOrder {
  order_hash: string;
  market_id: string;
  title: string;
  outcome: "YES" | "NO";
  side: Side;
  price: number;
  size: number;
  remaining: number;
  status: string;
}

export interface HistoryFill {
  title: string;
  side: Side;
  outcome: "YES" | "NO";
  price: number;
  size: number;
  ts: number;
  tx: string;
}

export interface Portfolio {
  balance_micro: number;
  positions: Position[];
  orders: OpenOrder[];
  history: HistoryFill[];
}
