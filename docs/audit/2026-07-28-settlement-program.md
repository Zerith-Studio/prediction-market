# PitchMarket — Settlement Program Audit

**Date:** 2026-07-28
**Scope:** `programs/pitchmarket` (~1,050 lines, Rust / Anchor 0.31.1)
**Program ID:** `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs`

A targeted review of the Anchor settlement program behind PitchMarket's on-chain trade
settlement. Two critical, exploitable findings — both fixed and covered by regression
tests in this pass — plus one open medium finding.

| | |
|---|---|
| Critical found | 2 |
| Critical fixed | 2 |
| Medium, open | 1 |
| Patterns verified sound | 6 |

---

## Scope & methodology

Reviewed the full Anchor program source: `lib.rs`, `sig_verify.rs`, `state.rs`,
`errors.rs` (since split into `instructions/` — see the commit trail below),
cross-referenced against `docs/interface-contract.md` and the existing TypeScript test
suite to see what was and wasn't already covered.

- **Primary tool:** trailofbits/skills `solana-vulnerability-scanner` — 6 Solana/Anchor-specific
  vulnerability patterns (arbitrary CPI, PDA validation, missing ownership/signer checks,
  sysvar spoofing, instruction introspection).
- **Secondary reference:** frankcastleauditor/safe-solana-builder `shared-base.md` checklist.
- **Cross-check:** every instruction diffed against `docs/interface-contract.md` §4 (the
  frozen E1↔E2 boundary).
- **Out of scope:** `combo_accept` / `resolve_combo` — typed stubs, not yet implemented;
  oracle tiers b/d — not started.

---

## Findings

### 01 — Redeem never binds the claimed outcome to the mint burned

**Severity:** Critical · **Status:** Fixed
**Location:** `instructions/redeem.rs` — `redeem_handler`, `Redeem` accounts struct

**Impact.** A holder of the *losing* side's shares in a resolved market could redeem
them at full 1:1 value, draining `pool_usdc` beyond its real backing. On a VOID market
the check was skipped entirely, so either mint redeemed as a full win.

**Root cause.** `outcome_mint` was accepted as a bare `Account<'info, Mint>` with no
constraint tying it to `market.yes_mint` / `no_mint`. The handler checked the
caller-supplied `outcome: u8` against `market.outcome` for authorization, but never
checked that the *account actually burned* corresponded to that same outcome — the two
were independent, caller-controlled values.

**Exploit scenario.** Market resolves YES. Bob holds 100 real (now-worthless) NO shares
from an earlier MINT. Bob calls `redeem(outcome = YES, amount = 60)` but supplies
`outcome_mint = market.no_mint` and his own NO-share ATA. The burn succeeds — he really
owns those tokens — and the program pays out 60 × 1,000,000 micro-USDC regardless,
because nothing ever compared the mint to the outcome.

Before:
```rust
if market.outcome != MarketOutcome::Void {
    let winning_outcome = /* … */;
    require!(outcome == winning_outcome, PitchMarketError::MarketNotOpen);
}
// outcome_mint never checked against market.yes_mint / no_mint
token::burn(/* burns whatever outcome_mint was passed */)?;
```

After:
```rust
let expected_mint = match outcome {
    OUTCOME_NO => market.no_mint,
    OUTCOME_YES => market.yes_mint,
    _ => return err!(PitchMarketError::InvalidOutcome),
};
require_keys_eq!(ctx.accounts.outcome_mint.key(), expected_mint,
    PitchMarketError::OutcomeMintMismatch);
// …then the existing winning-outcome check, then the burn
```

**Fix & verification.** Fixed in `90ebdef` — new `OutcomeMintMismatch` /
`InvalidOutcome` errors; the mint is checked before any burn or payout. Regression test
added to `tests/lifecycle.ts`: a losing-side holder attempting this exact redeem must
revert, pool balance and share balance confirmed untouched. `cargo check` / `cargo test`
green; not re-run on a local validator this session (see Verification below).

---

### 02 — `cancel_order` never verifies the signer owns the order

**Severity:** Critical · **Status:** Fixed
**Location:** `instructions/orders.rs` — `cancel_order_handler`, `CancelOrder` accounts struct

**Impact.** Any wallet could permanently cancel *any other user's* resting order — a
griefing / denial-of-service vector against the whole order book, at the cost of rent
only.

**Root cause.** The instruction took a bare `order_hash: [u8; 32]` and any `Signer`
named `maker`. The `OrderStatus` PDA is seeded purely by that hash — public information
visible via the order book, REST API, and WS events — and nothing compared the signer
to the order's actual `maker` field, because the full order (which contains that field)
was never passed in to begin with.

This was a genuine regression, not a design choice: `docs/interface-contract.md` §4
already specified `cancel_order(order_hash, maker_sig)` — a signature check the shipped
code had silently dropped.

Before:
```rust
pub fn cancel_order(ctx: Context<CancelOrder>, _order_hash: [u8; 32]) -> Result<()> {
    let status = &mut ctx.accounts.order_status;
    require!(!status.is_filled_or_cancelled, PitchMarketError::OrderClosed);
    status.is_filled_or_cancelled = true;   // no check that `maker` == order.maker
    Ok(())
}
```

After:
```rust
pub fn cancel_order_handler(ctx: Context<CancelOrder>, order: OrderArgs) -> Result<()> {
    require_keys_eq!(ctx.accounts.maker.key(), order.maker, PitchMarketError::Unauthorized);
    let status = &mut ctx.accounts.order_status;
    ensure_order_status_initialized(status, &order, ctx.bumps.order_status);
    require!(!status.is_filled_or_cancelled, PitchMarketError::OrderClosed);
    status.is_filled_or_cancelled = true;
    Ok(())
}
```

**Fix & verification.** Fixed in `c76eff1` — the instruction now takes the full order,
checks `maker.key() == order.maker`, and derives the PDA seed from the
on-chain-computed hash rather than a caller-supplied one, mirroring `settle_match`'s
existing pattern. Confirmed neither the Go backend, frontend, nor mobile client call
the on-chain instruction at all yet (cancellation today is off-chain-mirror only via
`DELETE /orders/{hash}`), so this fix has zero integration blast radius. Regression
test added to `tests/settle_paths.ts`: a third-party signer attempting to cancel
another maker's order must revert, and the order must remain live afterward.

---

### 03 — `initialize_market` has no caller allowlist

**Severity:** Medium · **Status:** Open
**Location:** `instructions/market.rs` — `InitializeMarket` accounts struct

**Impact.** Anyone can call `initialize_market` for a given `market_id` before the
legitimate operator does. Because the market PDA uses Anchor's `init` constraint, a
squatted market permanently blocks the real one at that address and hands the attacker
`resolver_authority` for their fake market. No funds are directly at risk — the real
market simply can never be created at that ID — but it's a live griefing surface
against auto market creation.

**Recommendation.** Constrain `operator` to a known key (e.g. an `address =` constraint
against a pinned operator pubkey, or a program-level allowlist account) if this hasn't
already been mitigated at the E2 layer by only ever calling it with a trusted key. Left
unfixed in this pass — lower severity than the two critical findings above, and out of
scope for the current fix cycle.

---

## What was already sound

The scanner's other five pattern checks, plus CPI validation, came back clean — worth
recording so future changes don't accidentally regress them.

- **Arbitrary CPI.** Every token transfer/mint/burn goes through `Program<'info, Token>`
  — Anchor validates the program ID on every call.
- **PDA validation.** All seeds use a canonical, stored bump; none accept a
  user-supplied bump.
- **Ownership checks.** Every real account is `Account<'info, T>` (auto owner-checked);
  the one `UncheckedAccount` is redundant with an already seed-pinned vault, not a gap.
- **Sysvar spoofing.** `instructions_sysvar` is checked against the real sysvar address
  before use, via `load_instruction_at_checked`.
- **Instruction introspection.** `verify_order_signature` uses fixed indices, but
  deep-validates the embedded pubkey/message/signature bytes against the exact order
  passed in — closes the generic "instruction reuse" gap by content, not just position.
- **BPF stack frame.** `SettleMatch`'s 18 accounts are already `Box`ed, keeping the
  context off the 4KB stack frame (a real prior finding from the Jul-12 build fix, still
  correctly applied).

---

## Verification & residual risk

`cargo check -p pitchmarket` and `cargo test -p pitchmarket` (4/4, including the borsh
golden vectors) are green on the final state, with the same warning count as the
pre-audit baseline — no new warnings introduced. Both TypeScript test files were
extended with adversarial regression tests and confirmed to type-check cleanly.

**Not verified this session:** `cargo build-sbf` — the real BPF compile — hit a
pre-existing platform-tools/`edition2024` mismatch already documented in `progress.md`
§4 (this machine's Agave install is v1.48; the documented fix needs v1.54, a global
toolchain reinstall not attempted without checking first). Without a fresh `.so`, the
TS suite could not be run end-to-end against a local validator. The fixes are verified
at the source/unit level, not on-chain, in this session.

**⚠️ The devnet-deployed program (2026-07-15) predates both fixes and still runs the
vulnerable code.** Redeploying requires the fixed toolchain plus the operator keypair.

---

## Commit trail

| Commit | Subject |
|---|---|
| `1bf2f73` | chore(skills): install Solana security audit skills |
| `4f63d47` | refactor(program): split pitchmarket lib.rs into instructions/ modules |
| `c76eff1` | fix(program): require cancel_order's maker to match order.maker — Finding 02 |
| `90ebdef` | fix(program): bind redeem outcome_mint to the resolved outcome — Finding 01 |
| `8503510` | docs(progress): log the security audit and program refactor |
