-- Postgres schema for the off-chain index (PROJECT_PLAN.md §4).
-- Chain (Anchor program PDAs) is authoritative for money/positions/settlement;
-- this schema is the resting order book, soft-locks, RFQ, precision pools, and a
-- read-cache/index of chain state for fast UI reads.
--
-- Everything is IF NOT EXISTS so store.Bootstrap can run it idempotently on boot.

CREATE TABLE IF NOT EXISTS users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    privy_id    TEXT UNIQUE NOT NULL,
    wallet      TEXT UNIQUE NOT NULL, -- base58 Solana pubkey
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS matches (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    txodds_fixture_id TEXT UNIQUE NOT NULL,
    home              TEXT NOT NULL,
    away              TEXT NOT NULL,
    kickoff_at        TIMESTAMPTZ NOT NULL,
    status            TEXT NOT NULL DEFAULT 'scheduled', -- scheduled|live|finished
    live_state        JSONB NOT NULL DEFAULT '{}'::jsonb
);
-- Team sheets (starting XI + subs) from the TxLINE scores feed. Nullable: only
-- set once the feed delivers lineups. Added post-hoc; safe on existing DBs.
ALTER TABLE matches ADD COLUMN IF NOT EXISTS lineups JSONB;

CREATE TABLE IF NOT EXISTS markets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market_id       BYTEA UNIQUE NOT NULL, -- [u8;32] on-chain market_id
    match_id        UUID NOT NULL REFERENCES matches(id),
    template_key    TEXT NOT NULL,
    type            TEXT NOT NULL CHECK (type IN ('binary', 'precision')),
    title           TEXT NOT NULL,
    rule            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','open','closed','resolving','settled','void')),
    outcome         JSONB,
    chain_condition TEXT, -- base58 Market PDA
    chain_tx        TEXT, -- resolve_market tx sig
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Admin "pin" for the featured hero on the markets index. NULL = not pinned;
-- lower rank = higher priority. Added post-hoc; safe on existing DBs.
ALTER TABLE markets ADD COLUMN IF NOT EXISTS featured_rank INTEGER;

-- Demo mirror of vault USDC balances (micro-USDC). The vault-owned ATAs on chain
-- are authoritative; usdc_locked is the E2 soft-lock (UX only, interface-contract §6.2).
CREATE TABLE IF NOT EXISTS balances (
    wallet          TEXT PRIMARY KEY,
    usdc_available  BIGINT NOT NULL DEFAULT 0 CHECK (usdc_available >= 0),
    usdc_locked     BIGINT NOT NULL DEFAULT 0 CHECK (usdc_locked >= 0)
);

-- order fill-accounting is authoritative on-chain (OrderStatus PDA); `remaining`
-- here mirrors it for book/UX reads and is not the source of truth.
-- `locked` = residual soft-locked collateral attributed to this order:
-- micro-USDC for BUY (price×size+fee at entry), outcome tokens for SELL.
CREATE TABLE IF NOT EXISTS orders (
    order_hash  BYTEA PRIMARY KEY, -- sha256(borsh(Order))
    market_id   BYTEA NOT NULL REFERENCES markets(market_id),
    maker       TEXT NOT NULL,
    outcome     SMALLINT NOT NULL,
    side        SMALLINT NOT NULL,
    price       SMALLINT NOT NULL CHECK (price BETWEEN 1 AND 99),
    size        BIGINT NOT NULL,
    remaining   BIGINT NOT NULL,
    fee_bps     SMALLINT NOT NULL DEFAULT 0,
    expiry      TIMESTAMPTZ,
    salt        BIGINT NOT NULL,
    sig         BYTEA NOT NULL,
    locked      BIGINT NOT NULL DEFAULT 0 CHECK (locked >= 0),
    status      TEXT NOT NULL DEFAULT 'live' CHECK (status IN ('live','matched','cancelled')),
    created_seq BIGSERIAL
);
CREATE INDEX IF NOT EXISTS orders_book_idx ON orders (market_id, outcome, side, price, created_seq)
    WHERE status = 'live';

CREATE TABLE IF NOT EXISTS fills (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market_id   BYTEA NOT NULL REFERENCES markets(market_id),
    taker_hash  BYTEA NOT NULL REFERENCES orders(order_hash),
    maker_hash  BYTEA NOT NULL REFERENCES orders(order_hash),
    price       SMALLINT NOT NULL,
    size        BIGINT NOT NULL,
    match_type  TEXT NOT NULL CHECK (match_type IN ('NORMAL','MINT','MERGE')),
    settle_tx   TEXT, -- null until crank confirms on-chain
    ts          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- mirror of chain balances for fast UI; chain is authoritative.
-- *_locked = tokens soft-locked under live SELL orders (UX only).
CREATE TABLE IF NOT EXISTS positions_cache (
    "user"      TEXT NOT NULL,
    market_id   BYTEA NOT NULL REFERENCES markets(market_id),
    yes         BIGINT NOT NULL DEFAULT 0 CHECK (yes >= 0),
    no          BIGINT NOT NULL DEFAULT 0 CHECK (no >= 0),
    yes_locked  BIGINT NOT NULL DEFAULT 0 CHECK (yes_locked >= 0),
    no_locked   BIGINT NOT NULL DEFAULT 0 CHECK (no_locked >= 0),
    avg_cost    BIGINT NOT NULL DEFAULT 0,
    realized    BIGINT NOT NULL DEFAULT 0, -- micro-USDC: Σ (exec − avg_cost)·size on sells
    PRIMARY KEY ("user", market_id),
    CHECK (yes >= yes_locked),
    CHECK (no >= no_locked)
);

CREATE TABLE IF NOT EXISTS combo_quotes (
    quote_hash  BYTEA PRIMARY KEY, -- sha256(borsh(ComboQuote))
    rfq_id      UUID, -- the RFQ request this quote answers (null = unsolicited)
    maker       TEXT NOT NULL,
    legs        JSONB NOT NULL, -- [{market_id, outcome}]
    stake       BIGINT NOT NULL,
    payout      BIGINT NOT NULL,
    expiry      TIMESTAMPTZ,
    salt        BIGINT NOT NULL,
    sig         BYTEA NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','accepted','expired'))
);

-- RFQ requests: a taker asks for quotes on a leg combination (ADR 0004).
CREATE TABLE IF NOT EXISTS combo_rfqs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    taker       TEXT NOT NULL,
    legs        JSONB NOT NULL, -- [{market_id, outcome}]
    stake       BIGINT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','quoted','accepted','expired')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS combo_escrows (
    quote_hash  BYTEA PRIMARY KEY REFERENCES combo_quotes(quote_hash),
    taker       TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('accepted','won','lost','void')),
    accept_tx   TEXT,
    resolve_tx  TEXT
);

CREATE TABLE IF NOT EXISTS precision_entries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market_id   BYTEA NOT NULL REFERENCES markets(market_id),
    "user"      TEXT NOT NULL,
    guess       NUMERIC NOT NULL,
    stake       BIGINT NOT NULL,
    score       NUMERIC, -- null until settled; = 1/(1+|guess-actual|/s)^k, ADR 0006
    payout      BIGINT,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (market_id, "user") -- one entry per (user, market) — anti-gaming, ADR 0006
);

CREATE TABLE IF NOT EXISTS oneliners (
    market_id     BYTEA NOT NULL REFERENCES markets(market_id),
    lines         JSONB NOT NULL,
    generated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market_id, generated_at)
);

-- Hourly breaking-news, one row per match per generation: a REAL Exa article
-- (headline/source/url/published_at) tied to a representative market, with a
-- Yes% snapshot + momentum delta from real odds. Never fabricated.
CREATE TABLE IF NOT EXISTS breaking_news (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id      UUID NOT NULL REFERENCES matches(id),
    market_id     BYTEA NOT NULL REFERENCES markets(market_id),
    headline      TEXT NOT NULL,          -- real Exa article title
    summary       TEXT,                   -- optional Gemini one-sentence condense (grounded)
    source        TEXT,                   -- source domain (e.g. goal.com)
    url           TEXT NOT NULL,          -- real article URL
    published_at  TIMESTAMPTZ,            -- article publish time
    yes_pct       INTEGER,                -- market Yes% at generation
    delta         INTEGER,                -- yes_pct change vs the previous snapshot
    generated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS breaking_news_gen_idx ON breaking_news (generated_at DESC);
CREATE INDEX IF NOT EXISTS breaking_news_market_idx ON breaking_news (market_id, generated_at DESC);

-- Per-market comment threads. wallet is the base58 poster (client-claimed —
-- comments are unsigned, unlike orders). parent_id (nullable) = a reply.
-- deleted_at soft-deletes so replies keep their thread structure.
CREATE TABLE IF NOT EXISTS comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market_id   BYTEA NOT NULL REFERENCES markets(market_id),
    parent_id   UUID REFERENCES comments(id),
    wallet      TEXT NOT NULL, -- base58 Solana pubkey (claimed identity)
    body        TEXT NOT NULL,
    deleted_at  TIMESTAMPTZ,
    edited_at   TIMESTAMPTZ,   -- set when the author edits (shows "(edited)")
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS comments_market_idx ON comments (market_id, created_at);
CREATE INDEX IF NOT EXISTS comments_wallet_idx ON comments (wallet, created_at DESC);

-- One like per (comment, wallet); toggled by insert/delete.
CREATE TABLE IF NOT EXISTS comment_likes (
    comment_id  UUID NOT NULL REFERENCES comments(id),
    wallet      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (comment_id, wallet)
);
CREATE INDEX IF NOT EXISTS comment_likes_comment_idx ON comment_likes (comment_id);

-- post-hoc migrations (idempotent) — columns added after first bootstrap
ALTER TABLE positions_cache ADD COLUMN IF NOT EXISTS realized BIGINT NOT NULL DEFAULT 0;
-- Per-user avatar seed (deterministic gradient, @outpacelabs/avatars). Defaults
-- to the wallet; kept as a column so it can be customized later.
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_seed TEXT;
UPDATE users SET avatar_seed = wallet WHERE avatar_seed IS NULL;
-- comments existed before author-edit; add edited_at on existing DBs.
ALTER TABLE comments ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;
