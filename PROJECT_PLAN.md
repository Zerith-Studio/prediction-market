# PitchMarket — Build Plan (regenerated from verified spec)

**Superteam × TxODDS World Cup Hackathon — "Prediction Markets & Settlement."**
Prize 18,000 USDT. Internal deadline **2026-07-15** (submissions judged by 2026-07-29). Team = 2
engineers (Anchor-fluent, fullstack).

**Decisions are pinned elsewhere — this doc is *how we build it*, not *why*:**
- `docs/adr/` (0001–0007) — the pinned decisions + trust story. `docs/adr/README.md` = index.
- `docs/core-features-spec.md` (v2, Polymarket-verified) — feature parity, corrections M1/M2/M3.
- `docs/interface-contract.md` — the E1↔E2 boundary (freeze Day 0).
- `docs/glossary.md` · `docs/txodds-day1-email.md`.

---

## 1. What we're building (one paragraph)
A football prediction exchange that is **Polymarket's CTF + CLOB, faithfully re-built on Solana**,
plus two net-new formats. Users sign orders off-chain (silent); an operator crank settles each
match **on-chain, non-custodially** (it can't forge or over-fill). Three products: **binary CLOB
markets**, **RFQ combos/parlays** (the differentiator), and **precision pools** (numeric closeness).
**TxODDS/TxLINE** drives auto market creation, live pricing, resolution, and — the headline — is the
**signed on-chain root of settlement trust**. Delight: a live one-liner agent (Claude).

## 2. Architecture
```
Next.js (Vercel)  ──REST/WS──►  Go backend  ──crank txs──►  Solana devnet (Anchor program)
  Privy embedded wallet            ├ order API + WS hub          ├ Market/condition PDAs
  signs orders (ed25519)           ├ matching engine (per-mkt)   ├ 2 SPL mints / market
                                   ├ operator crank (fee payer)  ├ settle_match (NORMAL/MINT/MERGE)
                                   ├ RFQ + MM/crowd bot          ├ combo_accept / resolve_combo
                                   ├ precision pools             ├ resolve_market (oracle tiers)
                                   ├ feed adapter (TxLINE|replay)└ redeem / OrderStatus / vaults
                                   └ one-liner (Claude)
        Postgres = off-chain index: order book, soft-locks, RFQ, precision, one-liners, cache
        (Chain is source of truth for balances / positions / settlement.)
```
**Split of truth:** on-chain = money, fills, settlement, order fill-accounting. Postgres = the
resting order book, soft-locks, RFQ negotiation, precision pools, one-liners, and a read-cache/index
of chain state for fast UI.

## 3. Services (Go packages) & the Anchor program
**E1 — Anchor program (`programs/pitchmarket`):** `initialize_market`, `settle_match`,
`cancel_order`, `combo_accept`, `resolve_market`, `resolve_combo`, `redeem` (see interface-contract
§4). Owns: outcome mints, vaults/delegate, `OrderStatus`/`QuoteStatus`, oracle verification.

**E2 — Go (`api`, `matching`, `crank`, `rfq`, `mmbot`, `precision`, `feed`, `oneliner`, `index`):**
- `matching` — in-memory CLOB/market, price-time priority, produces NORMAL/MINT/MERGE matches.
- `crank` — builds one `settle_match` tx per match; operator is fee payer; handles reverts (§6 IC).
- `rfq` — combo lifecycle, compat mutex groups, quote intake/expiry; `combo_accept` via crank.
- `mmbot` — binary bid/ask around TxLINE fair price; RFQ quoting; precision **crowd-seeding**.
- `precision` — kickoff-lock, one-entry-per-wallet, rake, σ-normalized score, settlement.
- `feed` — `FeedProvider` interface: `txodds` (live) + `replay` (recorded, demo safety net).
- `oneliner` — 2-min cron → match context → Claude → 6 lines/market.
- `index` — subscribes to program events, mirrors chain state to Postgres for UI reads.

## 4. Data model (Postgres — the off-chain index; chain is authoritative for money)
```
users(id, privy_id, wallet, ...)
matches(id, txodds_fixture_id, home, away, kickoff_at, status, live_state jsonb)
markets(id/market_id, match_id, template_key, type[binary|precision], title, rule,
        status[draft|open|closed|resolving|settled|void], outcome jsonb, chain_condition, chain_tx)
orders(order_hash pk, market_id, maker, outcome, side, price, size, remaining, fee_bps, expiry,
       salt, sig, status[live|matched|cancelled], created_seq)          -- book + soft-locks
fills(id, market_id, taker_hash, maker_hash, price, size, match_type, settle_tx, ts)
positions_cache(user, market_id, yes, no, avg_cost, ...)                -- mirror of chain
combo_quotes(quote_hash pk, maker, legs jsonb, stake, payout, expiry, salt, status)
combo_escrows(quote_hash, taker, status[accepted|won|lost|void], accept_tx, resolve_tx)
precision_entries(id, market_id, user, guess, stake, score, payout, ts)  -- one per (user,market)
oneliners(market_id, lines jsonb, generated_at)
```
Money in **micro-USDC** on-chain; `price` integer 1..99. Order fill-accounting is authoritative
**on-chain** (`OrderStatus`); `orders.remaining` is a mirror.

## 5. Key formulas (from spec/ADRs — don't re-derive, just implement)
- **Trading fee (M1):** `fee = baseFeeRate × min(p, 1−p) × size`, output asset; `baseFeeRate=0` demo.
- **Precision score (ADR 0006, M-verified):** `score = 1/(1 + |guess−actual|/s)^k`, `s`=per-template
  scale, `k=2`; `payout_i = Pool × (stake_i·score_i)/Σ`.
- **Combo quote (bot):** `fair_odds = 1/(Πp_i · corr_adj)`; `quoted = fair·(1−margin)`; exposure-capped.

## 6. API & pages
**REST:** `POST /orders`, `DELETE /orders/:hash`, `GET /markets`, `GET /markets/:id`,
`GET /markets/:id/book`, `POST /combos`, `GET /combos/:id`, `POST /combos/:id/accept`,
`POST /markets/:id/precision`, `GET /markets/:id/precision/leaderboard`, `GET /portfolio`,
`GET /balance`, `POST /wallet/deposit`, `GET /markets/:id/settlement`, `GET /markets/:id/oneliners`.
**WS `/ws`:** `book_update`, `fill`, `order_update`, `combo_quote`, `match_state`, `oneliner`.
**Pages (Next.js):** `/` (matches), `/match/[id]`, `/market/[id]` (book + trade), `/precision/[id]`,
`/combo` (builder + RFQ), `/portfolio` (3 sections), `/settlement/[id]` (outcome + Solana verify),
`/profile` (NFT — add-on).

## 7. Two-track schedule (ADR 0007) — Jul 4 → Jul 15
**Day 0 (Jul 4–5, shared):** scaffold repo + Postgres + Privy; freeze `interface-contract.md`
(incl. the two §6 decisions: **Vault PDA recommended**, revert→reconcile path); `feed` interface +
`replay`; **send the TxODDS signed-data email today.**

**E1 (Anchor):** Jul 5–7 vault + mints + `mint_set`/`merge_set` + `settle_match` + tier-a resolver
(binary settles on devnet via test harness) · Jul 8–9 **on-chain ed25519 order-sig verify + `OrderStatus`
fill-accounting** (longest pole) · Jul 10–11 `combo_accept` + `resolve_combo` (reads leg conditions) +
`redeem` + VOID · Jul 12–13 oracle tier **d** (TxODDS sig) or **b** (challenge window) · Jul 14–15 hardening.

**E2 (Go + Next):** Jul 5–7 matching (NORMAL/MINT/MERGE) + soft-lock + crank skeleton + order API + WS ·
Jul 8–9 binary market frontend end-to-end vs devnet → **demoable floor ~Jul 9–10** · Jul 10–11 RFQ + MM
bot + combo builder UI · Jul 12 precision + pool UI + crowd-seed bot · Jul 13 portfolio + settlement/verify
page · Jul 14 one-liner + polish · Jul 15 dress rehearsal + record.

**Go/No-Go — Jul 11 EOD:** if E1 binary settlement (through fill-accounting) isn't end-to-end on
devnet, cut to **binary-on-chain-trustless only**; combos → off-chain or cut; precision off-chain;
drop one-liner/NFT. Tier-a floor stays demoable throughout.

**Cut order:** NFT → one-liner → combos-on-chain → precision polish → multi-match. **Never cut:** one
match, one binary market, fully on-chain-trustless (signed order → crank → settle → resolve → redeem).

## 8. Demo (≈3 min)
Login + fund (Privy → Vault deposit, one popup) → binary trade (silent signed orders, book seeded by
bot, one-liner ticker) → precision (pick a number, live distribution, settle → leaderboard) → **combo
RFQ** (build legs, one greys out on conflict, bot quote counts down, accept, legs tick green) →
settlement page → **"Verified on Solana ↗"** (devnet tx: signed TxODDS outcome) → portfolio. Narrate:
*"Polymarket's exchange, faithfully on Solana; UMA replaced by TxODDS as the signed on-chain root."*

## 9. What's mocked vs real
**Real:** matching, non-custodial on-chain settlement (signed order → crank → settle → resolve →
redeem), combo escrow reading on-chain legs, payout math, at least one signed TxODDS resolution tx.
**Mocked/safety-net:** live data via `replay` if TxLINE access lags; USDC = devnet; NFT = off-chain
badges (optional cNFT); scale = 1–2 fixtures.

## 10. Top risks
Greenfield **ed25519 verify + fill-accounting** (critical path — the Jul 11 gate guards it) · TxODDS
reply gates oracle tier (floor de-risks) · E2 bucket overloaded (cuttable tail by design) · live-match
timing (replay compresses a match to minutes for rehearsal).

---
**Next actions:** (1) freeze interface-contract §6 choices; (2) send TxODDS email; (3) E1 scaffolds the
Anchor program, E2 scaffolds matching+crank — both against the frozen contract.
