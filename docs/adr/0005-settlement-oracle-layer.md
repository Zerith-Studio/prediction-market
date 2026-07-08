# ADR 0005 — Settlement / oracle layer: root-of-trust, finality, and the build ladder

**Status:** Accepted for policy & build-ladder. The **(d)-vs-(b) branch is empirically
gated on TxODDS's reply** to the Day-1 signed-data question.
**Date:** 2026-07-04

## Context — the confession
Stripped of romance, the plain single-key settlement trust story is: *"trust our one key to
copy TxLINE honestly."* This is **worse than the custody power ADR 0003 bounded away**: custody
power lets you steal the pot; **oracle power lets you pay the pot to whichever side you choose
and call it a settlement** — economically identical to theft, laundered through the one ix the
whole system is built to honor. ADR 0003 did not eliminate the operator's god-power; it
**relocated it from the vault to the resolver key.**

For a track literally named **Settlement**, the scoring axis is "how little must we trust you at
the moment money moves." A single-key resolver answers "completely" → **good enough to not lose,
not good enough to win.** So the resolver key's power must visibly shrink.

## Decision — finality policy (independent of which oracle tier ships)

**Resolution states are three, not two:** `YES | NO | VOID`. (VOID is required for abandonment.)

**Finality rule (revisions):** settle only on a TxLINE `final=true` flag, then apply a **T+X-min
settlement delay** before escrow pays. Revisions *inside* the window supersede; the payout reads
the last value at window close. Revisions *after* payout are **out of scope and ignored** — and
this is stated out loud, because escrow is **non-clawbackable by design.** X is a UX↔safety dial:
long enough to cover TxLINE's normal revision latency (minutes), short enough to feel live. The
sin is having no finality policy, not choosing this one.

**Abandonment:** match abandoned → condition resolves **VOID** → escrow refunds each party its
own contribution.
- **Combo void rule (hackathon-honest, chosen):** any voided leg → whole combo voids → full pot
  refund. No re-pricing, no dispute surface, trivially correct.
- **Productized rule (future, named not built):** voided leg = push, pay out at remaining legs'
  odds, refund the MM the odds difference (sportsbook convention). Forces mid-life re-pricing of a
  fixed escrow pot — real work + a dispute vector. Out of scope.

**Disputes:** in tier (a) there are none — TxLINE's word is absolute. Recourse is not
bolt-on-able; it *is* the challenge window (b) or the signed source (d).

## Decision — build ladder, rank-ordered for a Settlement track
1. **(a) single resolver key** — build **first**, as the floor, so the demo is never dead. Never
   *demoed as the answer* — only as the safety net.
2. **(d) TxODDS-signed data** — **best, if reachable.** Email TxODDS Day 1. If they sign the
   payload, `resolve` verifies TxODDS's ed25519 signature **on-chain** and rejects anything not
   TxODDS-signed → operator collapses to a **pure relay**, cannot forge outcomes. Oracle trust
   becomes "trust TxODDS" — the World Cup data authority, bounty sponsor, and judge, i.e. the one
   party trusted by definition. Converts D1's confessed weakness into the headline and aligns the
   root-of-trust with the people scoring.
3. **(b) commit-reveal + challenge window (UMA-lite)** — the swing if (d) fails. ~1 day. Operator
   posts outcome, N-min window for anyone to dispute via bond, then finalize. Converts "trust our
   key" → "trust our key *unless challenged*." Genuine decentralization story.
4. **(c) full UMA optimistic oracle** — too heavy; name as the productionization north star
   (Polymarket literally uses UMA), stub not build.

**Chosen path:** floor = (a) today → swing = (d) if TxODDS signs → fallback = (b) if not →
(c) as stated north star.

## Critical caveat — D4 solves authenticity, not finality
A TxODDS signature stops **forgery**, not **revision**: a signed "14 shots" and a later signed
"13 shots" are both valid. So **(d) does not retire the finality rule.** Ship **both**: signature
verification (authenticity) **and** the signed `final=true` flag + T+X delay (finality). Close the
forgery hole and you've still left the revision hole open unless finality ships too.

## Open / empirical
The remaining open item is **not architectural** — it's the TxODDS answer, which gates (d)-vs-(b).
See `docs/txodds-day1-email.md`. Highest-leverage hour in the build.
