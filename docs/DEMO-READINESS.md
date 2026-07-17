# Demo readiness — adversarial test report

Written 2026-07-15, the night before the demo. Goal: **break the system now so
it doesn't break on camera.** Everything below was written as a test, run
against the real stack, and — where it failed — fixed. One real exploit was
found and closed.

## TL;DR

| Category | Result |
|---|---|
| Money conservation (no USDC created/destroyed by trading) | ✅ proven |
| Concurrency (no over-fill, no double-spend) | ✅ proven under 20–40-way races |
| Replay / idempotency | ✅ one signed order → one fill, even fired 8× at once |
| Revert → reconcile (a failed settle undoes cleanly) | ✅ proven |
| Every hostile HTTP input → clean 4xx, never 5xx | ✅ 28 probes |
| Whole-server concurrent HTTP load | ✅ 120 concurrent reqs, 0×5xx, WS alive |
| **Integer-overflow lock exploit** | 🐛 **found → fixed** |

## The bug we found (and killed)

**`BuyCost` uint64 overflow → near-zero lock for a colossal order.**
`price · size · MicroPerCent + fee` was computed with plain Go multiplication.
A crafted `size` (~3×10¹³) wraps the product past 2⁶⁴ back near zero, so the
off-chain ledger locked ~$0.25 for an order of 30-trillion shares. That order
was **accepted (HTTP 200)** and would sweep all resting liquidity paying almost
nothing, or rest as a poisoned bid. On-chain `checked_mul` would revert the
settle, but the off-chain mirror (and the UI) would already be corrupted — a
demo-killer.

**Fix (commit `a52e892`):** `models.MaxOrderSize = 1e12` (generous — no real
order is close; overflow-proof even at 99¢ + max fee), enforced in **two**
independent chokepoints: `matching.validate` (the resting gate, also protects
the bot) and `store.PlaceOrder` (self-defends before any cost math). Oversize
orders now reject with a clean 4xx. Regression test:
`TestAttackVectors/overflow_size`.

## What was tested, and where

### 1. Trading-core invariants — `internal/exchange/chaos_test.go`
Real Postgres (scratch DB per run), offchain/flaky submitter.

- **`TestConcurrentTakersNeverOverfill`** — one resting SELL of 100; 20 buyers
  concurrently demand 800. Exactly 100 fill; seller drains to 0; buyers hold
  exactly 100 total. No phantom shares, no double-spend.
- **`TestMoneyConservedAcrossManyFills`** — a buyer's
  `available + locked + Σ(qty·avg_cost·micro)` equals what they deposited, to
  the micro, after a dozen MINT fills. USDC is neither created nor destroyed.
- **`TestConcurrentReplayFillsOnce`** — the same signed order fired from 8
  goroutines: exactly 1 accept, buyer holds the single order's size (not 8×).
- **`TestRevertReconcilesMoneyAndBook`** — every settle reverts; the buyer's
  lock is restored, no shares minted, and the maker's order re-rests and fills
  for a later taker once settles work again.

### 2. Hostile HTTP inputs — `internal/api/api_attack_test.go`
28 probes through the real REST surface. Each must reject 4xx, never 5xx:

malformed JSON · bad base58/hex/sig · forged signature · tampered field ·
wrong-market-than-signed · insufficient funds · naked short · price 0/100 ·
size 0 · **overflow size** · replay · cancel-no-maker · cancel-nonexistent ·
cancel-another-wallet's-order · unknown/bad market · deposit zero/bad-wallet ·
precision double-entry · precision bad-wallet · combo one-leg ·
combo mutex-conflict · accept-nonexistent-quote. Final probe: **a good order
still fills after the whole assault** (the exchange isn't wedged).

### 3. Whole-server load — `internal/api/api_load_test.go`
`TestConcurrentHTTPLoad`: 40 buyers, each doing deposit + forged order + good
order concurrently (120 in-flight requests) against 1000 resting shares.
Asserts **zero 5xx**, share conservation (buyers filled exactly what the seller
sold), and the **WS hub kept delivering events** throughout (no frozen book).

### 4. Live on-chain path (proven separately, earlier today)
A real fill settled on devnet via the Go crank:
[`27VgrXKi…`](https://explorer.solana.com/tx/27VgrXKiLby34HiPorxTZYRBhK2R8zjn191sxK3MRtuADqJ3cBkHnaxV5txqHWFvpucq7ktMfAemXWydQj83M4qQ?cluster=devnet).
Deposit → trade → settle → exit → cancel → precision → combo, all against the
live TxLINE-fed server. See progress.md changelog.

## Fixes made this session (chronological)

1. **Mirror divergence** (`984c9d5`) — a settle landing after the confirm
   timeout was reverted locally, then the index synced Postgres to chain truth;
   the in-memory book drifted ahead and later fills failed `ApplyFill` while the
   engine kept them. Now a rejected mirror write unwinds the engine fill too;
   bot quotes carry a 10-min expiry so stale orders age out.
2. **CreateIdempotent ATAs** (`f559864`) — settle ATA creation now uses the
   idempotent instruction, so a transient RPC error on the existence check can
   never fail a settle.
3. **Order-size overflow** (`a52e892`) — see above.

## Residual risks & mitigations (read before the demo)

| Risk | Likelihood | Mitigation |
|---|---|---|
| **Public devnet RPC 429s** under load | medium | Now on a **dedicated Alchemy endpoint**. Index poll eased to 30s. If a settle "times out," it usually landed — `solana confirm <sig>`. |
| Operator wallet runs out of SOL mid-demo | medium | Keep ≥1 SOL; each on-chain action (market create/resolve, deposit, settle, ATA) costs fees. Top up beforehand. |
| A market with no TxLINE price → empty book | low | Only `dnb_home` and `ou_1h_075` are bot-quoted (TxLINE prices them). **Demo those two.** The other 1X2/BTTS markets exist but rest empty until odds arrive. |
| DB tests flake on Neon network blips | n/a for demo | Test-only. `dial error`/`no such host` = re-run; not a code bug. |
| WS reconnect storm if backend restarts | low | Client backs off (1→8s). Don't restart the backend during the demo. |
| Combo escrow is off-chain (on-chain `combo_accept` is stubbed) | by design | Sanctioned cut (ADR 0004 seam). Narrate combos as "escrowed by the exchange, resolved from the same on-chain outcomes." |
| **Mirror-faucet wallets can't settle on-chain** | test-only | The `/wallet/deposit` faucet credits Postgres only — no `init_vault`, so no on-chain vault. Orders from such wallets match but revert at settle with `maker_vault AccountNotInitialized` (money stays safe via reconcile). **The frontend always uses the real two-step deposit** (`init_vault` + deposit), so real demo trades are unaffected. Only my test scripts left polluted orders. |

## Live-match findings (2026-07-16, real England vs Argentina coverage)

Running the fixed binary against the real match surfaced two more issues the
adversarial suite couldn't — both **money-safe** (every failure reconciled, no
funds lost), both fixed or documented:

1. **Sticky LUT/ATA caches** (fixed, `6aaade6`) — a market whose lookup-table
   or vault-ATA setup half-failed would revert *every* settle forever, because
   the caches recorded success optimistically and never evicted. Now the LUT
   cache recreates an unreadable table, the ATA cache only records verified
   existence, and we wait for ATA visibility before settling (closing an
   RPC-load-balancer read-after-write gap that caused `AccountNotInitialized`).

2. **Stale mirror-funded orders in the book** (data hygiene) — `RestoreBooks`
   reloads all live orders on restart, including ones left by mirror-faucet test
   wallets that have no on-chain vault. Those match and revert at settle. **Not
   a demo risk** (the frontend uses real deposits), but for a clean book before
   the demo, clear the stale orders — scoped SQL:

   ```sql
   -- cancel only NON-bot resting orders (bot re-quotes fresh; keeps the ledger
   -- honest by NOT zeroing locks — those test wallets are throwaway).
   -- Replace <BOT_WALLET> with the "mmbot: running wallet=" value from the log.
   UPDATE orders SET status = 'cancelled'
   WHERE status = 'live' AND maker <> '<BOT_WALLET>';
   ```

   Or simplest for a fresh demo: point the server at a clean database.

## Admin panel — manual market control (`/admin`)

The auto full-time cascade only fires when the TxLINE score feed reports the
final whistle, which can lag hours (§residual risks). The `/admin` page is the
mitigation and the demo's safety net: it lets the operator drive the lifecycle
by hand — settle on cue rather than on the feed.

**Enable it.** Set `ADMIN_PUBKEY` to the base58 pubkey of the wallet you sign
with *in the browser* (your Privy or local demo wallet — **not** a server
keypair). In on-chain mode it defaults to the operator pubkey; blank + no
operator ⇒ `/admin` returns 503.

**Sign in.** Open `/admin` (not linked from the nav — reach it by URL), connect
the admin wallet, click *Sign in as admin*. The wallet signs a one-time
challenge (`pitchmarket-admin:<nonce>`); the server checks the ed25519 signature
against `ADMIN_PUBKEY` and issues an 8-hour session token (in-memory, held in
`localStorage`). No password, nothing on the wire but the signature.

**What it does.**
- **Fixtures & odds** — browse the live TxLINE fixtures (competition filter,
  default 72), peek at implied ¢ per template, and **create a fixture's markets**
  on demand (the same `RegisterFixture` the feed uses; on-chain when enabled).
- **Markets** — per-market **Resolve** (binary YES/NO/VOID; precision settle to a
  value or void), **Close**, and **Clear orders** (off-chain cancel of every
  resting order — the clean-slate button that used to need raw SQL, see the stale
  mirror-order note above).
- **Resolve fixture from score** — enter the final score once to fire the whole
  cascade (every binary resolves on-chain, precision settles, combos sweep) —
  how you demo "Verified on Solana" without waiting on the whistle.
- **Ops** — operator SOL (low-balance warning), TxLINE credential validity, and
  market status tallies.

Every resolve goes through the same on-chain `resolve_market` + store mirror as
the automatic path, so money safety is unchanged. Auth is demo-grade (session
tokens live in memory); production auth is out of scope. Covered by
`internal/api/admin_test.go` (auth round-trip + single-market resolve).

## Resolution durability — missed full-time events self-heal

The live path resolves a match when TxLINE's SSE feed delivers `full_time`. That
event is a **single ephemeral trigger** — if it's missed (service down during the
window, SSE reconnect gap, or TxLINE never emits `final_whistle`), the old design
left the match stuck `live` forever and its markets never settled (exactly what
happened to fixture `18241006`).

There's now a **reconciliation guarantee** behind it
(`cmd/server reconcileResolutions`), modeled on the existing chain-index
reconciler: on **startup** and every few minutes it walks the *unresolved,
past-kickoff* matches (`store.UnresolvedMatches` — a bounded, usually-empty set)
and pulls TxLINE's **score snapshot** (`txodds.FinalState`), which is
authoritative and queryable any time unlike the transient stream. Per the
**hybrid policy**:

- TxLINE snapshot reports **finished** → resolve immediately.
- No finished signal, but the **score has held steady** past a stability window
  (40 min) → auto-resolve (a live match doesn't go 40 min without an event).
- Otherwise → leave it, surfaced as a **stale match in `/admin/ops`** for the
  operator to resolve by hand.

`ResolveFixture` is **idempotent** (skips already-settled markets — verified by
`TestResolveFixtureIdempotent`, incl. no double payout), so the reconciler runs
safely alongside the live SSE path and across restarts, and can finish a
*partially* resolved match. This is the fast-path-plus-reconcile durability
pattern, not naive polling — the loop is idle when everything is healthy. The
admin panel remains the final human backstop. (Note: `total_passes` still isn't
mapped from the txodds feed, so the passes precision pool needs an admin value.)

## Run the suite yourself

```sh
cd backend
go test ./internal/exchange/ -run 'Test(Concurrent|Money|Revert)'   # invariants
go test ./internal/api/ -run 'TestAttackVectors'                    # hostile inputs
go test ./internal/api/ -run 'TestConcurrentHTTPLoad'               # server under load
go test -p 1 ./...                                                   # everything
```

DB-backed tests need `DATABASE_URL` (auto-read from the repo-root `.env`); each
creates and drops a scratch database. They are slow (~300ms/statement to Neon).
