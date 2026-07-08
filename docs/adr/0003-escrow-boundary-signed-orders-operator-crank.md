# ADR 0003 — Escrow boundary: signed orders, operator-cranked on-chain settlement

**Status:** Accepted (refines ADR 0001's "settlement anchor" into a real settlement program)
**Date:** 2026-07-04

## Context
ADR 0001 posed a false binary: "on-chain at fill (popup per trade, laggy)" vs "off-chain
ledger (trust the database)." Polymarket dissolves it by **decoupling signatures**: the
user signs *orders* (cheap, off-chain messages); the operator signs *settlements*
(on-chain txs, pays gas). Funds are never operator-custodied.

## Decision — non-custodial, on-chain-at-fill, off-chain-matched

**Ownership of funds:** user funds stay under the user's control via an SPL `Approve`
delegate to the settlement program's PDA, **or** a per-user vault sub-account. The operator
never takes custody. The "locked in open orders" figure is off-chain **soft-locking** so the
operator won't over-commit a user.

**Per order (off-chain, silent):** user signs an ed25519 order message authorizing "move up
to `size` of my funds at price ≤/≥ `limit`." 20 orders = 20 signatures, 0 txs, 0 popups.
Optionally a session key makes even these silent.

**Per match (on-chain, operator-paid):** the operator crank submits a settlement instruction
that (a) verifies both users' order signatures (ed25519 program / sig-verify ix), (b) checks
each order's remaining fillable amount + delegate/vault limit, (c) atomically executes
NORMAL swap / MINT (`mint_set`) / MERGE (`merge_set`) against the two SPL outcome mints +
USDC vault. The user signs nothing at fill. Operator pays ~5000 lamports.

**At resolution:** oracle outcome → `redeem` converts the winning outcome token to USDC from
the vault. (Oracle = TxLINE-sourced; *how that outcome becomes trusted on-chain* is Thread D.)

**Operator's power is bounded to liveness/ordering/censorship** — it decides who matches and
when, and can refuse to match, but it can never forge a trade, exceed a signed order, or take
custody. That is the strong trust story.

## The judge sentence (strong version, now defensible)
"Your funds are safe during trading because the settlement contract moves them only per an
order you cryptographically signed, at a price at least as good as your limit; the operator
matches and pays gas but can never forge a trade, over-fill your order, or take custody."

## Consequences
- (+) Snappy demo AND genuine on-chain escrow; non-custodial; no "trust our DB" for the
  binary core.
- (−) Real Anchor program required: vault/delegate, per-market SPL outcome mints,
  `mint_set`/`merge_set`/`swap` settlement ix with **on-chain order-signature verification and
  per-order fill accounting** (order hash → remaining size, to stop operator over-fill/replay).
  Materially more than ADR 0001's 2-instruction registry.
- **OPEN (grilling):** (1) does *every* product get Tier-1 on-chain settlement, or only the
  binary core, with Precision/Combos off-chain (Tier 2) → see next ADRs. (2) Combos are **not**
  a CTF complete set — parlay collateralization/settlement has no off-the-shelf CTF answer.
  (3) Oracle: how TxLINE outcome becomes a trusted on-chain resolution (Thread D).

## Fallback — Tier 2 (only if timeline forces it)
Pure off-chain Postgres ledger, settled to chain once at market close. Legitimate hackathon
shortcut, but the judge sentence degrades to "…trust our operator's DB until close," pushing
the entire trust burden into the oracle/settlement layer (Thread D).
