-- Postgres schema for the off-chain index (PROJECT_PLAN.md §4).
-- Chain (Anchor program PDAs) is authoritative for money/positions/settlement;
-- this schema is the resting order book, soft-locks, RFQ, precision pools, and a
-- read-cache/index of chain state for fast UI reads.

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    privy_id    TEXT UNIQUE NOT NULL,
    wallet      TEXT UNIQUE NOT NULL, -- base58 Solana pubkey
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE matches (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    txodds_fixture_id TEXT UNIQUE NOT NULL,
    home              TEXT NOT NULL,
    away              TEXT NOT NULL,
    kickoff_at        TIMESTAMPTZ NOT NULL,
    status            TEXT NOT NULL DEFAULT 'scheduled', -- scheduled|live|finished
    live_state        JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE markets (
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

-- order fill-accounting is authoritative on-chain (OrderStatus PDA); `remaining`
-- here mirrors it for book/UX reads and is not the source of truth.
CREATE TABLE orders (
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
    status      TEXT NOT NULL DEFAULT 'live' CHECK (status IN ('live','matched','cancelled')),
    created_seq BIGSERIAL
);
CREATE INDEX orders_book_idx ON orders (market_id, outcome, side, price, created_seq)
    WHERE status = 'live';

CREATE TABLE fills (
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
CREATE TABLE positions_cache (
    "user"      TEXT NOT NULL,
    market_id   BYTEA NOT NULL REFERENCES markets(market_id),
    yes         BIGINT NOT NULL DEFAULT 0,
    no          BIGINT NOT NULL DEFAULT 0,
    avg_cost    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY ("user", market_id)
);

CREATE TABLE combo_quotes (
    quote_hash  BYTEA PRIMARY KEY, -- sha256(borsh(ComboQuote))
    maker       TEXT NOT NULL,
    legs        JSONB NOT NULL, -- [{market_id, outcome}]
    stake       BIGINT NOT NULL,
    payout      BIGINT NOT NULL,
    expiry      TIMESTAMPTZ,
    salt        BIGINT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','accepted','expired'))
);

CREATE TABLE combo_escrows (
    quote_hash  BYTEA PRIMARY KEY REFERENCES combo_quotes(quote_hash),
    taker       TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('accepted','won','lost','void')),
    accept_tx   TEXT,
    resolve_tx  TEXT
);

CREATE TABLE precision_entries (
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

CREATE TABLE oneliners (
    market_id     BYTEA NOT NULL REFERENCES markets(market_id),
    lines         JSONB NOT NULL,
    generated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market_id, generated_at)
);
