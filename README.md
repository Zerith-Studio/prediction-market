# PitchMarket

A non-custodial **football prediction exchange on Solana**, powered end-to-end by
TxODDS/TxLINE live match data. Off-chain central-limit-order-book matching,
on-chain non-custodial settlement.

*Superteam × TxODDS World Cup Hackathon — "Prediction Markets & Settlement" track.*

## Demo & links

[![PitchMarket demo](https://img.youtube.com/vi/NBan_VaTnqY/maxresdefault.jpg)](https://www.youtube.com/watch?v=NBan_VaTnqY)

- **Demo video:** https://www.youtube.com/watch?v=NBan_VaTnqY
- **Live app:** https://pitchmarket.fun
- **Repo:** https://github.com/Zerith-Studio/prediction-market

## What it is

Traders buy and sell YES/NO shares on real World Cup markets — match result,
over/under, both-teams-to-score, clean sheets, half-time lines and more — on a true
central-limit order book. Matching runs off-chain for speed; every fill settles
**non-custodially on Solana (devnet)** via an Anchor program (ed25519-signed orders,
complete-set MINT/MERGE, vault-held collateral), so users keep custody throughout.

The **TxLINE feed drives the whole lifecycle**: it auto-creates markets per fixture,
streams live score / possession / lineups into a match centre, and auto-resolves
markets at half-time and full-time straight from the feed — with winners redeeming
their payout **trustlessly on-chain**.

Also ships:

- **Precision (parimutuel) pools** — closeness-scored numeric markets (e.g. total goals).
- **RFQ parlay combos** — cash-collateralised multi-leg bets with a maker/taker escrow.
- **Market-maker bot** — two-sided liquidity priced off the live TxLINE odds.
- **Live match centre** — real-time score, possession, cards, corners and team sheets.
- **Breaking news (Exa)** + an **AI one-liner ticker**, watchlists, comments, and an admin panel.

## Layout

```
programs/pitchmarket/   E1 — Anchor settlement program (Rust)
backend/                E2 — Go matching engine, crank, feed, REST/WS API
frontend/               Next.js app + Privy embedded Solana wallet
docs/                   ADRs, interface contract, specs (read before coding)
progress.md             where we are right now
```

**Picking the project up?** Start with [`docs/HANDOFF.md`](docs/HANDOFF.md) — what's
proven on devnet, runbooks, invariants, and the remaining punch list. Full design:
`PROJECT_PLAN.md` + `docs/adr/` (0001–0007) + `docs/interface-contract.md` (the E1↔E2
boundary — the source of truth both tracks code against). Current status and blockers
live in [`progress.md`](progress.md) (kept there so there's one place to update — see
[`CLAUDE.md`](CLAUDE.md)).

## Prerequisites

- Rust (stable) + Solana CLI + Anchor CLI (`avm install latest && avm use latest`)
- Go 1.26+
- Node 20+ (frontend)
- Postgres (any; we use Neon) — schema bootstraps itself from `backend/db/schema.sql`

## Build & test

```sh
# E1 — Anchor program
cargo check -p pitchmarket   # fast host check; does NOT prove BPF compiles
cargo test -p pitchmarket    # includes the borsh golden vectors (see below)
anchor build                 # the real gate — see progress.md for current status

# E2 — Go backend
cd backend && go build ./... && go vet ./...
go test -p 1 ./...           # DB-backed tests need DATABASE_URL (or a .env); each run
                             # creates a scratch database and drops it after.
                             # -p 1 serializes packages — parallel packages contend on
                             # the shared Neon endpoint and flake.
```

The Go and Rust suites pin the SAME borsh golden vector
(`backend/internal/models/hash_conformance_test.go` ↔
`programs/pitchmarket/src/sig_verify.rs`). If you touch either encoder, run both.

## Run (dev)

```sh
cp .env.example .env          # fill in DATABASE_URL at minimum
cd backend && DEMO_FIXTURE=demo-final go run ./cmd/server
```

Boots the full stack on `:8080`: REST + `/ws`, Postgres mirror, matching engine,
crank (off-chain mirror mode until `SOLANA_RPC_URL` + `OPERATOR_KEYPAIR` are set, then
it settles real devnet transactions), MM bot, RFQ, precision pools — and with
`DEMO_FIXTURE`, auto-creates the demo match's markets and streams the recorded fixture
(`backend/fixtures/`). Full config reference: `.env.example` and the doc comment in
`backend/cmd/server/main.go`.

```sh
# frontend
cd frontend && npm install && npm run dev   # points at NEXT_PUBLIC_API_URL
```

## Architecture in one breath

TxLINE (on-chain `subscribe` → live SSE feed) → Go engine registers fixtures, creates
markets, and streams live state → traders sign borsh orders (Privy wallet) → off-chain
CLOB matches them → the crank settles each fill on Solana devnet (`settle_match`) →
the feed auto-resolves markets (`resolve_market`) → winners `redeem` on-chain. Postgres
is a soft-lock ledger + read index; **the chain is authoritative for money**.
