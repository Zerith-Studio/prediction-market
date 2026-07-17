# PitchMarket — Manual E2E Test Plan (real TxLINE + Solana devnet)

Everything below runs against the **real** stack: live TxLINE feed, real Privy
wallet, real devnet transactions. **Nothing is mocked.** Work top to bottom —
later sections assume the earlier ones passed.

**How to read this:** each section has **Steps** (do these in the browser) and
**Pass ✅** (what must be true). `☐` = check it off. If a step needs a terminal,
it's marked `$`. Ports assume backend `:8080` and frontend `:3003` (your `dev`
script — adjust if `next dev` printed a different port).

---

## 0. Preflight — get into a clean, fully-real state

### 0.1 Backend in ON-CHAIN mode
Env (repo-root `.env`), all real:
```
DATABASE_URL=<fresh Neon db>            # see 0.5 — start clean
SOLANA_RPC_URL=https://solana-devnet.g.alchemy.com/v2/<key>
OPERATOR_KEYPAIR=~/.config/solana/id.json
GEMINI_API_KEY=<key>                    # one-liners (cosmetic)
FEED_PROVIDER=txodds
TXODDS_COMPETITION=72
ADMIN_PUBKEY=<your browser wallet pubkey>   # see 0.3 / 10.1
```
`$ cd backend && go build ./... && ./... ` — start the server from **latest
`main`** (this is what activates the resolution reconciler).

- **Pass ✅** ☐ Startup log shows `ON-CHAIN mode rpc=…alchemy… operator=… usdc_mint=…`
  (NOT `off-chain mirror mode`). ☐ Log shows `feed: TxLINE live competition=72`
  and `mmbot: running`. ☐ `admin panel enabled`.

### 0.2 Frontend
`frontend/.env` (gitignored): `NEXT_PUBLIC_API_URL=http://localhost:8080`,
`NEXT_PUBLIC_PRIVY_APP_ID=<app id>`. `$ cd frontend && next dev -p 3003`.

### 0.3 Privy dashboard (do this or sign-in silently fails)
- ☐ **Solana embedded wallets ENABLED** (this app uses Solana, not EVM).
- ☐ `http://localhost:3003` added as an **allowed origin**.
- Login method (email/social) enabled.

### 0.4 Operator wallet
- ☐ `$ solana balance <operator>` ≥ **1.5 SOL** on devnet (each market
  create / resolve / deposit / settle costs fees; airdrop or transfer if low).

### 0.5 Clean slate
Point `DATABASE_URL` at a **fresh Neon database** (new branch/db) so there are no
old markets, positions, or orders. On boot the backend re-discovers live TxLINE
fixtures and recreates markets from scratch.
- **Pass ✅** ☐ `$ curl -s localhost:8080/markets | jq '.markets|length'` starts
  at `0`, then grows within ~5 min as TxLINE fixtures in the coverage window
  (48h pre-kickoff → 4h post) get registered.

### 0.6 Smoke check
- **Pass ✅** ☐ `$ curl -sw '%{http_code}\n' localhost:8080/healthz -o /dev/null` → `200`.
  ☐ `/markets` returns real fixtures (real team names, not "Team A").
  ☐ Frontend loads at `:3003`, top bar reads **PITCHMARKET**, "Connect wallet".

> **Liquidity reality (read before trading):** the MM bot only quotes markets
> TxLINE actually prices — **`dnb_home` ("… draw no bet")** and **`ou_1h_075`
> ("First-half goal")**. Other 1X2 / BTTS / O2.5 markets exist but rest **empty**
> until odds arrive, so a Yes/No order there will just *rest* (won't fill). To
> test **fills** any time, use §5a (two wallets crossing each other).

---

## 1. Sign in — Privy embedded wallet
**Steps:** Click **Connect wallet** (top-right) → complete Privy login (email code
/ social).
- **Pass ✅** ☐ A Privy modal appears (NOT an instant "demo" connect). ☐ After
  login the top bar shows a truncated **Solana** address (base58, ~`8Uxj…k6sV`),
  no "demo" tag. ☐ Vault reads **$0.00**.
- **Fail signals:** address stays null after login → Solana wallets not enabled
  in Privy (0.3). Modal never opens → app id not inlined (rebuild frontend).

## 2. Fund the vault — real devnet deposit
**Steps:** Open any market → **Trade** panel → **Buy** → the error shows
**"Fund 1,000 demo USDC"** → click it → Privy signs the deposit message.
- **Pass ✅** ☐ Privy shows a signing prompt (the one deliberate signing moment).
  ☐ Vault updates to **$1,000.00**. ☐ The deposit is a **real devnet tx** — verify
  in §Appendix A (or backend log shows a deposit tx sig).
- **Fail:** if it silently credits with no tx, the server is in mirror mode (0.1).

## 3. Markets index (cards + live data)
**Steps:** Go to **Markets** (`/`).
- **Pass ✅** ☐ Matches are grouped by fixture (real teams). ☐ A live match shows
  a **LIVE · x–y** pulse; finished shows **FT**; upcoming shows kickoff time.
  ☐ Markets render as **cards** (title + category + right-side state). ☐ Binary
  cards on an **open** match show **Yes / No** buttons; a settled binary shows
  its result (YES/NO); a precision card shows **POOL** (open) / a number (settled)
  / **LOCKED** (kickoff-locked).

## 4. Market detail — live orderbook over WebSocket
**Steps:** Click a **`dnb_home`** or **`ou_1h_075`** card title (priced markets).
- **Pass ✅** ☐ Large YES price in ¢ + a price chart. ☐ Order book shows bot
  bids/asks (there IS resting depth on these two). ☐ Recent fills / one-liner
  ticker present. ☐ **Live update:** leave it open — as the bot re-quotes or a
  trade happens, the price/book update **without a refresh** (WS push). ☐ Network
  tab shows a `ws://…/ws` connection open.

## 5. Prediction (binary) trading
Use a **priced** market (`dnb_home` / `ou_1h_075`) so you fill against the bot.

**5.1 Buy YES**
- **Steps:** Trade panel → **YES** tab (default) → **Buy** → set size (e.g. 200)
  → **Sign & Buy YES** (Privy signs silently).
- **Pass ✅** ☐ Order accepted. ☐ If your price crosses the bot's ask, a **fill**
  appears in Recent Fills and the book/price update live. ☐ **Portfolio** (§8)
  shows a YES position. ☐ Within ~30s a **settle tx** attaches to the fill (crank
  → devnet); the fill flips to "Verified on Solana".

**5.2 Buy NO**
- **Steps:** **NO** tab → note the price auto-switches to `100 − YES` ¢ → **Buy**
  → **Sign & Buy NO**.
- **Pass ✅** ☐ Order submits as the **NO** outcome. ☐ Portfolio reflects a NO
  position (or a MINT fill vs a YES buyer). ☐ Button read "Sign & Buy NO".

**5.3 Sell / limit**
- **Steps:** With a YES position, **Sell** tab → set a limit price → **Sign & Sell
  YES**. Then place a **non-crossing** buy (price below best ask) to test resting.
- **Pass ✅** ☐ Sell reduces your position on fill. ☐ A non-crossing order **rests**
  on the book (visible in the ladder) rather than filling. ☐ Naked sell (no shares)
  is **rejected** (`Not enough … shares to sell`).

**5a. Two-wallet fill test (works on ANY market, any time)**
- **Steps:** Open a **second** browser profile / incognito → second Privy wallet →
  fund it. In wallet A place a resting SELL (e.g. YES @ 60, size 100) on an empty
  market; in wallet B place a BUY that crosses it.
- **Pass ✅** ☐ B's order fills against A's. ☐ Both portfolios update. ☐ Shares
  conserved (A sold exactly what B bought). ☐ A settle tx lands on devnet.

## 6. Precision pool
**Steps:** Open a **precision** market (`Total goals — precision` /
`Total passes — precision`), `/precision/[id]` → enter a **guess** + **stake** →
submit. (No wallet signature — precision entry is a soft-locked stake.)
- **Pass ✅** ☐ Entry accepted; **vault drops by the stake**. ☐ You appear on the
  **leaderboard** with your guess. ☐ The distribution/odds view renders. ☐
  **Kickoff-lock:** once the match is live, entry is **closed** (409 / disabled) —
  you cannot enter after kickoff. ☐ One entry per wallet (a second entry is
  rejected).

## 7. Combos (RFQ)
**Steps:** **Combos** (`/combos`) → pick **2–6 legs** across matches → set a stake
→ **Request quote** → wait for the market maker → **Accept**.
- **Pass ✅** ☐ Picking two legs from the **same mutex group** (e.g. `home_win` +
  `draw` on one match) **greys out** the conflicting one ("exclusive"). ☐ After
  Request quote, the **bot returns a signed quote** (payout + a live **countdown**)
  within a few seconds. ☐ **Accept** escrows the pot; the slip shows
  "✓ Combo escrowed — pays $X" and returns an `accept_tx`. ☐ Portfolio shows the
  combo. ☐ Letting the quote **expire** disables Accept and prompts a fresh quote.
- **Note:** combo escrow is off-chain by design (ADR 0004); resolution reads the
  same on-chain market outcomes.

## 8. Portfolio — positions, orders, PnL
**Steps:** **Portfolio** (`/portfolio`).
- **Pass ✅** ☐ **Positions** list your YES/NO holdings with avg cost and a live
  **unrealized PnL** (marked at best-bid). ☐ **Open orders** list resting orders
  with a **cancel** button → cancel releases the lock and the order leaves the book.
  ☐ **Exit** on a position signs a one-click SELL at best bid → on fill it reduces
  the position and moves PnL from unrealized → **realized**. ☐ Totals (unrealized /
  realized) tint green/red correctly.

## 9. Settlement & payout (on-chain resolution)
Trigger a resolution via the **admin panel** (§10.4/10.5) on a market you traded,
then:
- **Steps:** Open `/settlement/[id]` for that market (or click through from a
  settled card).
- **Pass ✅** ☐ Settlement page shows the **outcome** (YES/NO/VOID or the precision
  value). ☐ A **"Verified on Solana"** link opens the real `resolve_market` tx on
  the explorer. ☐ In **Portfolio**, a winning position's value is realized (vault /
  realized PnL reflects the 1:1 payout on winning shares; a losing side goes to 0).
- **Confirm during test:** whether winning binary shares auto-credit to the vault
  or need an explicit redeem — note the actual behavior here (redeem exists
  on-chain; UI surfacing is the thing to verify).

## 10. Admin panel (`/admin`)
Reach it by **URL** (not linked in the nav).

**10.1 Sign in (operator-wallet signature)**
- **Steps:** `/admin` → connect the wallet whose pubkey = `ADMIN_PUBKEY` →
  **Sign in as admin** (signs a one-time challenge).
- **Pass ✅** ☐ A **wrong** wallet is rejected ("not the admin wallet"). ☐ The
  admin wallet signs in and the console (Ops / Fixtures / Markets) loads.

**10.2 Fixtures & odds**
- **Pass ✅** ☐ Lists live TxLINE fixtures (competition 72). ☐ **odds** reveals
  implied ¢ per template for a fixture. ☐ Already-registered fixtures show
  "markets ✓".

**10.3 Create markets**
- **Steps:** On a fixture with no markets → **create markets**.
- **Pass ✅** ☐ All 9 template markets are created and appear in the Markets table
  (and on the public index). ☐ In on-chain mode the binaries are initialized on
  devnet (backend logs `market initialized … tx=…`).

**10.4 Resolve a single market**
- **Steps:** On a binary market → arm **YES** → **confirm yes**. On a precision
  market → enter a value → **settle** (or **void**).
- **Pass ✅** ☐ Binary → status **settled**, a **tx** appears (real
  `resolve_market` on devnet). ☐ Precision → status settled with the value. ☐ The
  card on the public index updates.

**10.5 Resolve fixture from score (the cascade)**
- **Steps:** **Resolve fixture from score** → enter the fixture id + final score
  (home/away, **HT score**, **total passes**, or abandoned) → **Resolve fixture**.
- **Pass ✅** ☐ Every binary resolves on-chain (tx per market), precision pools
  settle, combos sweep. ☐ The result panel lists each market with its outcome + tx.
  ☐ Note: leave **total passes** blank → the passes pool won't settle (known gap);
  fill it to settle that pool.

**10.6 Clear orders / close**
- **Pass ✅** ☐ **Clear orders** on a market cancels all resting orders (book
  empties; count returned). ☐ **Close** flips the market to `closed`.

**10.7 Ops dashboard**
- **Pass ✅** ☐ Shows **operator SOL** (warns if < 1). ☐ **TxLINE creds** valid +
  expiry. ☐ Market status tallies. ☐ **Stale matches** lists any match well past
  kickoff still unresolved (this is the reconciler's manual-review backstop).

## 11. Resolution durability (reconciler) — advanced
Proves a **missed full-time event self-heals**.
- **Steps:** Find a match stuck `live` well past kickoff (e.g. via Ops → stale
  matches). Ensure the backend is on latest `main`. Restart the backend.
- **Pass ✅** ☐ On startup the log shows `reconcile: …` lines. ☐ If TxLINE's
  snapshot reports finished → the match **resolves automatically** on boot. ☐ Else,
  after the score is stable ~40 min, it auto-resolves; otherwise it stays listed
  as **stale** in Ops for manual resolution (10.5). ☐ Re-running never
  double-settles or double-pays (idempotent).

---

## Appendix A — On-chain verification (nothing mocked)
- Program: `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs`
  (`explorer.solana.com/address/…?cluster=devnet`).
- For any tx sig: `explorer.solana.com/tx/<sig>?cluster=devnet` or
  `$ solana confirm <sig> -u devnet`.
- **You should be able to see real devnet txs for:** deposit, settle_match (per
  fill), resolve_market (per resolution), and market init. If an action produces
  **no** tx, the server is in mirror mode — fix 0.1.

## Appendix B — Known limitations / expected non-failures
- **Empty books** on non-priced markets (only `dnb_home` / `ou_1h_075` are
  bot-quoted) — orders rest, don't fill. Not a bug (use §5a to test fills).
- **`total_passes` not mapped** from the txodds feed → passes precision pool needs
  an admin-supplied value (10.5).
- **One-liner ticker** may go quiet on Gemini 5xx/DNS blips — cosmetic only.
- **Don't restart the backend mid-live-match** (feeds + crank stop with it);
  §11's restart is for a match that's already over.
- A settle that "times out" usually landed — `solana confirm <sig>`.

## Appendix C — Quick reference
| Thing | Value |
|---|---|
| Backend | `:8080` (REST + `/ws`) |
| Frontend | `:3003` (`/`, `/market/[id]`, `/precision/[id]`, `/combos`, `/portfolio`, `/settlement/[id]`, `/admin`) |
| Priced (fillable) markets | `dnb_home`, `ou_1h_075` |
| Health | `curl localhost:8080/healthz` → 200 |
| Markets | `curl localhost:8080/markets` |
