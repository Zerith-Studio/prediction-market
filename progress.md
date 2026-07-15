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
| Go/No-Go gate | **Jul 11 EOD** — binary settlement end-to-end (PROJECT_PLAN §7) |
| Demoable floor | 🟡 **normal core verified on a local validator** — not yet on devnet |
| **Top blocker** | **~~`anchor build` fails~~ RESOLVED (§4). Next: devnet deploy (open decision #1, keypair) + crank submitter.** |

**Honest summary.** The §4 toolchain blocker is fixed and the program now compiles to
BPF. The full normal-core lifecycle — `initialize_market → deposit → settle_match
(MINT & NORMAL) → resolve_market → redeem` — **runs green against a local validator**
(`solana-test-validator`), with balance assertions proving the collateral-pool, mint,
peer-to-peer, and redemption math. `sig_verify` (ed25519 sysvar introspection, the old
longest pole) executed for real, which also proves the TS borsh encoding matches
`sig_verify.rs::borsh_order` byte-for-byte. All three settle paths (MINT/NORMAL/MERGE)
plus `cancel_order`'s fail-closed guard are now exercised — **8/8 tests green**. **Still
not done:** devnet deploy (needs the program keypair / decision #1) and E2's crank still
has no submitter. Verification is **localnet, not devnet** — treat ✅ marks accordingly.

---

## 2. E1 — Anchor program (`programs/pitchmarket`)

Builds to BPF (`cargo build-sbf`, see §4). ✅ marks below = **exercised on a local
validator** via `tests/lifecycle.ts` (`npm test`), 5/5 passing. Not yet run on devnet.

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
| `sig_verify::verify_order_signature` | ✅ | ed25519 sysvar introspection **executed for real** in settle_match. Also confirms TS borsh == `sig_verify.rs::borsh_order` |
| `combo_accept` | 🔴 | typed stub |
| `resolve_combo` | 🔴 | typed stub |
| VOID path | 🔴 | |
| Oracle tier b (challenge) / d (TxODDS sig) | 🔴 | gated on TxODDS reply |

**Two program changes were needed to build & run** (both in this commit):
- `SettleMatch` accounts are now `Box`ed — the 18-account context otherwise overflowed
  the 4KB BPF stack frame by 64 bytes (only surfaces at BPF build, not `cargo check`).
- `Cargo.toml` gained the `idl-build` feature (was missing; blocked IDL generation).

**Also found:** the settle_match tx (2 ed25519 precompiles + 18-account `settle_match`)
is **1453 bytes > the 1232 legacy limit**. It only fits as a **v0 tx with an Address
Lookup Table** — the crank MUST build it this way (`tests/lifecycle.ts` shows how).

**Program ID** `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs` — pinned in `declare_id!`
and `Anchor.toml`.

⚠️ **The keypair at `target/deploy/pitchmarket-keypair.json` is gitignored and exists on
one machine only.** Both engineers can *build* this program ID, but only whoever holds
that file can *deploy* to it. **Decide before deploy day:** `git add -f` it (fine for a
devnet hackathon) or share out of band. If it's lost, the program ID changes everywhere.

---

## 3. E2 — Go backend (`backend/`)

Verified with `go build ./... && go vet ./...` — both pass.

| Package | State | Notes |
|---|---|---|
| `matching` | 🟡 | in-memory CLOB, price-time priority. **NORMAL fills only** — MINT/MERGE crossing is a TODO in `engine.go:59` |
| `models` | 🟡 | `hash.go` borsh encoding — **must stay byte-identical to `sig_verify.rs:borsh_order`** |
| `api` | 🟡 | `POST /orders`, `GET /healthz`. `GET /markets/{id}/book` is a placeholder (`api.go:75`). No ed25519 check on intake (`api.go:51`) |
| `crank` | 🔴 | `Submitter` is an interface with **no implementation**. Nothing reaches chain. |
| `feed` | 🟡 | `replay` adapter works; `txodds` live provider blocked on TxODDS access |
| `db/schema.sql` | 🟡 | schema written, **never applied — no Postgres wiring at all** (server is in-memory) |
| WS hub | 🔴 | not started |
| `rfq` / `mmbot` / `precision` / `oneliner` / `index` | 🔴 | not started |
| Next.js frontend | 🔴 | not started |

---

## 4. ✅ RESOLVED — program now builds to BPF (fixed 2026-07-12)

The `edition2024` failure was caused entirely by an **old platform-tools** (v1.43 →
rustc/cargo 1.79), which can't parse deps that Anchor 0.31.1 pulls (`block-buffer`,
`crypto-common`, etc. require cargo ≥1.85). The fix is a **modern Agave install**, which
ships **platform-tools v1.54 / rustc 1.89** and compiles the whole tree cleanly.

**How it was fixed (reproducible on a fresh machine):**
1. Install Rust (`rustup`, gives host cargo 1.97), Agave CLI 4.1.1
   (`release.anza.xyz/stable/install` → platform-tools **v1.54**), and Anchor via avm.
2. **Build with `cargo build-sbf` from the program dir, NOT `anchor build`.** This is the
   crux: `anchor build` (and even `anchor idl build`) runs its own toolchain override
   that **re-installs Solana 2.1.0 and repoints `active_release` back to the old v1.43
   tools** — re-breaking the build. That override is the "inconsistent state" the earlier
   note hit. After any `anchor` invocation, repoint:
   ```sh
   cd ~/.local/share/solana/install
   ln -sfn "$PWD/releases/stable-<hash>/solana-release" active_release && hash -r
   cargo-build-sbf --version   # must read platform-tools v1.54 / rustc 1.89
   ```
3. `cd programs/pitchmarket && cargo build-sbf` → `target/deploy/pitchmarket.so` (419 KB).

**IDL:** `anchor idl build` chokes on the two `ostatus` PDAs whose seed is a function
call on an instruction arg (`sig_verify::order_hash(&taker)`) — it can't introspect that.
Workaround used: temporarily swap those seeds for a plain arg field to emit the IDL, then
restore. The runtime `.so` keeps the real hash-based seeds. (A cleaner long-term fix is
worth finding, but the IDL is only needed for the TS client.)

**Verify on a second machine** — this fix was done on a clean box; E2 should reproduce.

---

## 5. Definition of done for the Jul 11 Go/No-Go

The floor we promised never to cut — one match, one binary market, fully trustless:

- [x] `anchor build` produces a `.so` — via `cargo build-sbf` (§4); 419 KB
- [ ] program deploys to devnet at the pinned ID — **still open** (needs keypair, decision #1);
      currently loaded on `solana-test-validator` via `--bpf-program <declared-id> pitchmarket.so`
- [ ] `crank.Submitter` implemented against `solana-go` — **still open** (E2). Note: must emit a
      **v0 tx with an Address Lookup Table**, the 3-ix tx is 1453 B > the 1232 legacy limit
- [x] crank builds the exact 3-instruction tx (ed25519 taker, ed25519 maker, `settle_match`)
      — **proven in `tests/lifecycle.ts`** (TS reference impl); Go crank still TODO
- [~] `models.OrderHash` borsh bytes == `sig_verify.rs` borsh bytes — **TS ↔ Rust proven**
      (settle_match sig check passed); Go ↔ Rust conformance test still needed (E2)
- [x] one signed order → matched → `settle_match` lands (MINT & NORMAL) — **localnet, not devnet**
- [x] `resolve_market` (tier-a) → `redeem` → user's USDC balance moves — **localnet, not devnet**

If this isn't green by Jul 11 EOD, cut per PROJECT_PLAN §7 (combos → off-chain or cut,
precision off-chain, drop one-liner/NFT).

---

## 6. Open decisions

| # | Decision | Owner | Status |
|---|---|---|---|
| 1 | Commit `pitchmarket-keypair.json` or share out of band? | both | **open — blocks deploy** |
| 2 | Oracle tier for demo: **a** (operator) vs **d** (TxODDS signed) | E1 | open, gated on TxODDS reply |
| 3 | Has the TxODDS signed-data email been sent? (`docs/txodds-day1-email.md`) | — | **unknown — confirm** |
| 4 | Postgres for the demo, or stay in-memory and cut persistence? | E2 | open |

---

## 7. Next actions

**E1** — unblock `anchor build` (§4) → deploy to devnet → exercise `settle_match` with a
real ed25519 tx via a test harness. That single path is worth more than `combo_accept`.

**E2** — implement `crank.Submitter` against `solana-go`; add the cross-language borsh
conformance test (§5); finish `GET /markets/:id/book`. Then the binary market frontend.

**Both** — resolve open decision #1 today; it silently blocks deploy day.

---

## 8. Housekeeping / paper cuts

- `docs/interface-contract.md`: new `## 6.5` section was inserted **above** `## 6`.
  Reorder so it reads top-to-bottom.
- `README.md` "State of the scaffold" was stale (claimed `sig_verify` was a
  stub that always errors — it's implemented). Status now lives here; README points at it.
  Keep it that way so the two don't drift.
- `.DS_Store` was committed before `.gitignore` existed; now untracked.
- `Cargo.lock` was regenerated on 2026-07-10 while debugging §4.

---

## 9. Changelog

Newest first. One row per meaningful change. **Append here in the same commit as the code.**

| Date | Who | What changed | Verified how |
|---|---|---|---|
| 2026-07-13 | E1 | Added MERGE + cancel_order tests; refactored the TS harness into `tests/helpers.ts` (single borsh impl) | `npm test` **8/8 ✅** on `solana-test-validator` — all settle paths + cancel fail-closed |
| 2026-07-12 | E1 | Fixed §4 build blocker (platform-tools v1.54); Boxed `SettleMatch` accounts (BPF stack overflow); added `idl-build` feature; added TS lifecycle test harness (`tests/`, `package.json`) | `cargo build-sbf` ✅ · `npm test` 5/5 ✅ on `solana-test-validator` (initialize→deposit→settle MINT+NORMAL→resolve→redeem, balances asserted) |
| 2026-07-10 | Ashish | Added `progress.md` + `CLAUDE.md`; trimmed stale README status; untracked `.DS_Store`; committed the E1/E2 scaffold | `cargo check` ✅ · `go build ./... && go vet ./...` ✅ · `anchor build` ❌ (§4) |
| 2026-07-09 | E1 | Implemented `sig_verify::verify_order_signature`; pinned settle_match tx layout in interface-contract §6.5 | `cargo check` only — never executed |
| 2026-07-08 | E1/E2 | Anchor program scaffold; Go matching engine, crank skeleton, order API, replay feed, Postgres schema | `cargo check` · `go build` |
