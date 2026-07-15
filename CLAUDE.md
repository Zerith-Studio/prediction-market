# PitchMarket — working agreement

Football prediction exchange on Solana. Off-chain matching, on-chain non-custodial
settlement. Superteam × TxODDS hackathon; internal deadline **2026-07-15**.

## Read these first, in this order

1. **`progress.md`** — what actually works today, current blockers, next actions.
   **Always read this before starting work, and always update it when you finish.**
2. `docs/interface-contract.md` — the E1↔E2 boundary. Frozen. Source of truth for
   account layouts, instruction args, tx layouts, and the REST/WS surface.
3. `PROJECT_PLAN.md` — what we're building, the schedule, and the cut order.
4. `docs/adr/0001`–`0007` — pinned decisions. Don't re-litigate; read and move on.

## The one rule about progress.md

**If you change code, update `progress.md` in the same commit.** Update the component
table, tick anything in the Jul 11 checklist you actually made green, and append a
Changelog row.

Status marks mean exactly this:

- ✅ **done & verified** — you ran it and watched it work.
- 🟡 **written but unverified** — it compiles; it has never executed.
- 🔴 **not started / blocked**.

Never mark something ✅ because it "should" work. On a 5-day clock, a table full of
optimistic ✅s is worse than no table. If you didn't verify it, say what's unverified.

## Repo layout

```
programs/pitchmarket/   E1 — Anchor settlement program (Rust)
backend/                E2 — Go matching engine, crank, feed, API
docs/                   ADRs, interface contract, specs
```

## Build & verify

```sh
cargo check -p pitchmarket           # fast host check (does NOT prove BPF compiles)
cargo test -p pitchmarket            # includes the borsh golden vectors
anchor build                         # the real check — currently FAILING, see progress.md §4
cd backend && go build ./... && go vet ./...
go test -p 1 ./...                   # -p 1 matters: parallel packages contend on Neon and flake
```

`cargo check` passing means very little. **`anchor build` is the gate**, and deployment to
devnet is the only thing that proves an instruction works.

Go DB tests need `DATABASE_URL` (auto-read from the repo-root `.env`, which is gitignored —
copy `.env.example`). Each test run creates a scratch database and drops it after; they are
slow (~300ms/statement to Neon), so scope test runs while iterating
(`go test ./internal/matching/` is instant; `./internal/api/` is ~3 min).

## Things that will bite you

- **Borsh encoding is duplicated across languages.** `backend/internal/models/hash.go`
  `BorshOrder()` and `programs/pitchmarket/src/sig_verify.rs` `borsh_order()` must stay
  byte-identical. Golden vectors pin both sides
  (`hash_conformance_test.go` ↔ `sig_verify.rs` tests) — if you touch either encoder,
  update the SAME vector in both tests and run both suites.
- **`settle_match` requires an exact 3-instruction transaction** (ed25519 taker, ed25519
  maker, then `settle_match`) — interface-contract §6.5. `crank.TxBuilder` is the only
  place that builds it; `builder_test.go` re-implements the on-chain byte checks.
- **On-chain is authoritative for money.** Postgres is an index and a soft-lock store.
  `orders.remaining` is a mirror of on-chain `OrderStatus`, never the truth.
- **MINT/MERGE money moves at each order's OWN limit price** (lib.rs `settle_mint`/
  `settle_merge`), not at the engine's fill price — only NORMAL uses `fill_price`. The
  store's `legDeltaFor` mirrors this exactly; keep them in lockstep.
- **pgx runs in simple query protocol** (Neon's pooler breaks prepared statements). That
  means: explicit `::bigint` casts in SQL arithmetic on parameters, JSONB params passed
  as Go `string` (never `[]byte` — that becomes bytea hex), and `uint64` salts scanned
  via `int64` (BIGINT is signed).
- **All order flow goes through `internal/exchange`** — API and MM bot alike. Don't
  submit to the matching book or the store directly; you'd skip sig verification,
  soft-locks, the crank, or WS events.
- **The program keypair is gitignored** and lives on one machine. See progress.md §6.

## Commits

Short imperative subject, `type(scope): summary`. Do not add `Co-Authored-By` trailers or
tool-attribution footers to commit messages or PR bodies.
