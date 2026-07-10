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

## 1. Status at a glance — 2026-07-10 (Day 6 of 11)

| | |
|---|---|
| Deadline | **2026-07-15** (internal) · judged by 2026-07-29 |
| Days left | **5** |
| Go/No-Go gate | **Jul 11 EOD** — binary settlement end-to-end on devnet (PROJECT_PLAN §7) |
| Demoable floor | 🔴 not yet — nothing has run against devnet |
| **Top blocker** | **`anchor build` fails; program has never been compiled to BPF or deployed.** See §4. |

**Honest summary.** Both tracks have a real scaffold and both compile on the host
toolchain. But *no code has ever run against a Solana validator* — the program has not
been built for BPF, not deployed, and the crank has no submitter. The Jul 11 gate is at
risk, and the thing standing in the way is a toolchain problem, not a code problem.

---

## 2. E1 — Anchor program (`programs/pitchmarket`)

Verified with `cargo check -p pitchmarket` (host target, passes with 26 warnings).
**Not** verified with `anchor build` / on devnet — see §4.

| Instruction | State | Notes |
|---|---|---|
| `initialize_market` | 🟡 | Market PDA + 2 outcome mints |
| `init_vault` / `deposit` | 🟡 | Vault PDA custody (per interface-contract §6 decision) |
| `settle_match` NORMAL | 🟡 | collateral-pool CTF model |
| `settle_match` MINT | 🟡 | |
| `settle_match` MERGE | 🟡 | |
| `cancel_order` | 🟡 | |
| `resolve_market` | 🟡 | **tier-a only** (operator-signed). Tiers b/d not started |
| `redeem` | 🟡 | |
| `sig_verify::verify_order_signature` | 🟡 | **Now implemented** (ed25519 sysvar introspection). Was the "longest pole" — code is written, never executed. |
| `combo_accept` | 🔴 | typed stub |
| `resolve_combo` | 🔴 | typed stub |
| VOID path | 🔴 | |
| Oracle tier b (challenge) / d (TxODDS sig) | 🔴 | gated on TxODDS reply |

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

## 4. 🔴 BLOCKER — `anchor build` does not compile (found 2026-07-10)

`cargo check` passes on the host, so the *code* is fine. `anchor build` fails because the
BPF toolchain that Solana CLI ships is far older than the crates Anchor 0.31.1 pulls in.

```
$ anchor build
error: failed to parse manifest at .../crypto-common-0.2.2/Cargo.toml
Caused by: feature `edition2024` is required
  ... not stabilized in this version of Cargo (1.79.0)
```

**Diagnosis.** `cargo-build-sbf` uses **platform-tools v1.43 → rustc/cargo 1.79**.
Transitive deps of `anchor-lang`/`anchor-spl` 0.31.1 now require `edition2024` (needs
cargo ≥1.85). Offenders seen, in order: `crypto-common 0.2.2` → `zeroize_derive 1.5.0` →
`hashbrown 0.17.1` (via `borsh-derive → proc-macro-crate → toml_edit → indexmap`) →
`unicode-segmentation 1.13.3`. Pinning them one at a time works but is whack-a-mole and
will rot — **don't go down that path.**

**Real fix = newer platform-tools.** Attempted `agave-install init 2.3.13`; `solana
--version` then reports 2.3.13, but `~/.local/share/solana/install/active_release` still
symlinks to `2.1.0/solana-release`, and even the 2.3.13 tree's `cargo-build-sbf` reports
`platform-tools v1.43 / rustc 1.79.0`. The local install is in an inconsistent state.

**Next steps for whoever picks this up (in order):**
1. Point `active_release` at the 2.3.13 tree, `hash -r`, confirm
   `cargo-build-sbf --version` reports platform-tools ≥ v1.48 / rustc ≥ 1.85.
2. If that fails, nuke `~/.cache/solana/` (holds only `v1.43`) so it re-downloads, and
   reinstall the CLI cleanly.
3. If still stuck, pin `anchor-lang`/`anchor-spl` **down** to a release matching the
   installed platform-tools, rather than pinning a dozen transitive crates up.
4. **Verify on a second machine** — this may be local-only. E2 should try `anchor build`
   independently before we conclude the repo is broken.

Nothing on the critical path (deploy → settle → resolve → redeem) can start until this
is green. **This is the Jul 11 gate.**

---

## 5. Definition of done for the Jul 11 Go/No-Go

The floor we promised never to cut — one match, one binary market, fully trustless:

- [ ] `anchor build` produces a `.so`
- [ ] program deploys to devnet at the pinned ID
- [ ] `crank.Submitter` implemented against `solana-go`
- [ ] crank builds the exact 3-instruction tx from interface-contract §6.5
      (ed25519 taker, ed25519 maker, `settle_match`)
- [ ] `models.OrderHash` borsh bytes == `sig_verify.rs` borsh bytes (**write a cross-language
      test — a silent drift here fails closed as `BadSignature` and will cost hours**)
- [ ] one signed order → matched → `settle_match` lands on devnet
- [ ] `resolve_market` (tier-a) → `redeem` → user's USDC balance moves

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
| 2026-07-10 | Ashish | Added `progress.md` + `CLAUDE.md`; trimmed stale README status; untracked `.DS_Store`; committed the E1/E2 scaffold | `cargo check` ✅ · `go build ./... && go vet ./...` ✅ · `anchor build` ❌ (§4) |
| 2026-07-09 | E1 | Implemented `sig_verify::verify_order_signature`; pinned settle_match tx layout in interface-contract §6.5 | `cargo check` only — never executed |
| 2026-07-08 | E1/E2 | Anchor program scaffold; Go matching engine, crank skeleton, order API, replay feed, Postgres schema | `cargo check` · `go build` |
