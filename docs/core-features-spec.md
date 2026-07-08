# PitchMarket — Core Feature Spec & Polymarket Alignment (on Solana)

**Version:** v2 — verified against Polymarket docs/contracts on 2026-07-04. Three corrections
applied (M1/M2/M3) + two minor notes. See **Changelog** below for exactly what changed and why.

**Purpose:** pin every core mechanic precisely, mapped to the Polymarket concept (verified against
their docs/contracts), with the Solana translation beside it.

**Legend**
- ✅ **ALIGNED** — intended 1:1 parity with Polymarket's mechanic.
- 🔧 **ADAPTED** — same concept, re-implemented in Solana primitives (EVM→SVM).
- ✨ **NEW** — our extension; not a Polymarket feature.
- 🔴 **CHANGED (v2)** — corrected from v1 during verification; old value shown struck through.

---

## Changelog — what changed in v2 (read this first)

| ID | Section | v1 (was) | v2 (corrected) | Why |
|---|---|---|---|---|
| **M1** | §6 Fees | fee = `feeRateBps × size`, charged in output asset | fee = **`baseFeeRate × min(price, 1−price) × size`**, charged in output asset | The `min(price,1−price)` term is what makes the fee *symmetric* (a YES-seller @0.99 pays the same as a NO-buyer @0.01). Without it, MINT/MERGE matches become fee-asymmetric. Verified in ctf-exchange `Overview.md`. |
| **M2** | §2 / §4 | order field `nonce`; §4 plans "on-chain **nonce** invalidation" | order field **`salt`** (uniqueness/replay); cancel + fill-accounting are **hash-based** via `OrderStatus{isFilledOrCancelled, remaining}` | Polymarket **V2 removed nonce-based cancellation** in favor of hash-based order tracking. V1's `incrementNonce()` is a nuclear cancel-all with a *known race exploit*. Copying nonces re-imports a bug V2 fixed. |
| **M3** | §8 Parlays | "Polymarket has no parlays *because CTF can't express them*" | "Polymarket ships **no parlay product**; CTF **can** express `AND(legs)` via nested positions but fragments liquidity — we productize via RFQ" | CTF supports deep/combinatorial positions (`parentCollectionId`); the value of a nested position is the product of its legs. The claim "CTF can't" is false and a judge will catch it. We own the *product*, not a new trust primitive. |
| **m4** | §2 Order types | "limit + market" | GTC / GTD / FOK / FAK; market = marketable limit; **`expiry` field == GTD** | Minor: verified exact type set; surface `expiry` as GTD explicitly. |
| **m5** | §5 Statuses | had a distinct `partial` status | Polymarket has **no `partial`** — a partial fill stays `live` with reduced `remaining`; their set is `live/matched/delayed/unmatched` | Minor vocab alignment. `partial` kept only as a local convenience label. |

Everything else in the spec was **verified correct as written** — see the checklist at the bottom.

---

## 1. Collateral & token model — ✅/🔧

| Aspect | PitchMarket | Polymarket concept (verified) | Solana mapping | Status |
|---|---|---|---|---|
| Collateral | USDC (devnet), integer units | CTF collateral = USDC | SPL USDC | ✅ |
| Outcome tokens | 2 per binary market: YES, NO | CTF outcome tokens (ERC-1155), complementary | **2 SPL mints per market** | 🔧 |
| Complete-set invariant | `1 YES + 1 NO ⇄ $1`, always | `splitPosition` / `mergePositions`; complete set = $1 | `mint_set` / `merge_set` ix | ✅ |
| Price meaning | YES+NO prices sum to $1; price = P(YES), 1–99¢ | Prices sum to $1 | same | ✅ |
| Redemption | Winning token → $1, losing → $0 | `redeemPositions` vs escrowed collateral; payouts set by oracle `reportPayouts` | `redeem` ix vs vault | ✅ |

**Verified:** complete-set = $1 split/merge/redeem is exactly this. ERC-1155 (one contract, many
token IDs) → **2 SPL mint accounts per market** is an intentional SVM adaptation (SPL has no
multi-token 1155). Trust properties preserved.

---

## 2. Order model — ✅/🔧 · 🔴 CHANGED (M2, m4)

| Aspect | PitchMarket | Polymarket concept (verified) | Solana mapping | Status |
|---|---|---|---|---|
| Order = signed message, not a tx | User signs an off-chain order; no gas, no popup | EIP-712 signed limit order | **ed25519-signed order message** | 🔧 |
| BUY vs SELL by asset provided | BUY provides USDC→receives token; SELL provides token→receives USDC | Order `side` BUY/SELL; maker/takerAsset | same semantics | ✅ |
| Fields 🔴 | `market, outcome, side, price, size, expiry, `**`salt`**`, maker` (was `nonce`) | maker, tokenId, maker/takerAmount, side, feeRateBps, **expiration**, **salt**, signature | analogous struct | ✅ |
| Entry-time collateral | BUY locks `price×size(+fee)` USDC; SELL locks `size` tokens; SELL w/o tokens **rejected** | allowance checked; **no naked shorts** | delegate/vault balance check | ✅ |
| Order types 🔴 | **GTC** (rest), **GTD** (`expiry`), **FOK**, **FAK**; market order = marketable limit | GTC / GTD / FOK / FAK; market = marketable limit | same | ✅ |

**Verified:** SELL requires holding the outcome token (no naked short). "Market order" = a limit
order at a marketable price (executes immediately at best book price). Your `expiry` field **is**
GTD — expose it as such.

**🔴 M2:** replaced `nonce` with **`salt`** (per-order uniqueness + signature-replay protection).
Do **not** use a sequential nonce — see §4.

---

## 3. Matching engine (off-chain) — ✅

| Aspect | PitchMarket | Polymarket concept (verified) | Solana mapping | Status |
|---|---|---|---|---|
| Where matching happens | Off-chain, in Go; one writer goroutine per market | Off-chain operator matching | same | ✅ |
| Price-time priority, partial fills | Yes | Yes | same | ✅ |
| **Three match types** | **NORMAL** (BUY vs SELL swap), **MINT** (BUY YES + BUY NO → split/mint), **MERGE** (SELL YES + SELL NO → merge) | Exchange match types NORMAL/COMPLEMENTARY, MINT, MERGE | executed in settlement ix | ✅ |
| Taker hits mixed makers | One taker may hit NORMAL + MINT makers in one settlement, **never MINT + MERGE** together | Same batching rule | same | ✅ |

**Verified (load-bearing):** CTFExchange genuinely supports **MINT** (pairing two buyers) and
**MERGE** (pairing two sellers), not just BUY-vs-SELL. A single settlement may combine
COMPLEMENTARY + MINT makers but **never MINT + MERGE**. This is what lets an order book fill
without a natural counterparty. Confirmed verbatim in the exchange docs.

---

## 4. Settlement & custody (on-chain) — 🔧 (mechanics) / ✅ (trust shape) · 🔴 CHANGED (M2)

| Aspect | PitchMarket | Polymarket concept (verified) | Solana mapping | Status |
|---|---|---|---|---|
| Who signs the on-chain tx | **Operator** cranks settlement, pays fees; user signs nothing at fill | Operator submits `matchOrders`, pays gas | operator crank = fee payer | ✅ |
| Custody | Non-custodial; funds under user's **allowance/delegate**, not operator-held | ERC-20/1155 allowance to Exchange; funds stay in user (proxy) wallet | **SPL `Approve` delegate** or per-user vault PDA | 🔧 |
| On-chain at fill? | Yes — settlement moves funds the moment a match settles (not batched at close) | Yes — `matchOrders` transfers on-chain per match | settlement ix per match | ✅ |
| Operator power | liveness / ordering / censorship only; **cannot forge or over-fill** | same bounded power | same | ✅ |
| Anti-over-fill / replay 🔴 | **Hash-based** `OrderStatus{ isFilledOrCancelled, remaining }` keyed by order hash; partial fill decrements `remaining`; **`salt`** gives signature-replay protection | Orders keyed by hash → `OrderStatus{filled/remaining}`; **V2 is hash-based, not nonce-based** | fill-accounting account keyed by order hash | ✅ |

**Verified:** operator is a non-custodial fee-payer; funds stay in the user's (proxy) wallet under
allowance; each match settles on-chain immediately.

**🔴 M2 detail — do NOT use nonces for cancellation:**
- Per-order fill accounting = **order hash → `OrderStatus{ isFilledOrCancelled, remaining }`**. Over-fill impossible (`remaining` can't go negative; flips `isFilledOrCancelled`).
- Replay protection = order **hash uniqueness via `salt`** + the once-filled/cancelled flag.
- **Cancellation is hash-based** (surgical, per order). Polymarket **V2 removed** `incrementNonce()`. V1's nonce cancel-all is nuclear and has a *known race exploit* — do not replicate. If you want cancel-all, make it an explicit, separate, documented op.

---

## 5. Order & position lifecycle — ✅ · 🔴 note (m5)

| State | PitchMarket | Polymarket (verified) |
|---|---|---|
| Unmatched / resting | `open` | `live` |
| Partially filled 🔴 | `partial` *(local label only)* | **still `live`** with reduced `remaining` (no distinct `partial` status) |
| Fully filled | `filled` | `matched` |
| Cancelled | `cancelled` (off-chain cancel; **hash-based** on-chain invalidation if needed) | cancel (hash-based) |
| (marketable, delayed) | *(not implemented)* | `delayed` / `unmatched` (match-delay states) |
| Resolved position | position marked resolved on market resolution | post-resolution |
| Settled / claimable | `redeem` credited | redeemed |

**Verified:** Polymarket insert statuses are `live / matched / delayed / unmatched` (+ cancel).
There is **no `partial`** — keep it only as an internal convenience.

---

## 6. Fees — 🔧 · 🔴 CHANGED (M1)

| Aspect | PitchMarket | Polymarket concept (verified) | Status |
|---|---|---|---|
| Trading fee 🔴 | `fee = baseFeeRate × min(price, 1−price) × size`, charged in **output asset**; **0 for the demo** | Same symmetric formula; `feeRateBps` often 0 | ✅ mechanism / 🔧 value |
| Precision rake | Small rake off the pool (anti-carpet, ADR 0006) | *no Polymarket equivalent* | ✨ |

**🔴 M1 — the formula, verified:**
```
fee_usdc = baseFeeRate × min(price, 1 − price) × outcomeShareCount
  • Sell tokens → collateral:   fee = baseRate × min(p, 1−p) × size
  • Buy tokens with collateral: fee = baseRate × min(p, 1−p) × (size / p)
```
The `min(price, 1−price)` term makes the fee **symmetric**: selling 100 of A @ $0.99 pays the same
fee value as buying 100 of A′ @ $0.01 (required because mint/merge can happen at any time).
`baseFeeRate` is 2× the fee rate at equilibrium ($0.50). Charged in the **output/proceeds** token
(received tokens on a buy, received collateral on a sell). We set it to 0 for the demo but keep the
field + formula so the model matches Polymarket.

---

## 7. Resolution & oracle — ⚠️ **DELIBERATE DIVERGENCE** (verified shape)

The **one place we intentionally do NOT copy Polymarket** — our headline for a "Settlement" track.

| Aspect | PitchMarket | Polymarket (verified) | Status |
|---|---|---|---|
| Oracle | **TxODDS / TxLINE** football data as resolution source | UMA Optimistic Oracle (OOv2) via `UmaCtfAdapter` + DVM disputes | ✨ divergent |
| Trust root (tier d, best) | **TxODDS cryptographically signs the data; `resolve` ix verifies the signature on-chain** → operator is a pure relay, cannot forge | UMA proposer + economic dispute game | ✨ |
| Fallback (tier b) | **Bonded challenge window** (UMA-lite): operator posts outcome, N-min window for a bonded dispute, then finalize | UMA *is* this shape (proposer bond + liveness) | 🔧 (same shape) |
| Floor (tier a) | Single-key resolver (demo floor only, never the headline) | n/a | ✨ |
| Finality | Settle on TxLINE `final=true` + **T+X-min delay**; post-payout revisions ignored (non-clawbackable) | UMA liveness window | 🔧 (same shape) |
| Void state | `YES / NO / VOID`; abandonment → VOID → refunds | UMA can return 50/50 / unresolvable | 🔧 |

**Verified:** Polymarket resolves via **UMA's Optimistic Oracle** — a proposer posts an outcome with
a bond, a liveness/challenge window runs, and disputes escalate to UMA's DVM (token-holder vote).
Our tier-b bonded window mirrors this proven shape; our TxODDS-signed on-chain root is a genuine,
ownable divergence: *"Polymarket uses UMA; we make the World Cup data authority (TxODDS) the signed
on-chain root — same optimistic shape, sport-specific source."*

> **Note (from Thread D):** a TxODDS signature solves *authenticity* (operator can't forge), not
> *finality* (revisions). Complete answer = **TxODDS signature + a signed `final=true` flag**;
> settle only after the signed-final message, then T+X delay.

---

## 8. RFQ combos / parlays — ✨ **NEW** · 🔴 CHANGED (M3)

| Aspect | PitchMarket | Polymarket (verified) | Status |
|---|---|---|---|
| Parlays 🔴 | Multi-leg combo via **RFQ**: taker bundles legs, MM/bot quotes, taker accepts | **No parlay product.** (CTF *can* express `AND(legs)` via nested positions but fragments into 2^N illiquid leaves, so Polymarket doesn't productize it.) | ✨ (product, not new trust) |
| Collateralization | Combo = binary set over `C = AND(legs)`; full pot escrowed at accept in a combo-escrow PDA (option **b**) | n/a | ✨ |
| Settlement | On-chain `resolve` reads the **same leg-condition accounts** binary redeem reads → **no new oracle** | n/a | ✨ |
| Double-commit guard | On-chain atomic vault debit at accept = hard backstop (Nth over-committing accept **reverts**); operator soft-locks off-chain for UX | n/a | ✨ |
| Leg compatibility | Mutex groups reject impossible leg combos | n/a | ✨ |

**🔴 M3 — precise claim:** Polymarket offers **no native parlay product** — we own that. But do
**not** say "CTF can't express parlays": CTF supports deep/combinatorial positions
(`parentCollectionId`), where a nested position's value is the *product* of its leg-collections.
Our RFQ combo is a **product/UX extension on the same CTF trust base** (it inherits CTF's
collateralization + reads on-chain leg outcomes), **not a new trust primitive**.

---

## 9. Precision markets — ✨ **NEW**

| Aspect | PitchMarket | Polymarket (verified) | Status |
|---|---|---|---|
| Format | Pool-based **numeric** prediction of an exact stat; **σ-normalized** error-weighted payout | Binary/categorical only; numeric questions are **bucketed binary** markets, not a closeness pool | ✨ |
| Score formula | `score = 1 / (1 + |guess − actual| / s)^k`, `s` = per-template scale (σ or range) | n/a | ✨ |
| Anti-gaming | **Kickoff-lock** + one-entry-per-wallet + rake + formula convexity | n/a | ✨ |
| Pool seeding | **Crowd-seeding bot** (simulated retail around fair value; not a strategic/carpet player) | n/a | ✨ |

**Verified:** Polymarket has **no closeness-pool product** — numeric outcomes are expressed as
bucketed binary/categorical markets on the CLOB. We own the Trepa-style format. (Fallback framing
for a skeptical judge: *"the priced-bucket version would be carpet-proof and live-safe for free; we
chose the pool for the closeness UX and bounded its exploits."*)

---

## 10. Market-making & crowd-seeding bot — 🔧/✨

| Aspect | PitchMarket | Polymarket concept (verified) | Status |
|---|---|---|---|
| Binary MM | Bot quotes bid/ask around TxLINE-implied fair price; inventory limits | Polymarket has external MMs + a liquidity-rewards program | 🔧 (we automate it) |
| RFQ quoting | Bot prices combos (product of leg probs × margin, exposure-capped) | n/a | ✨ |
| Precision seeding | Bot simulates a retail **crowd** around fair value (not a carpet-better) | n/a | ✨ |

---

## 11. TxLINE data integration & auto lifecycle — 🔧/✨

TxODDS/**TxLINE** is the spine — it drives creation, pricing, resolution, and the oracle root.

| Function | PitchMarket use of TxLINE | Status |
|---|---|---|
| Fixtures | Detect upcoming matches → auto-instantiate market templates | ✨ |
| Live state | Feed shots/goals → MM bot re-pricing + one-liner context + live leg tracking | ✨ |
| Resolution input | `final=true` stats → binary outcome, precision actual value, combo leg outcomes | 🔧 (Polymarket-shaped, TxLINE-sourced) |
| **Oracle root (tier d)** | **TxODDS-signed payload verified on-chain** in `resolve` → TxODDS is the cryptographic settlement authority | ✨ (headline) |
| Feed abstraction | Behind a `FeedProvider` interface; `replay` provider = demo safety net | — |
| **OPEN** | Does TxLINE offer **signed/attested** data? Gates oracle tier d-vs-b — see `txodds-day1-email.md` | ⚠️ |

---

## 12. Portfolio — 🔧

All three product types in one view: binary (open orders, filled positions, avg entry, value,
claimable), combos (legs, quote, maker, leg status, settlement), precision (guess, actual, score,
pool share, payout). Polymarket has a positions/portfolio view for binary; combos + precision
sections are ✨ ours.

---

## Alignment summary

**Exchange core: 1:1 with Polymarket, deliberately.** Collateral/complete-sets, the signed-order
CLOB, the three match types (NORMAL/MINT/MERGE), non-custodial operator-cranked on-chain
settlement, and redemption are intended 1:1 with Polymarket's CTF + CLOB, re-expressed in Solana
primitives (2 SPL mints instead of ERC-1155; ed25519 orders instead of EIP-712; **`salt` +
hash-based order status** instead of ERC allowances-with-nonces; operator crank instead of
`matchOrders`). **We diverge on purpose in exactly one place — the oracle:** Polymarket uses UMA;
we make **TxODDS/TxLINE the signed on-chain root of settlement trust** (same optimistic/challenge
shape, sport-specific authority). **We extend beyond Polymarket in two places — RFQ combos and
Precision markets** — both built on top of the same CTF trust base so they inherit its guarantees.

---

## Verification checklist — RESULTS (checked against Polymarket docs/contracts, 2026-07-04)

| ✔ | Item | Result |
|---|---|---|
| ✅ | CTF: complete-set = $1, split/merge/redeem semantics (§1) | **Match.** Verified in ctf-exchange `Overview.md` + Gnosis dev guide. |
| ✅ | Exchange supports **MINT + MERGE**, not just BUY-vs-SELL (§3) — load-bearing | **Confirmed verbatim.** COMPLEMENTARY + MINT can combine in one settlement; never MINT+MERGE. |
| ✅ | Signed-order + allowance + operator-cranked non-custodial model (§2/§4) | **Match.** Operator submits `matchOrders` & pays gas; funds stay in user/proxy wallet. |
| 🔴 | On-chain fill-accounting / replay protection (§4) | **Corrected (M2).** It's **hash-based** `OrderStatus{filled,remaining}` + **`salt`**; V2 removed nonces (V1 nonce cancel-all has a known race). |
| ✅ | UMA resolution + dispute-window shape (§7) | **Confirmed.** Proposer + bond + liveness + DVM disputes; tier-b mirrors it. |
| 🔴 | Fee model = feeRateBps in output asset (§6) | **Corrected (M1).** Output-asset ✅ **but formula needs `min(price,1−price)` term** for symmetry. |
| 🔴 | No native parlays (§8) | **Corrected (M3).** No parlay *product* ✅ — but CTF *can* express `AND(legs)` (nested positions); reworded the claim. |
| 🔴 | Order-status vocabulary (§5) | **Minor (m5).** `live/matched/delayed/unmatched`; **no `partial`** status. |
| ✅ | Order types / "market order = marketable limit" (§2) | **Confirmed (m4).** GTC/GTD/FOK/FAK; `expiry` == GTD. |
| ✅ | Confirm Polymarket has **no** closeness-pool (§9) | **Confirmed.** Numeric = bucketed binary; we own the pool format. |
| ⬜ | Send TxODDS the signed-data email (§11) | **Still open — empirical.** Gates oracle tier d-vs-b. |

**Net:** core is faithful; only three edits were needed (M1 fee formula, M2 nonce→hash/salt, M3
parlay wording) — all spec corrections, none architectural. This v2 doc has them applied. Ready to
regenerate `PROJECT_PLAN.md` + interface contract from here.

### Sources
- Polymarket ctf-exchange `docs/Overview.md` — match types, fee symmetry, complete sets
- Polymarket CLOB — orders (types & statuses)
- Polymarket CLOB — introduction (non-custodial operator model)
- ctf-exchange `Trading.sol` (hash-based OrderStatus)
- polymarket-nonce-guard (V1 nonce race exploit)
- Gnosis Conditional Tokens dev guide (deep/combinatorial positions)
