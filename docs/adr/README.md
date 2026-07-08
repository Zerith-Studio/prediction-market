# PitchMarket — Architecture Decision Records

The design, grilled to bedrock. Each ADR is one pinned decision; read top to bottom for the
full trust story. Companions:
- [`../core-features-spec.md`](../core-features-spec.md) — **Polymarket-verified (v2)** feature parity + corrections M1/M2/M3.
- [`../interface-contract.md`](../interface-contract.md) — the E1↔E2 build boundary (freeze Day 0).
- [`../../PROJECT_PLAN.md`](../../PROJECT_PLAN.md) — the execution/build plan + two-track schedule.
- [`../glossary.md`](../glossary.md) — domain vocabulary. [`../txodds-day1-email.md`](../txodds-day1-email.md) — the highest-leverage hour.

| # | Decision | One-line takeaway |
|---|---|---|
| [0001](0001-off-chain-matching-on-chain-settlement.md) | Off-chain matching, on-chain settlement | No on-chain order book; Solana enters at settlement. |
| [0002](0002-collateralization-and-matching-model.md) | Fully-collateralized CTF model | BUY locks USDC, SELL locks tokens; three match types NORMAL/MINT/MERGE; no naked shorts; provably solvent. |
| [0003](0003-escrow-boundary-signed-orders-operator-crank.md) | Escrow boundary | Users sign *orders* (off-chain, silent); operator signs *settlements* (on-chain, pays gas). Non-custodial, on-chain-at-fill, snappy. |
| [0004](0004-combo-settlement-onchain-reads-leg-conditions.md) | RFQ combo settlement | A parlay = binary set over `C=AND(legs)`. Off-chain negotiation, on-chain escrow + resolve that **reads the same leg-condition accounts binary redeem reads** → no new oracle, no seam. |
| [0005](0005-settlement-oracle-layer.md) | Oracle / root-of-trust | Everything bottoms out here. Floor=(a) single key; **swing=(d) TxODDS-signed data verified on-chain** → operator becomes a pure relay; fallback=(b) challenge window. Finality rule = signed `final=true` + T+X delay; `VOID` for abandonment. |
| [0006](0006-precision-market-economics.md) | Precision economics | Kickoff-lock + one-entry-per-wallet + rake + **σ-normalized score** + crowd-seeding bot. Formula convexity already charges carpeters ~5×. |
| [0007](0007-scope-team-schedule.md) | Scope / team / schedule | 2 engineers, greenfield Anchor, Jul 15. Two parallel tracks; **Go/No-Go Jul 11**; binary trust-core is the never-cut hill. |

## The trust story in one paragraph
Matching is off-chain for snappy UX (0001). The book is fully collateralized by construction —
no naked shorts, provably solvent (0002). Funds are never operator-custodied: users sign orders,
the operator cranks settlements it cannot forge or over-fill (0003). Combos inherit that trust
exactly, reading the same on-chain leg outcomes binary redemption reads, adding zero new oracle
(0004). All of it bottoms out on one question — how a TxLINE datapoint becomes a trusted on-chain
outcome — answered best by making **TxODDS the cryptographic root of settlement trust** via signed
data verified on-chain (0005). Precision markets are pool-based and economically bounded (0006).
Two engineers build it on two parallel tracks with a hard mid-point cut line (0007).

## Load-bearing open item
**Does TxLINE offer signed/attested data?** → gates ADR 0005 (d)-vs-(b). Email drafted; send Day 1.
