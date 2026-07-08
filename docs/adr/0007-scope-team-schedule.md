# ADR 0007 — Scope, team, and the two-track schedule

**Status:** Accepted
**Date:** 2026-07-04

## Context
Team = **2 engineers, both strong + Anchor-fluent + fullstack.** Internal deadline **Jul 15**
(submission window runs to Jul 29 → ~2 weeks buffer, good). Anchor program is built **greenfield**
(F3), no fork. The grilling made the design more correct *and* more expensive (ADR 0003 ≫ a
2-instruction registry). Blunt math: **11 days × 2 = ~22 engineer-days**, and the settlement
program is a full-time job for one dev the whole window → the other engineer's bucket is
**overloaded**, so a cut line is structural, not hypothetical. Team agreed (F2) the **binary
trust-core is the hill.**

## Decision — two parallel tracks with a defined interface contract

**Day 0 (Jul 4–5, shared):** repo + schema + Privy auth + feed interface & `replay` provider;
**send the TxODDS signed-data email today** (gates oracle tier, ADR 0005); and freeze the
**cross-track interface contract**: settlement ix signatures, the signed-order message format, and
the condition-account layout. This contract is what lets the two tracks proceed without blocking.

**Track E1 — Anchor / settlement (owns ADRs 0003–0005):**
- Jul 5–7: vault/delegate, per-market SPL outcome mints, `mint_set`/`merge_set`/swap settlement
  ix, **tier-(a) single-key resolver** → binary settlement works on devnet via a test harness.
- Jul 8–9: **on-chain ed25519 order-sig verification + per-order fill-accounting** ← longest pole.
- Jul 10–11: combo-escrow PDA + `resolve` ix reading N leg-condition accounts + `redeem` + VOID.
- Jul 12–13: oracle tier — **(d)** TxODDS-sig verify if they sign, else **(b)** challenge window.
- Jul 14–15: hardening + crank integration + buffer.

**Track E2 — Go engine + off-chain + frontend:**
- Jul 5–7: matching engine (NORMAL/MINT/MERGE pairing) + ledger soft-lock + operator crank
  skeleton (calls E1 ix) + order API + WS hub.
- Jul 8–9: binary market frontend (order book, trade panel) end-to-end vs engine+crank on devnet.
  → **demoable floor by ~Jul 9–10.**
- Jul 10–11: RFQ negotiation + MM/crowd-seeding bot + combo builder UI.
- Jul 12: precision (kickoff-lock, one-entry, rake, σ-score) + pool UI + crowd-seed bot.
- Jul 13: portfolio (3 sections) + settlement/verify page.
- Jul 14: one-liner agent (if time) + polish. Jul 15: dress rehearsal + record.

## Go / No-Go checkpoint — **Jul 11 EOD**
If E1's binary settlement **through fill-accounting** isn't working end-to-end on devnet by Jul 11,
invoke the cut: ship **binary-on-chain-trustless only** (the F2 hill), drop combos to Tier-2
off-chain (or cut), keep precision Tier-2 off-chain, cut one-liner/NFT. The tier-(a) floor stays
demoable throughout — never a "nothing runs" night before submission.

## Cut order (first overboard → last)
NFT → one-liner agent → combos-on-chain (→ combos off-chain → cut combos) → precision-in-demo
polish → multi-match scale. **Never cut:** one match, one binary market, fully on-chain-trustless
end-to-end (signed order → crank → settlement → resolve → redeem).

## Named risks
- **Overloaded E2 bucket** — some of {one-liner, NFT, polish, precision-in-demo} realistically
  slips. That's the cuttable tail by design; name it as a choice, not a surprise.
- **Greenfield ed25519 + fill-accounting** is the critical path; slip here compresses combo+oracle.
- **TxODDS reply latency** gates (d)-vs-(b); tier-(a) floor de-risks it regardless.
