# ADR 0001 — Off-chain matching, on-chain settlement anchor

**Status:** Accepted
**Date:** 2026-07-04

## Context
The track is "Prediction Markets & Settlement" on Solana. A fully on-chain order book
(every order/cancel/fill as a signed tx) has ~400ms+ confirmation latency and a wallet
popup per action, which makes a live-trading demo feel broken, and matching logic
on-chain is high-risk to build in a hackathon. Judges care that outcomes/payouts are
*verifiable*, not that every limit order is a transaction.

## Decision
- Matching engine, order book, balances, positions, precision pools, and RFQ state are
  **off-chain** (Go + Postgres).
- Solana is used at **settlement only**: a devnet Anchor program records each market's
  `(market_id, outcome)` via `resolve_market`, and settles/redeems positions on-chain
  (per-match `settle_match`, then `redeem`), so results are independently verifiable.
- **No on-chain order book.**
- Custody is an off-chain demo ledger; a trustless escrow-vault path is designed but
  optional (Tier 2).

## Consequences
- (+) Fast, familiar trading UX; small, low-risk on-chain surface; clear settlement story.
- (−) Trust model is operator-custodial until the escrow vault ships. Must be stated
  honestly in README/demo. Recording the outcome on-chain proves *what was decided*, not
  that custody is trustless. ← this trust boundary is being grilled (see open questions).
- (−) A single `resolver_authority` key is a centralization point. ← under grilling.
