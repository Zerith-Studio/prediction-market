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
  outcome?: { winner?: "YES" | "NO"; value?: number } | null;
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
  ts: number; // unix ms (client-side for the demo)
}

export type Side = "buy" | "sell";
