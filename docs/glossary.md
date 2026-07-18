# Glossary — PitchMarket

Living domain vocabulary. Terms are precise and should be used consistently in code, DB, API, and UI. Add/refine as the design is grilled.

| Term | Definition | Notes / invariants |
|---|---|---|
| **Market** | A single resolvable question tied to one match. Type is `binary` or `precision`. | Has a lifecycle: `draft → open → closed → resolving → settled` (or `void`). |
| **Binary market** | YES/NO question priced on a CLOB. YES + NO settle to exactly $1. | Price = implied P(YES) in integer cents 1–99. |
| **Precision market** | Pool-based numeric prediction of an exact stat value. | Payout = error- and stake-weighted share of pool. |
| **Combo / Parlay** | Multiple legs bundled into one position; pays only if **all** legs win. | Priced via RFQ, not by routing each leg through its book. |
| **Leg** | One prediction inside a combo, referencing a market + predicted outcome. | Subject to compatibility rules (mutex groups). |
| **RFQ** | Request-for-Quote: taker requests a price for a combo; makers/bot quote. | Quote has an expiry; taker accepts or rejects. |
| **CLOB** | Central Limit Order Book. Off-chain, in Go, one writer goroutine per market. | Source of truth is Postgres; book rebuilt on boot. |
| **Share** | A unit of a binary outcome. 1 YES share pays $1 if YES resolves true, else $0. | YES+NO pair is always fully collateralized to $1. |
| **Pair mint** | Creating 1 YES + 1 NO from $1 collateral when complementary orders match. | Keeps the book collateralized without an AMM. |
| **Ledger** | Off-chain custodial balance system (integer cents). | Tracks `balance`, `locked`; double-entry audit trail. |
| **Resolver** | Service that closes markets and computes outcomes from verified match data. | Holds the `resolver_authority` key for on-chain posting. |
| **Settlement** | Applying outcomes to positions → payouts credited in ledger. | Also settled on Solana: outcome via `resolve_market`, positions via `settle_match` / `redeem`. |
| **Resolution Registry** | Solana devnet Anchor program storing each market's `(market_id, outcome)` + on-chain settlement/redemption. | Makes results independently verifiable. |
| **MM bot** | Automated market maker: quotes binary books and answers RFQs. | Counterparty model UNDER GRILLING (see open questions). |
| **Feed provider** | Interface over match data; `txodds` (live) or `replay` (recorded) impl. | Demo safety net = replay. |
| **Precision score** | σ-normalized closeness weight: `1/(1 + \|guess−actual\|/s)^k`, `s` = per-template scale. | Multiplied by stake → pool weight. σ-norm makes one `k` work across all stats (ADR 0006). |
| **Crowd-seeding** | Bot simulates N retail personas around TxLINE fair value (mean±σ) to populate a precision pool. | Demo-population, not a strategic player — distinct from carpet-betting. |
| **Carpet-betting** | One actor blanketing the outcome space to capture a pool. | Countered by one-entry-per-wallet + rake + formula convexity (ADR 0006). |
| **Signed order** | Off-chain ed25519 message authorizing "move ≤ size at price ≤/≥ limit." | No gas, no popup. Verified on-chain at settlement. Not a transaction. |
| **Delegate / vault** | SPL `Approve` delegate (or per-user vault PDA) giving the settlement program bounded spend authority. | Funds stay non-custodial; operator never holds them. |
| **Operator crank** | Off-chain service that submits the on-chain settlement ix per match and pays fees. | Power bounded to liveness/ordering/censorship; cannot forge or over-fill. |
| **Soft-lock** | Off-chain reservation of a user's balance against resting orders. | Prevents over-commit; not an on-chain escrow while the order rests. |
| **`mint_set` / `merge_set`** | Program ix implementing CTF split ($1→YES+NO) / merge (YES+NO→$1). | Called inside the settlement ix for MINT/MERGE matches. |
| **Fill accounting** | On-chain per-order record (order hash → remaining size). | Stops the operator replaying/over-filling a signed order beyond its authorized size. |
| **Complete set** | 1 YES + 1 NO of the *same* binary market = $1 of collateral. | A combo is a complete set over the *derived* condition `C=AND(legs)`; CTF can express it via nested positions but fragments (2^N leaves), so we productize via RFQ (ADR 0004, M3). |
| **`salt`** | Per-order / per-quote uniqueness + signature-replay marker. | **Not** a sequential nonce. Cancellation & fill-tracking are hash-based (`OrderStatus`), per Polymarket V2 (spec M2). |
| **`OrderStatus`** | On-chain per-order record `{ isFilledOrCancelled, remaining }` keyed by order hash. | Over-fill impossible (`remaining` can't go negative). Replaces nonce-based cancel-all (which has a known race). |
| **Fee formula** | `fee = baseFeeRate × min(price, 1−price) × size`, charged in output asset. | `min(p,1−p)` makes it symmetric across MINT/MERGE; 0 for the demo (spec M1). |
| **TxLINE** | TxODDS's live football data delivery layer; the oracle input feeding resolution/redeem. | Trust path pinned in ADR 0005; empirically gated on signed-data reply. |
| **Resolution state** | A condition resolves to `YES`, `NO`, or **`VOID`**. | VOID (abandonment) refunds each party its own contribution. Combo: any voided leg → whole combo voids. |
| **Finality rule** | Settle only on TxLINE `final=true`, then a **T+X-min delay**; revisions inside window supersede, post-payout revisions ignored. | Escrow is non-clawbackable by design. Authenticity ≠ finality. |
| **Signed-source oracle (tier d)** | `resolve` ix verifies TxODDS's ed25519 signature on-chain; operator can only relay, not forge. | Root of trust = TxODDS (sponsor). Best tier; gated on TxODDS offering signed feeds. |
| **Challenge window (tier b)** | Operator posts outcome; N-min bonded dispute window; then finalize. | UMA-lite fallback if TxODDS won't sign. "Trust our key unless challenged." |
| **Resolver authority** | The key that writes a condition's outcome on-chain. | In tier (a) it is the operator's god-power; tiers (b)/(d) shrink it. |

_Terms marked "UNDER GRILLING / pending" have unresolved design questions being interrogated now._
