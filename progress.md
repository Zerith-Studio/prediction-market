# PitchMarket — Progress Log

**This file is the single source of truth for "where are we right now."**
`PROJECT_PLAN.md` says what we're building and why. `docs/interface-contract.md` is the
E1↔E2 boundary. **This file says what actually works today.**

> **Rule for both engineers:** if you change code, change this file in the same commit.
> Update the component table, tick the checklist, and add a Changelog row. A claim in this
> file means *"I ran it and saw it work"* — not *"I wrote it and it should work."*
> If you didn't verify it, mark it 🟡 and say what's unverified.

Legend: ✅ done & verified · 🟡 written but unverified · 🔴 not started / blocked

---

## 1. Status at a glance — 2026-07-11 (Day 7 of 11, Go/No-Go day)

| | |
|---|---|
| Deadline | **2026-07-15** (internal) · judged by 2026-07-29 |
| Days left | **4** |
| Go/No-Go gate | **TODAY EOD** — binary settlement end-to-end on devnet (PROJECT_PLAN §7) |
| E2 backend | ✅ **complete and tested end-to-end** (off-chain mirror mode) |
| **Top blocker** | **unchanged: `anchor build` fails → nothing deployed → no on-chain settlement.** See §4. |

**Honest summary.** E2 is done: the full backend — matching (all three match types),
Postgres mirror with soft-locks, ed25519 intake verification, crank tx builder producing
the exact §6.5 layout, WS hub, RFQ combos, precision pools, MM bot, feed lifecycle,
one-liners, chain index — builds, and passes an end-to-end suite against real Postgres
(signed orders over HTTP → MINT fill → crank capture → WS events → portfolio →
resolution → payout, plus the revert→reconcile path). **What it is NOT: on-chain.** The
crank runs in off-chain mirror mode because the program still won't compile to BPF (§4).
The moment `anchor build` is green and the program is deployed, settlement flips on by
setting two env vars — no code changes expected on the E2 side.

**Gate call (per PROJECT_PLAN §7):** if E1's toolchain isn't unblocked today, the demo
falls back to "everything real except the on-chain leg" — which is now a much softer
landing than it was yesterday, but it is still a **No-Go** on the trustless story until
§4 closes.

---

## 2. E1 — Anchor program (`programs/pitchmarket`)

Verified with `cargo check` + `cargo test -p pitchmarket` (host target; includes the
borsh golden vectors). **Not** verified with `anchor build` / on devnet — see §4.

| Instruction | State | Notes |
|---|---|---|
| `initialize_market` | 🟡 | Market PDA + 2 outcome mints |
| `init_vault` / `deposit` | 🟡 | Vault PDA custody |
| `settle_match` NORMAL / MINT / MERGE | 🟡 | collateral-pool CTF model |
| `cancel_order` / `resolve_market` (tier-a) / `redeem` | 🟡 | |
| `sig_verify::verify_order_signature` | 🟡 | implemented; **borsh encoding now pinned by golden vectors on both sides** ✅ |
| `combo_accept` / `resolve_combo` | 🔴 | typed stubs (E2 runs combos off-chain meanwhile) |
| Oracle tier b / d | 🔴 | gated on TxODDS reply |

**Program ID** `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs`. Keypair still gitignored
on one machine — open decision §6.1.

---

## 3. E2 — Go backend (`backend/`) — 2026-07-11 rebuild

Verified with `go build ./... && go vet ./... && go test -p 1 -count=1 ./...` against a
real Postgres (Neon; each test run creates and drops a scratch database).

| Package | State | Verified how |
|---|---|---|
| `matching` | ✅ | Unified ladder: NORMAL + **MINT** (two BUYs ≥100) + **MERGE** (two SELLs ≤100), price-time priority across both populations, cancel, expiry, replay-reject, revert `Unfill`, book snapshots. 15 unit tests. |
| `models` | ✅ | borsh + sha256 hashing, ed25519 sign/verify, fee/cost formulas, base58/hex wire helpers. **Golden vectors pinned Go↔Rust** (`hash_conformance_test.go` ↔ `sig_verify.rs tests`) — the §5 drift risk is now guarded on both sides. |
| `store` (new) | ✅ | Full Postgres layer: orders + soft-locks (BUY locks USDC, SELL locks tokens — no naked shorts), fills mirroring **lib.rs money movement exactly** (NORMAL at fill price, MINT/MERGE at own limits), RevertFill, balances, positions, combos, precision, oneliners. Integration-tested against Neon. |
| `exchange` (new) | ✅ | The trading core: sig verify → soft-lock → match → mirror → crank → WS. Both API and bot submit through it. Revert→reconcile (§6.2 IC) proven: reverted fill restores balances, book, and re-crossability (`TestRevertReconcilesEverywhere`). |
| `crank` | ✅ | **Builds the exact §6.5 3-instruction tx** (ed25519 taker ‖ ed25519 maker ‖ settle_match with anchor discriminator + 16 accounts in lib.rs order). Test re-implements sig_verify.rs's byte checks in Go and runs them against built instructions. RPC submitter written 🟡 (needs devnet). Off-chain mirror mode is the default until deploy. |
| `ws` | ✅ | Hub with the six pinned events; slow-client drop; tested with live WS clients. |
| `api` | ✅ | Full REST surface (orders, markets, book, fills, combos, precision, portfolio, balance, deposit, settlement, oneliners) + `/ws`. **End-to-end suite over real HTTP**: deposit → signed orders → MINT fill → crank capture → WS fill/book events → Postgres rows → portfolio → resolve → settlement endpoint. Bad sig → 401, replay → 409, double-accept → 409, post-kickoff precision → 410. |
| `rfq` (new) | ✅ | Combo lifecycle with **mutex groups** (home_win+draw rejected, home_win+over2.5 allowed), sig + expiry + leg-match checks, single-use accept, resolve sweep reading the same market outcomes binary settlement writes (ADR 0004 seam). On-chain `combo_accept` behind an interface (Noop until E1 lands it). |
| `precision` | ✅ | In store + lifecycle: kickoff-lock (entry after kickoff → rejected), one-entry-per-wallet, σ-normalized score k=2, pool payout ±3 micro dust, rake, VOID refunds. ADR 0006 end-to-end. |
| `mmbot` (new) | ✅ | Two-sided **MINT liquidity** (both quotes are BUYs — bot needs only USDC), re-quote on fair-price ticks, RFQ quoting per the §5 formula (verified ≈ expected payout), exposure cap, crowd-seeding N distinct persona wallets. |
| `lifecycle` (new) | ✅ | Fixture registration → 7 template markets auto-created → replay feed drives match_state/odds/kickoff-lock → full-time resolves **all** templates correctly from the score (2-1 ⇒ home_win yes, draw no, away_win no, over2.5 yes, btts yes), settles precision, sweeps combos. Balances verified to the micro. Abandoned match → everything VOID + refunds. |
| `feed` | ✅ | `replay` drives the whole lifecycle test; `txodds` SSE client skeleton tested against a fake SSE server (auth header, garbage-frame tolerance) — real endpoint shapes still pending TxODDS reply. |
| `oneliner` | ✅ | Claude Messages API client (no SDK dep) behind a Generator seam; 2-min ticker; only live matches generate; tested with a fake generator. Real API 🟡 (needs ANTHROPIC_API_KEY at runtime). |
| `index` (new) | ✅ | OrderStatus mirror processor (chain wins) tested with fake source; RPC poller written 🟡 (needs deployed program). |
| `cmd/server` | ✅ | Full wiring: env config (+ .env), graceful shutdown, `DEMO_FIXTURE` auto-registers + streams the recorded match, bot funding + pool seeding at boot. See `.env.example`. |

**Test-suite caveats:** DB-backed tests are slow (~300ms RTT to Neon per statement) and
**network-flaky**: parallel packages contend on the shared endpoint (seen: 8/20 persona
seeding), and even serialized runs occasionally hit Neon DNS/TLS timeouts mid-suite —
a failed package that shows `dial error`/`no such host` is the network, not the code;
re-run it. Use `go test -p 1 ./...`. Two knowns worth recording: pgx simple-protocol
needs explicit `::bigint` casts in SQL arithmetic and JSONB params passed as strings;
`uint64` salts round-trip through BIGINT as negative — scan via `int64`.

---

## 4. 🔴 BLOCKER — `anchor build` does not compile (unchanged from 2026-07-10)

Root cause: `cargo-build-sbf` uses platform-tools v1.43 (rustc/cargo 1.79); Anchor
0.31.1's transitive deps now need `edition2024` (cargo ≥1.85). The local
`agave-install init 2.3.13` claimed success but `active_release` still points at 2.1.0.
Full diagnosis + ordered next steps in yesterday's entry (see git history of this file
§4). **Nothing on deploy → settle → resolve → redeem can start until this is green.**

New since yesterday: the E2 side of that path is finished and tested — crank tx layout,
RPC submitter, chain index are all written and waiting. Remaining once `anchor build`
works: deploy, flip `SOLANA_RPC_URL`/`OPERATOR_KEYPAIR` in env, run one real settle.

---

## 5. Definition of done for the Go/No-Go (today EOD)

- [ ] `anchor build` produces a `.so` ← **the only line stopping everything below**
- [ ] program deploys to devnet at the pinned ID
- [x] `crank.Submitter` implemented (RPC submitter written; fake-verified)
- [x] crank builds the exact §6.5 3-instruction tx (byte-verified in tests)
- [x] `models.OrderHash` borsh == `sig_verify.rs` borsh (golden vectors both sides)
- [x] signed order → matched → fill produced and mirrored (HTTP e2e, off-chain mode)
- [ ] …and that `settle_match` lands on **devnet**
- [ ] `resolve_market` (tier-a) → `redeem` → user's USDC moves **on-chain**

---

## 6. Open decisions

| # | Decision | Owner | Status |
|---|---|---|---|
| 1 | Commit `pitchmarket-keypair.json` or share out of band? | both | **open — blocks deploy** |
| 2 | Oracle tier for demo: a (operator) vs d (TxODDS signed) | E1 | open, gated on TxODDS reply |
| 3 | TxODDS signed-data email sent? | — | **still unknown — confirm** |
| 4 | ~~Postgres vs in-memory~~ | E2 | **CLOSED 2026-07-11: Postgres (Neon), wired + tested** |
| 5 | Combos on-chain (`combo_accept`) vs off-chain for demo | E1 | E2 ships either way (interface seam); default off-chain per cut plan |

---

## 7. Next actions

**E1 (only thing that matters):** unblock `anchor build` (§4) → deploy → run one real
`settle_match` through the already-built crank. Then `combo_accept`/`resolve_combo`.

**E2:** frontend (Next.js + Privy) is now the entire remaining scope — the backend
surface it consumes is live and documented in `backend/internal/api/api.go`.
Run locally: `DEMO_FIXTURE=demo-final go run ./cmd/server` (see `.env.example`).

**Both:** decision #1 (keypair) today; confirm #3 (TxODDS email).

---

## 8. Housekeeping / paper cuts

- Neon DATABASE_URL lives in the gitignored `.env` (`.env.example` documents the shape).
  The URL was shared in chat — **treat it as compromised-ish; rotate after the hackathon.**
- `docs/interface-contract.md` §6.5-above-§6 ordering still unfixed.
- DB tests: `go test -p 1` (see §3 caveats).
- `orders.locked` column added to schema (per-order residual soft-lock accounting);
  `balances` and `combo_rfqs` tables added; schema is now idempotent (IF NOT EXISTS).

---

## 9. Changelog

Newest first. One row per meaningful change. **Append here in the same commit as the code.**

| Date | Who | What changed | Verified how |
|---|---|---|---|
| 2026-07-11 | Ashish | **E2 backend complete**: matching MINT/MERGE + cancel/expiry/unfill; Postgres store (orders/soft-locks/fills/balances/positions/combos/precision/oneliners) on Neon; exchange core with revert→reconcile; crank §6.5 tx builder + RPC submitter + off-chain mode; WS hub; full REST API; RFQ with mutex groups; precision (kickoff-lock, σ-score, rake, VOID); MM bot (MINT liquidity, RFQ formula, crowd-seed); lifecycle (auto markets, feed→resolution); txodds SSE skeleton; Claude one-liners; chain-index poller; cmd/server wiring; borsh golden vectors Go↔Rust; demo replay fixture | `go build` ✅ `go vet` ✅ · every test package green against real Postgres (11 pkgs incl. HTTP e2e + WS + revert path); two packages hit Neon network timeouts in the one full serialized run and passed on immediate re-run (§3 caveat) · `cargo test -p pitchmarket` ✅ 4/4 · `anchor build` still ❌ (§4) |
| 2026-07-10 | Ashish | Added `progress.md` + `CLAUDE.md`; trimmed stale README status; untracked `.DS_Store`; committed the E1/E2 scaffold | `cargo check` ✅ · `go build ./... && go vet ./...` ✅ · `anchor build` ❌ (§4) |
| 2026-07-09 | E1 | Implemented `sig_verify::verify_order_signature`; pinned settle_match tx layout in interface-contract §6.5 | `cargo check` only — never executed |
| 2026-07-08 | E1/E2 | Anchor program scaffold; Go matching engine, crank skeleton, order API, replay feed, Postgres schema | `cargo check` · `go build` |
