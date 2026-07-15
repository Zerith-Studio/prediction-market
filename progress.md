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

## 1. Status at a glance — 2026-07-12 (Day 8 of 11)

| | |
|---|---|
| Deadline | **2026-07-15** (internal) · judged by 2026-07-29 |
| Days left | **3** |
| E1 program | ✅ **DEPLOYED TO DEVNET** at the pinned ID; full lifecycle proven on devnet |
| E2 backend | ✅ complete; **Go crank settled a real match ON DEVNET** (v0+ALT) |
| **Top blocker** | **frontend not started** — everything else on the floor is done |

**Honest summary.** Both halves of the trustless floor now work — separately. E1: the
§4 toolchain blocker is fixed, the program compiles to BPF, and the full lifecycle
(`initialize_market → deposit → settle_match (NORMAL/MINT/MERGE) → cancel fail-closed →
resolve_market → redeem`) runs green on `solana-test-validator` with balance assertions;
`sig_verify` executed for real. E2: the whole backend (matching, Postgres mirror,
crank builder, API/WS, RFQ, precision, bot, lifecycle) passes an HTTP end-to-end suite
against real Postgres. **Not yet done: the two halves have never met on devnet.** The
crank still settles in off-chain mirror mode, and E1's tx-size finding means the Go
crank needs a v0 + Address Lookup Table rework before it can submit for real. That
join — Go crank → deployed program on devnet — is the last hard step of the floor.

---

## 2. E1 — Anchor program (`programs/pitchmarket`)

Builds to BPF (`cargo build-sbf`, see §4). ✅ marks below = **exercised on a local
validator** via `tests/` (`npm test`), 8/8 passing. Not yet run on devnet.

| Instruction | State | Notes |
|---|---|---|
| `initialize_market` | ✅ | Market PDA + 2 outcome mints + pool. localnet |
| `init_vault` / `deposit` | ✅ | Vault PDA custody; USDC moved into vault ATA. localnet |
| `settle_match` NORMAL | ✅ | peer-to-peer USDC↔shares swap. localnet |
| `settle_match` MINT | ✅ | opposite-outcome buys mint a complete set into the pool. localnet |
| `settle_match` MERGE | ✅ | opposite-outcome sells burn a complete set, release pooled collateral. localnet |
| `cancel_order` | ✅ | maker cancels; a later settle of that order fails closed (`OrderClosed`). localnet |
| `resolve_market` | ✅ | **tier-a only** (operator-signed); localnet. Tiers b/d not started |
| `redeem` | ✅ | burns winning shares, pays 1:1 from pool. localnet |
| `sig_verify::verify_order_signature` | ✅ | ed25519 sysvar introspection **executed for real** in settle_match. TS borsh == `sig_verify.rs::borsh_order` proven at runtime; Go borsh pinned by golden vectors (§3) |
| `combo_accept` | 🔴 | typed stub |
| `resolve_combo` | 🔴 | typed stub |
| VOID path | 🔴 | |
| Oracle tier b (challenge) / d (TxODDS sig) | 🔴 | gated on TxODDS reply |

**Two program changes were needed to build & run** (PR #3):
- `SettleMatch` accounts are now `Box`ed — the context otherwise overflowed the 4KB BPF
  stack frame by 64 bytes (only surfaces at BPF build, not `cargo check`). ABI-unchanged:
  account order, args, and semantics are identical, so the Go crank needs no change here.
- `Cargo.toml` gained the `idl-build` feature (was missing; blocked IDL generation).

**⚠️ Cross-track finding:** the settle_match tx (2 ed25519 precompiles + the
`settle_match` ix) is **1453 bytes > the 1232 legacy limit**. It only fits as a **v0 tx
with an Address Lookup Table** — `tests/lifecycle.ts` shows how. **The Go crank
(`crank.TxBuilder`/`RPCSubmitter`) currently builds a legacy tx and must be reworked
before devnet settlement** — tracked in §5/§7.

**Program ID** `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs` — pinned in `declare_id!`
and `Anchor.toml`.

⚠️ **The keypair at `target/deploy/pitchmarket-keypair.json` is gitignored and exists on
one machine only.** Both engineers can *build* this program ID, but only whoever holds
that file can *deploy* to it. **Decide before deploy day:** `git add -f` it (fine for a
devnet hackathon) or share out of band. If it's lost, the program ID changes everywhere.

---

## 3. E2 — Go backend (`backend/`) — completed 2026-07-11

Verified with `go build ./... && go vet ./... && go test -p 1 -count=1 ./...` against a
real Postgres (Neon; each test run creates and drops a scratch database).

| Package | State | Verified how |
|---|---|---|
| `matching` | ✅ | Unified ladder: NORMAL + **MINT** (two BUYs ≥100) + **MERGE** (two SELLs ≤100), price-time priority across both populations, cancel, expiry, replay-reject, revert `Unfill`, book snapshots. 15 unit tests. |
| `models` | ✅ | borsh + sha256 hashing, ed25519 sign/verify, fee/cost formulas, base58/hex wire helpers. **Golden vectors pinned Go↔Rust** (`hash_conformance_test.go` ↔ `sig_verify.rs` tests) — with E1's runtime TS↔Rust proof, all three encoders are now cross-checked. |
| `store` | ✅ | Full Postgres layer: orders + soft-locks (BUY locks USDC, SELL locks tokens — no naked shorts), fills mirroring **lib.rs money movement exactly** (NORMAL at fill price, MINT/MERGE at own limits), RevertFill, balances, positions, combos, precision, oneliners. Integration-tested against Neon. |
| `exchange` | ✅ | The trading core: sig verify → soft-lock → match → mirror → crank → WS. Both API and bot submit through it. Revert→reconcile (§6.2 IC) proven end-to-end. |
| `crank` | 🟡→rework | Builds the §6.5 3-instruction tx byte-verified against sig_verify.rs checks; RPC submitter written. **BUT: emits a LEGACY tx — E1's §2 finding (1453 B > 1232) means it must be reworked to a v0 tx + Address Lookup Table before devnet.** Off-chain mirror mode is the default until then. |
| `ws` | ✅ | Hub with the six pinned events; slow-client drop; tested with live WS clients. |
| `api` | ✅ | Full REST surface + `/ws`. **End-to-end suite over real HTTP**: deposit → signed orders → MINT fill → crank capture → WS events → Postgres rows → portfolio → resolve → settlement endpoint. Bad sig → 401, replay → 409, double-accept → 409, post-kickoff precision → 410. |
| `rfq` | ✅ | Combo lifecycle with **mutex groups**, sig + expiry + leg-match checks, single-use accept, resolve sweep reading the same market outcomes binary settlement writes (ADR 0004 seam). On-chain `combo_accept` behind an interface (Noop until E1 lands it). |
| `precision` | ✅ | Kickoff-lock, one-entry-per-wallet, σ-normalized score k=2, pool payout ±3 micro dust, rake, VOID refunds. ADR 0006 end-to-end. |
| `mmbot` | ✅ | Two-sided **MINT liquidity** (both quotes are BUYs — bot needs only USDC), re-quote on fair ticks, RFQ quoting per the §5 formula, exposure cap, crowd-seeding distinct persona wallets. |
| `lifecycle` | ✅ | Fixture → 7 template markets → replay feed → full-time resolves all templates correctly, settles precision, sweeps combos; balances verified to the micro. Abandoned match → VOID + refunds. |
| `feed` | ✅ | `replay` drives the lifecycle test; `txodds` SSE skeleton tested against a fake server — real endpoint shapes pending TxODDS reply. |
| `oneliner` | ✅ | Claude Messages API client behind a Generator seam; 2-min ticker; tested with fake. Real API 🟡 (needs key at runtime). |
| `index` | ✅ | OrderStatus mirror processor (chain wins) tested with fake source; RPC poller written 🟡 (needs deployed program). |
| `cmd/server` | ✅ | Full wiring, env config (+ .env), graceful shutdown, `DEMO_FIXTURE` demo mode. See `.env.example`. |

**Test-suite caveats:** DB-backed tests are slow (~300ms RTT to Neon per statement) and
**network-flaky**: parallel packages contend on the shared endpoint, and even serialized
runs occasionally hit Neon DNS/TLS timeouts mid-suite — a failed package showing
`dial error`/`no such host` is the network, not the code; re-run it. Use
`go test -p 1 ./...`. Knowns: pgx simple-protocol needs explicit `::bigint` casts and
JSONB params as strings; `uint64` salts scan via `int64`.

---

## 4. ✅ RESOLVED — program now builds to BPF (fixed 2026-07-12, PR #3)

The `edition2024` failure was caused entirely by an **old platform-tools** (v1.43 →
rustc/cargo 1.79), which can't parse deps that Anchor 0.31.1 pulls. The fix is a
**modern Agave install** (platform-tools **v1.54** / rustc 1.89).

**How it was fixed (reproducible on a fresh machine):**
1. Install Rust (`rustup`), Agave CLI 4.1.1 (`release.anza.xyz/stable/install` →
   platform-tools **v1.54**), and Anchor via avm.
2. **Build with `cargo build-sbf` from the program dir, NOT `anchor build`.** The crux:
   `anchor build` (and `anchor idl build`) runs a toolchain override that **re-installs
   Solana 2.1.0 and repoints `active_release` back to the old v1.43 tools** — re-breaking
   the build (this is the "inconsistent state" the 07-10 note hit). After any `anchor`
   invocation, repoint:
   ```sh
   cd ~/.local/share/solana/install
   ln -sfn "$PWD/releases/stable-<hash>/solana-release" active_release && hash -r
   cargo-build-sbf --version   # must read platform-tools v1.54 / rustc 1.89
   ```
3. `cd programs/pitchmarket && cargo build-sbf` → `target/deploy/pitchmarket.so` (419 KB).

**IDL:** `anchor idl build` chokes on the two `ostatus` PDAs whose seed is a function
call on an instruction arg (`sig_verify::order_hash(&taker)`). Workaround: temporarily
swap those seeds for a plain arg field to emit the IDL, then restore. The runtime `.so`
keeps the real hash-based seeds.

**Verify on a second machine** — fixed on one clean box; E2 should reproduce.

---

## 5. Definition of done for the floor (one match, one binary market, fully trustless)

- [x] program compiles to BPF — `cargo build-sbf` (§4); 419 KB `.so`
- [x] full lifecycle green **on localnet**: signed order → settle_match (all 3 paths) →
      cancel fail-closed → resolve_market → redeem, balances asserted (`npm test` 8/8)
- [x] borsh conformance: TS↔Rust proven at runtime; Go↔Rust pinned by golden vectors
- [x] crank builds the §6.5 3-instruction tx — TS reference proven on localnet; Go
      builder byte-verified in unit tests
- [x] **Go crank reworked to v0 tx + Address Lookup Table** — per-market cached LUT
      (`crank/lut.go`); size proven in tests: legacy 1421 B ❌ vs v0 1116 B ✅ (limit 1232)
- [x] program deploys to **devnet** at the pinned ID — deployed 2026-07-15, tx
      `5Ayf6cLmSpqFue5odVvTVSBQSPMyJjyV6ndhp9FPu6F46CYDSkJucuDyPTpKMQvbpfv4XzC33v4bnfnaj4xXgVqa`
- [x] one signed order → matched → **Go crank** settles on devnet — ed25519 verify +
      fill-accounting + MINT executed, tx
      `3zNVPQJqLZhAuRpEmCzGxVfA9aqQe3mm3qT1yFzcN34rrNqM1Eu2oyuagxvdcT51xTjW86ggzjNGhrbvYoKzvdXS`
- [x] `resolve_market` → `redeem` on devnet → user's USDC moved 1:1 — txs
      `5oNcWKQBin6atteQcvAAtEkdivE5q9hXKmYXWeNiKzrXrS7X2VJN2SvSe7pxQ8oCMvjrSjBMr2T9i1uWtVJfXiK8` /
      `4qKCYL4G1VzsPighcWLQ6wgEfYBggHnFCkpHfomXBkWdCfVzrEHv4Ju3dLAwtKHNx62WEyV7Tvi2VqxeRSMWkMku`

**THE FLOOR IS DONE.** `go run ./cmd/devnet-e2e` reproduces it end-to-end (all balance
assertions green: 60/40 vault debits, complete set minted, pool 100% collateralized,
1:1 redemption). Devnet RPC note: public endpoint rate-limits aggressive status polls —
the harness and crank poll gently (1.5–2.5s) with long windows; a tx that "times out"
usually landed (check `solana confirm`).

---

## 6. Open decisions

| # | Decision | Owner | Status |
|---|---|---|---|
| 1 | ~~Commit `pitchmarket-keypair.json` or share out of band?~~ | both | **CLOSED 2026-07-12: committed** (devnet-only key; `git add -f`, on `feat/devnet-settlement`) |
| 2 | Oracle tier for demo: a (operator) vs d (TxODDS signed) | E1 | open, gated on TxODDS reply |
| 3 | TxODDS signed-data email sent? | — | **still unknown — confirm** |
| 4 | ~~Postgres vs in-memory~~ | E2 | CLOSED 2026-07-11: Postgres (Neon), wired + tested |
| 5 | Combos on-chain (`combo_accept`) vs off-chain for demo | E1 | E2 ships either way (interface seam); default off-chain per cut plan |

---

## 7. Next actions

**Full handoff (runbooks, invariants, punch list with code seams): `docs/HANDOFF.md`.**

**E2:** **frontend** — the last unstarted scope, with the deadline TODAY. The backend
serves everything it needs (HANDOFF §5.1 is the API contract); wire
`SOLANA_RPC_URL`/`OPERATOR_KEYPAIR` env to run the server in on-chain mode against the
deployed program. Then HANDOFF §5.2 (on-chain market lifecycle wiring).

**E1:** `combo_accept` / `resolve_combo` → oracle tier d if TxODDS replies (cut-safe:
combos run off-chain behind the interface seam).

**Both:** confirm the TxODDS email (decision #3); record the demo.

---

## 8. Housekeeping / paper cuts

- Neon DATABASE_URL lives in the gitignored `.env` (`.env.example` documents the shape).
  Rotate the credential after the hackathon (it was shared in chat).
- **Toolchain trap:** any `anchor` CLI invocation may silently repoint `active_release`
  to old platform-tools — see §4 step 2 before debugging "mystery" build failures.
- `docs/interface-contract.md` §6.5-above-§6 ordering still unfixed; §6.5 should also
  gain the v0+ALT requirement once the Go crank lands it.
- DB tests: `go test -p 1` (see §3 caveats).

---

## 9. Changelog

Newest first. One row per meaningful change. **Append here in the same commit as the code.**

| Date | Who | What changed | Verified how |
|---|---|---|---|
| 2026-07-15 | Ashish | **Program deployed to devnet** at the pinned ID; **Go crank settled a real match on devnet** (v0+ALT); gentler RPC polling in crank/harness (public devnet endpoint rate-limits) | `cmd/devnet-e2e` ✅ full run: initialize→vaults→deposits→engine MINT fill→**settle_match**→resolve→redeem, every balance asserted; tx sigs in §5 |
| 2026-07-12 | Ashish | **Crank v0 + per-market ALT** (`crank/lut.go`, `BuildSettleMatchTxV0`); chain builders (initialize_market/init_vault/deposit/resolve/redeem) + `cmd/devnet-e2e` harness; committed program keypair (decision #1 closed); reproduced §4 fix on 2nd machine (Agave 4.1.1 → `.so` 419,400 B); pinned v0+ALT in interface-contract §6.5. **Devnet deploy blocked only on faucet SOL.** | `go test ./internal/crank` ✅ (v0 1116 B ≤ 1232, legacy 1421 B rejected; layout tests) · `cargo-build-sbf` ✅ on this machine · devnet run pending funds |
| 2026-07-12 | Ashish | Merged PR #3 into main; reconciled this file across both tracks (E1 localnet results + E2 backend state + v0/ALT crank rework now tracked in §5/§7) | host `cargo test -p pitchmarket` ✅ · `go build`/`vet` + targeted Go suites ✅ on the merged tree |
| 2026-07-13 | E1 | Added MERGE + cancel_order tests; refactored the TS harness into `tests/helpers.ts` (single borsh impl) | `npm test` **8/8 ✅** on `solana-test-validator` — all settle paths + cancel fail-closed |
| 2026-07-12 | E1 | Fixed §4 build blocker (platform-tools v1.54); Boxed `SettleMatch` accounts (BPF stack overflow); added `idl-build` feature; added TS lifecycle test harness (`tests/`, `package.json`) | `cargo build-sbf` ✅ · `npm test` 5/5 ✅ on `solana-test-validator` (initialize→deposit→settle MINT+NORMAL→resolve→redeem, balances asserted) |
| 2026-07-11 | Ashish | **E2 backend complete**: matching MINT/MERGE + cancel/expiry/unfill; Postgres store on Neon; exchange core with revert→reconcile; crank §6.5 tx builder + RPC submitter + off-chain mode; WS hub; full REST API; RFQ with mutex groups; precision; MM bot; lifecycle; txodds SSE skeleton; Claude one-liners; chain-index poller; server wiring; borsh golden vectors Go↔Rust; demo replay fixture | `go build` ✅ `go vet` ✅ · every test package green against real Postgres (11 pkgs incl. HTTP e2e + WS + revert path); two packages hit Neon network timeouts in the one full serialized run and passed on immediate re-run (§3 caveat) · `cargo test -p pitchmarket` ✅ 4/4 · `anchor build` ❌ at the time (fixed next day, §4) |
| 2026-07-10 | Ashish | Added `progress.md` + `CLAUDE.md`; trimmed stale README status; untracked `.DS_Store`; committed the E1/E2 scaffold | `cargo check` ✅ · `go build ./... && go vet ./...` ✅ · `anchor build` ❌ (§4) |
| 2026-07-09 | E1 | Implemented `sig_verify::verify_order_signature`; pinned settle_match tx layout in interface-contract §6.5 | `cargo check` only — never executed |
| 2026-07-08 | E1/E2 | Anchor program scaffold; Go matching engine, crank skeleton, order API, replay feed, Postgres schema | `cargo check` · `go build` |
