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
- Go 1.22+
- Postgres (schema: `backend/db/schema.sql`)

## Build

```sh
# E1 — Anchor program
cargo check -p pitchmarket   # fast host check; does NOT prove BPF compiles
anchor build                 # the real gate — currently FAILING, see progress.md §4

# E2 — Go backend
cd backend && go build ./... && go vet ./...
```

## Run (E2, dev)

```sh
cd backend && go run ./cmd/server
```
Serves `POST /orders`, `GET /markets/{id}/book`, `GET /healthz` on `:8080`.
Not yet wired: Postgres persistence, crank → devnet submission, feed provider selection.
See TODOs in `backend/internal/crank/crank.go` and `backend/internal/api/api.go`.

## State of the scaffold

Per-component status, the current blocker, and next actions: **[`progress.md`](progress.md)**.
Kept there rather than here so there's exactly one place to update.
