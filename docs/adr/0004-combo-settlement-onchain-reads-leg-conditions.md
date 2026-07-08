# ADR 0004 — RFQ combo settlement: on-chain escrow + resolve that reads leg conditions

**Status:** Accepted
**Date:** 2026-07-04

## Context
CTF *can* express `AND(legs)` — via deep/combinatorial positions (`parentCollectionId`), where a
nested position's value is the product of its leg-collections — but it fragments into 2^N illiquid
leaves, which is why **Polymarket ships no parlay product** (M3, verified). An RFQ combo is the one
2-way partition that matters: taker holds `{all N legs win}`, MM holds the complement, over the
derived condition `C = AND(legs)`. That IS a binary complete set over `C`. So a combo is a
**product/UX extension on the same CTF trust base, not a new trust primitive**. The only open
questions were representation and tiering.

## Decision

**Quote (off-chain):** MM signs a contingent quote message `{legs[], stake S, payout P,
expiry, salt, MM_vault}`. `salt` = per-quote uniqueness / single-use marker (M2 — **not** a
sequential nonce). Nothing escrowed while live — like a resting order. Discovery/negotiation is
off-chain (analogous to off-chain order matching).

**Accept (on-chain, one operator-crankable ix):** taker submits `accept` referencing the
signed quote. Atomically: verify MM sig + expiry + quote-`salt` **not yet spent** → pull `S`
from taker and `risk = P − S` from MM vault into a **combo-escrow PDA** → mark quote-`salt`
spent (hash-based, like order `OrderStatus`). Full pot `P` escrowed from two parties in one tx.

**Double-commit:** deliberately no on-chain lock at quote time (no tx per quote). The chain is
the hard backstop — `accept` debits the MM vault atomically, so the first accept wins and later
ones over the same free balance simply **revert**. Over-commitment is impossible on-chain by
construction; worst case is a taker's accept reverting. Operator **soft-locks** MM outstanding-
quote exposure off-chain to keep revert rate ~0 — but soft-lock is **UX, not the security
boundary**; the atomic on-chain debit is. (Known class, not a novel flaw — cf. *The Ghosts of
Polymarket*, arXiv 2606.16852.)

**Representation — option (b), dedicated combo-escrow, not (a) synthetic token.** (a) mints a
`COMBO-YES/NO` complete set on `C`; its only benefit over (b) is a transferable token, i.e. a
secondary market in half-settled combos. RFQ combos are bilateral and held to resolution — the
token wrapper is pure cost (a mint pair + tracking per combo). Use (b); reserve (a) for the day
combos must be transferable. Reject (c) off-chain — see seam.

**Resolve (on-chain, Tier-1) — this removes the seam.** The `resolve` ix takes the N leg
**condition PDAs as accounts**, reads their on-chain resolved outcomes directly, computes the
AND **on-chain, in the program**, and pays the escrow. Because the combo reads the *same*
condition accounts binary redemption reads, it never re-derives a leg outcome and never asks the
operator what a leg did. No new trusted party; no seam.

**Seam statement (for judges):** *Combo negotiation is off-chain; combo escrow and resolution
are on-chain, and resolution reads leg outcomes from the same condition accounts binary
redemption reads — so combos introduce no oracle beyond the per-leg oracle.*

## Consequences
- (+) Combo is trustless in exactly the way binary is; differentiator carries no extra trust.
- (+) No per-combo SPL mint; minimal surface.
- (−) Everything now rests on the **per-leg oracle** (Thread D). Binary redeem, precision
  settlement, and every combo leg bottom out on one question: how does a TxLINE datapoint become
  a trusted on-chain leg resolution, and who signs it?
