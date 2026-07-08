# ADR 0006 — Precision market economics: anti-gaming, entry timing, seeding, scoring

**Status:** Accepted
**Date:** 2026-07-04

## Context
Precision markets are pool-based (no MM, winners paid by losers), payout
`= Pool × (stakeᵢ·scoreᵢ)/Σ(stake·score)`. The on-chain oracle work (ADR 0005) makes them
*resolve* honestly but does nothing about *economic* gaming. Four exposures grilled.

## Decision

**C1 — Carpet-betting (spreading guesses to capture the pool).**
- The formula already penalizes spread: with `k≥2`, a carpeter blanketing 11 integers at $1 pays
  $11 but only 1–2 near-entries score → ≈0.18 score/$ vs a caster who nails it at 1.0 score/$ —
  **~5× worse efficiency.** Carpeting = buying variance reduction at a premium, only +EV against a
  dispersed bad field.
- Guards shipped anyway (don't rely on casuals being sharp on camera):
  1. **One entry per pool per wallet** (cf. Trepa). Kills carpeting; trivially explained. Sybil-
     across-wallets is **out of MVP scope** — stated honestly; entry cap + formula convexity are the
     backstop.
  2. **Rake / entry fee** → marginal blanket integers go −EV a few off the mean.
  3. *(Pocket, not shipped)* winner-take-all per bucket (true parimutuel tote) is structurally
     carpet-immune but loses the graded-closeness charm. The "manipulation-proof at scale" answer.

**C2 — Entry closes at kickoff. Non-negotiable.** A closeness pool has no price, so it cannot
absorb information; every second open live is pure TxLINE-latency arb on the entry side (at 80′
with shots=13, bet 13). Lock at kickoff → precision = pure pre-whistle forecasting. In-play
precision would require becoming a *priced* scalar/CLOB market (only a price charges late entrants
fair value) — explicitly future, not MVP.

**C3 — Pool seeding: crowd, not shark.** The bot can't "make" a pool (no bid/ask). It instead
**crowd-seeds**: simulates N independent retail personas, each one stake sampled around the
TxLINE-implied fair value with realistic noise (mean ± σ) → believable bell-shaped distribution +
live leaderboard on camera. It is **demo-population, not a strategic player** — no blanketing, not
playing to win. Demo script states this out loud, which defuses the C1×C3 interaction (same bot is
a villain as a carpeter, fine as a crowd).

**C4 — σ-normalized score (fixes scale-blind `k`).**
```
score = 1 / (1 + |guess − actual| / s)^k
```
`s` = the template's characteristic scale (σ or stored range constant — per-template metadata
already exists). Goals off-by-1 (s≈2) → `1/1.5^k`; passes off-by-40 (s≈100) → `1/1.4^k` — both
graded, no field-of-zeros. One global `k` now works across all stats. Chosen over per-template-`k`
(fewer knobs) and over restricting to small-range stats (a cop-out for a one-line fix).

## Consequences
- (+) Pool is demo-defensible: {kickoff lock + one-entry-per-wallet + rake + σ-normalized score +
  crowd-seeding bot}.
- (+) Honest scale story: *"Polymarket would price this as bucketed/scalar CLOB markets, removing
  carpeting and live-arb structurally; we chose the pool for the closeness UX and bounded its
  exploits for the MVP"* — turns C1–C2 into demonstrated understanding.
- (−) Cross-wallet sybil remains open by design (acknowledged, out of scope).
- VOID/finality (ADR 0005) apply: abandoned match → precision pool VOID → refund each stake.
