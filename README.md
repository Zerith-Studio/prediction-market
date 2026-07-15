# PitchMarket

Superteam × TxODDS World Cup Hackathon — "Prediction Markets & Settlement" track.

**Current status, blockers, and next actions live in [`progress.md`](progress.md).**
Read it before you start. Update it when you finish — see [`CLAUDE.md`](CLAUDE.md).

Full design: `PROJECT_PLAN.md` + `docs/adr/` (0001–0007) + `docs/interface-contract.md`
(the E1↔E2 boundary — read this first, it's the source of truth both tracks code against).

## Layout

```
programs/pitchmarket/   E1 — Anchor settlement program
backend/                E2 — Go matching engine, crank, feed, API
docs/                   ADRs, interface contract, specs (read before coding)
progress.md             where we are right now
```

## Prerequisites

- Rust (stable) + Solana CLI + Anchor CLI (`avm install latest && avm use latest`)
- Go 1.26+
- Postgres (any; we use Neon) — schema bootstraps itself from `backend/db/schema.sql`

## Build & test

```sh
# E1 — Anchor program
cargo check -p pitchmarket   # fast host check; does NOT prove BPF compiles
cargo test -p pitchmarket    # includes the borsh golden vectors (see below)
anchor build                 # the real gate — currently FAILING, see progress.md §4

# E2 — Go backend
cd backend && go build ./... && go vet ./...
go test -p 1 ./...           # DB-backed tests need DATABASE_URL (or a .env); each run
                             # creates a scratch database and drops it after.
                             # -p 1 serializes packages — parallel packages contend on
                             # the shared Neon endpoint and flake (seen: 8/20 seeding)
```

The Go and Rust suites pin the SAME borsh golden vector
(`backend/internal/models/hash_conformance_test.go` ↔
`programs/pitchmarket/src/sig_verify.rs`). If you touch either encoder, run both.

## Run (E2, dev)

```sh
cp .env.example .env         # fill in DATABASE_URL at minimum
cd backend && DEMO_FIXTURE=demo-final go run ./cmd/server
```

Boots the full stack on `:8080`: REST + `/ws`, Postgres mirror, matching engine,
crank (off-chain mirror mode until `SOLANA_RPC_URL` + `OPERATOR_KEYPAIR` are set),
MM bot, RFQ, precision pools — and with `DEMO_FIXTURE`, auto-creates the demo
match's markets and streams the recorded fixture (`backend/fixtures/`).
Full config reference: `.env.example` and the doc comment in `backend/cmd/server/main.go`.

## State of the scaffold

Per-component status, the current blocker, and next actions: **[`progress.md`](progress.md)**.
Kept there rather than here so there's exactly one place to update.
