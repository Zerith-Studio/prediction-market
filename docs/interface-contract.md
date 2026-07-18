# PitchMarket — Cross-Track Interface Contract (v1)

**Freeze this Day 0.** It is the boundary between **E1 (Anchor/settlement)** and **E2
(Go engine + operator crank + frontend)**. Both engineers code against this; changes require a
joint edit. Derived from `core-features-spec.md` v2 + ADRs 0002–0005. Money = integer **micro-USDC**
(1 USDC = 1_000_000) on-chain; UI shows cents. Price = integer **1..99** (¢, = P(YES)).

---

## 0. Shared identifiers & units
- `market_id: [u8;32]` — deterministic hash of `(match_id, template_key)`.
- `condition = Market PDA` — seeds `["market", market_id]`; holds outcome + resolution state.
- Outcome mints: `yes_mint`, `no_mint` — PDAs seeds `["yes", market_id]` / `["no", market_id]`.
- `order_hash: [u8;32]` — hash of the canonical order message (§1). Primary key everywhere.
- `quote_hash: [u8;32]` — hash of the canonical combo quote (§2).
- Amounts: `u64` micro-USDC. Shares: `u64` (1 share redeems to 1 USDC if its outcome wins).

---

## 1. Signed order message (ed25519) — the E2→user→E1 contract
Canonical borsh-serialized struct the **user** signs client-side; E2 stores it, the crank passes
it into `settle_match`, and E1 verifies the signature on-chain.

```rust
struct Order {
  maker:      Pubkey,   // user
  market_id:  [u8;32],
  outcome:    u8,       // 0 = NO, 1 = YES
  side:       u8,       // 0 = BUY (provide USDC), 1 = SELL (provide tokens)
  price:      u16,      // 1..99  (¢, implied P of `outcome`)
  size:       u64,      // shares
  fee_bps:    u16,      // baseFeeRate; 0 for demo (fee = fee_bps·min(p,1-p)·size, output asset)
  expiry:     i64,      // unix; 0 = GTC, else GTD
  salt:       u64,      // per-order uniqueness / replay marker (NOT a sequential nonce)
}
// signed = ed25519(maker_privkey, borsh(Order))
```
**Rules (both tracks enforce):** BUY requires `available_USDC ≥ price·size + fee`; SELL requires
`token_balance(outcome) ≥ size`. SELL without tokens is rejected **at E2 entry** and would revert
at E1. `order_hash = sha256(borsh(Order))`.

---

## 2. Signed combo quote message (ed25519) — MM/bot → taker
```rust
struct ComboQuote {
  maker:      Pubkey,        // MM / bot vault owner
  legs:       Vec<Leg>,      // Leg { market_id:[u8;32], outcome:u8 }
  stake:      u64,           // taker pays (micro-USDC)
  payout:     u64,           // total pot P; MM risk = payout - stake
  expiry:     i64,
  salt:       u64,           // single-use; marked spent on accept (hash-based, not a nonce)
}
```

---

## 3. On-chain accounts (E1 owns; E2 reads via RPC)
| Account | Seeds | Fields |
|---|---|---|
| `Market` (condition) | `["market", market_id]` | `outcome: {Unresolved,Yes,No,Void}`, `resolver_authority`, `resolved_at`, `oracle_tier` |
| `OrderStatus` | `["ostatus", order_hash]` | `is_filled_or_cancelled: bool`, `remaining: u64` |
| `Vault` (per-user) | `["vault", user]` | `usdc: u64` (or SPL delegate model — pick one Day 0, see §6) |
| `ComboEscrow` | `["combo", quote_hash]` | `taker`, `maker`, `legs`, `stake`, `payout`, `status` |
| `QuoteStatus` | `["qstatus", quote_hash]` | `spent: bool` |

---

## 4. Program instructions (E1 surface; E2 crank calls these)
```
initialize_market(market_id, template_meta)         → creates Market + yes_mint + no_mint
settle_match(taker: Order+sig, makers: []Order+sig, match_type, fills[])
     match_type ∈ {NORMAL, MINT, MERGE}; verifies sigs, checks/decrements OrderStatus.remaining,
     moves USDC/shares per §1 rules, mints (MINT) / merges (MERGE). Operator is fee payer.
cancel_order(order_hash, maker_sig)                 → sets OrderStatus.is_filled_or_cancelled
combo_accept(quote: ComboQuote+sig, taker_sig)      → verify sig+expiry+QuoteStatus.!spent;
     pull stake from taker, payout-stake from MM vault → ComboEscrow; mark QuoteStatus.spent
resolve_market(market_id, outcome, oracle_proof)    → tier a: resolver key | tier b: post+window
     | tier d: verify TxODDS ed25519 sig + signed final=true flag. Sets Market.outcome.
redeem(market_id, user)                             → winning shares → USDC from vault
resolve_combo(quote_hash)                            → reads N leg Market PDAs, computes AND,
     pays ComboEscrow to taker (all win) or MM (else); VOID leg → refund both
```
**Crank protocol (E2→E1):** after the Go matching engine produces a match, the operator builds one
`settle_match` tx with the taker order, the matched maker orders, the `match_type`, and the per-leg
`fills[]`, passing the `OrderStatus` PDAs + `Vault`s + mints as accounts. One tx per match.

---

## 5. Off-chain surface (E2 owns; frontend contract)
REST: `POST /orders` (submit signed Order), `DELETE /orders/:hash`, `GET /markets/:id/book`,
`POST /combos` (RFQ), `POST /combos/:id/accept`, `POST /markets/:id/precision`,
`GET /portfolio`, `POST /wallet/deposit`, `GET /watchlist`, `POST /watchlist`,
`DELETE /watchlist/:market_id`. WS `/ws`: `book_update`, `fill`, `order_update`,
`combo_quote`, `match_state`, `oneliner`. (Full list in regenerated PROJECT_PLAN.md.)

### 5.1 Watchlist routes
- `GET /watchlist?wallet=` → `{ market_ids: string[] }` — 64-hex market ids, newest-first.
- `POST /watchlist` body `{ wallet, market_id }` → `{ ok: true }` — favourites a market.
- `DELETE /watchlist/{market_id}?wallet=` → `{ ok: true }` — unfavourites a market.

`wallet` is base58; `market_id` is 64-char hex. Unknown `market_id` on `POST` → 400.

---

## 6. Two Day-0 decisions to lock before coding
1. **Custody model:** SPL `Approve` **delegate** (funds stay in user's ATA) **vs** per-user
   **Vault PDA** (deposit once). Delegate = more faithful to Polymarket; Vault = simpler crank.
   **Recommend Vault PDA for the hackathon** (one deposit tx, simpler accounts). Pick one.
2. **Reconciliation:** E2 soft-locks are UX only; the chain is truth. On a `settle_match` /
   `combo_accept` **revert**, E2 must unwind its soft-lock and re-emit `order_update`. Define the
   revert→reconcile path Day 0 so a losing race is a no-op, not a stuck order.

---

## 6.5. settle_match / combo_accept transaction layout (pinned 2026-07-09; v0 pinned 2026-07-12)

Ed25519 signature verification is on-chain instruction introspection, not a program
account. The crank MUST build each settle_match tx as exactly:
```
ix[0] = Ed25519Program.createInstructionWithPublicKey(taker.maker, borsh(taker), taker_sig)
ix[1] = Ed25519Program.createInstructionWithPublicKey(maker.maker, borsh(maker), maker_sig)
ix[2] = settle_match(...)
```
`settle_match` reads ix[0]/ix[1] via the instructions sysvar and asserts each one's
(pubkey, message) matches (order.maker, borsh(order)) exactly — see
`programs/pitchmarket/src/sig_verify.rs`. `combo_accept` will follow the same
one-Ed25519-ix-per-signature pattern once implemented. Wrong order or omitted
Ed25519 instructions fail closed with `BadSignature`, never silently skip
verification.

**Transaction version (pinned 2026-07-12):** the full 3-ix tx exceeds the 1232-byte
legacy limit (~1453 B measured). The crank MUST submit it as a **v0 transaction** with a
per-market **Address Lookup Table** holding the market's static accounts + each trading
wallet's vault/ATAs. The operator (fee payer/signer) and the two per-order `OrderStatus`
PDAs stay static keys — OrderStatus PDAs are unique per order, and keeping them inline
avoids a table-extend + activation wait on every settle. Reference implementations:
`backend/internal/crank/{builder,lut}.go` (Go, production) and `tests/helpers.ts` (TS).
