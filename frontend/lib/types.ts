// Wire types mirror the Go REST/WS surface (backend/internal/api/api.go).
// Money is integer micro-USDC; prices are integer cents 1..99; ids/hashes are hex.

export type MarketStatus =
  | "draft"
  | "open"
  | "closed"
  | "resolving"
  | "settled"
  | "void";

// Per-team live match stats (TxLINE scores feed). Only fields the feed actually
// carries are present; absent stats are simply not rendered.
export interface TeamMatchStats {
  yellow?: number;
  red?: number;
  corners?: number;
  shots?: number;
  shots_on?: number;
}

// One squad member. number + name are always present; position is "" when the
// feed's position code isn't mapped (we never fabricate a label).
export interface LineupPlayer {
  number: string;
  name: string;
  position?: string;
  unit: number; // line grouping: lower = own goal, higher = attack
  captain?: boolean;
}

export interface TeamLineup {
  team: string;
  formation?: string; // derived from unit counts; may be absent
  starters: LineupPlayer[];
  subs: LineupPlayer[];
}

export interface Lineups {
  home?: TeamLineup;
  away?: TeamLineup;
}

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
    possession?: { home: number; away: number };
    stats?: { home: TeamMatchStats; away: TeamMatchStats };
  };
  lineups?: Lineups | null; // present from the /matches/{id} detail route
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
  featured_rank?: number | null; // set = pinned to the featured hero (admin)
  scope?: "fixture" | "competition" | "team" | "player" | "custom";
  competition_id?: string;
  subject_type?: string;
  subject_id?: string;
  resolution_source?: string;
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
  yes_locked: number; // shares already committed to resting SELL orders
  no_locked: number;
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

export interface PrecisionResult {
  market_id: string;
  title: string;
  status: MarketStatus;
  guess: number;
  stake_micro: number;
  score?: number; // 0..1, present once settled
  payout_micro?: number; // present once settled/won
}

export interface ComboResult {
  quote_hash: string;
  status: "accepted" | "won" | "lost" | "void";
  legs: number;
  legDetails: { title: string; outcome: number }[]; // the actual legs
  stake_micro: number;
  payout_micro: number;
  resolve_tx?: string;
}

// One hourly Breaking News item: a REAL Exa article tied to a market, with a
// real Yes% snapshot + momentum delta. Rendered in the markets-index panel.
export interface NewsItem {
  match_id: string;
  market_id: string; // 64-hex
  home: string;
  away: string;
  question: string; // representative market title
  headline: string; // real article title
  summary?: string; // grounded one-sentence condense (or the raw excerpt)
  source?: string; // source domain
  url: string; // real article URL
  published_at?: string;
  yes_pct?: number | null;
  delta?: number | null; // Yes% change vs the previous snapshot
  generated_at: string;
}

// One market comment. wallet is a client-claimed identity (comments are
// unsigned). replies is built client-side by nesting on parent_id.
export interface Comment {
  id: string;
  market_id: string;
  parent_id?: string | null;
  wallet: string;
  avatar_seed: string; // users.avatar_seed, else the wallet
  body: string;
  deleted: boolean;
  edited: boolean;
  like_count: number;
  liked: boolean;
  created_at: string;
  replies?: Comment[];
}

export interface Portfolio {
  balance_micro: number;
  positions: Position[];
  orders: OpenOrder[];
  history: HistoryFill[];
  precision: PrecisionResult[];
  combos: ComboResult[];
}
