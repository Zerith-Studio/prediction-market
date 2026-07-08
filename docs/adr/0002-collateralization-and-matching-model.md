# ADR 0002 — Fully-collateralized binary model (CTF-style), three match types

**Status:** Accepted (supersedes the minting description in PROJECT_PLAN.md §7)
**Date:** 2026-07-04

## Context
The initial plan described "selling YES by minting a pair," which incorrectly handed a
resting seller the opposite (NO) leg and conflated selling with minting. This risked a
book that isn't provably collateralized. We adopt Polymarket's Conditional Token
Framework (CTF) model verbatim: a fully-collateralized binary market, **not** a margin
venue. No naked shorts, no leverage, no negative balances.

## Decision
**Primitives**
- Collateral = USDC (integer cents off-chain; SPL USDC on-chain). Outcome tokens: two
  complementary assets YES and NO per market.
- Complete-set invariant: `1 YES + 1 NO ⇄ $1`, always. Split $1 → mint YES+NO; merge
  YES+NO → $1. Prices sum to $1 (60¢/40¢).

**Order data model** — a BUY vs SELL is defined by which asset the maker provides:
| Side | Provides (makerAsset) | Receives (takerAsset) | Locks at entry |
|---|---|---|---|
| BUY | USDC | outcome token | `price × size (+ fee)` USDC |
| SELL | outcome token | USDC | `size` of that outcome's tokens |

**Entry-time collateral (enforced, no exceptions):**
- BUY: require `available_USDC ≥ price×size + fee`.
- SELL: require `token_balance(outcome) ≥ size`. A SELL for tokens you don't hold is
  **rejected at entry.** There is no naked-short path. To take the bearish side you BUY
  NO (which locks USDC). $X of USDC caps total open exposure at $X of shares across all
  markets combined.

**Three settlement paths in the matching engine:**
| Match type | Sides paired | Effect |
|---|---|---|
| NORMAL | BUY vs SELL (same outcome) | Direct swap: tokens→buyer, USDC→seller. No mint/burn. |
| MINT | BUY YES + BUY NO | Combine the two buyers' collateral, split $1 → mint both, one token to each buyer. |
| MERGE | SELL YES + SELL NO | Collect both tokens, merge → $1, proceeds to each seller. |

A single taker order may hit COMPLEMENTARY (NORMAL) makers and MINT makers in one call,
but never MINT+MERGE together. Nobody is ever handed the opposite leg by surprise — you
hold NO only if you explicitly bought NO.

**Solvency (structural, not a bolt-on guard):**
- Every share is prepaid; a set exists only because $1 was deposited via split, destroyed
  only via merge/redeem paying $1 back.
- Collateral is escrowed at mint/fill time; winners `redeem` against escrow, not against a
  possibly-insolvent loser. Book is 100% collateralized by construction; payouts cannot
  bounce. Max loss = exactly the price prepaid.

## Consequences
- (+) Provably solvent book; simple, correct engine; no margin/liquidation logic.
- (+) Matching = pairing NORMAL/MINT/MERGE; portable to SPL mints + escrow vault on Solana.
- (−) Requires the engine to actively pair two-buyers (MINT) and two-sellers (MERGE), not
  just BUY-vs-SELL. This must be a first-class feature, not an afterthought.
- **OPEN (grilling A4):** *where* escrow lives — off-chain ledger (per ADR 0001) vs the
  on-chain vault the CTF model implies. These are in tension; resolve before building.
