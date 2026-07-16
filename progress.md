# PitchMarket — Progress Log

**This file is the single source of truth for "where are we right now."**
`PROJECT_PLAN.md` says what we're building and why. `docs/interface-contract.md` is the
E1↔E2 boundary. **This file says what actually works today.**

> **Rule for both engineers:** if you change code, change this file in the same commit.
> Update the component table, tick the checklist, and add a Changelog row. A claim in this
> file means *"I ran it and saw it work"* — not *"I wrote it and it should work."*
> If you didn't verify it, mark it 🟡 and say what's unverified.

Legend: ✅ done & verified · 🟡 written but unverified · 🔴 not started / blocked

---

## 1. Status at a glance — 2026-07-12 (Day 8 of 11)

| | |
|---|---|
| Deadline | **2026-07-15** (internal) · judged by 2026-07-29 |
| Days left | **3** |
| E1 program | ✅ deployed on devnet; full lifecycle proven |
| E2 backend | ✅ **ALL REAL DATA**: TxLINE live feed (free-tier on-chain subscription — no email needed), markets auto-created on devnet, UI trades settled by the crank on devnet, real two-step deposits |
| Frontend | ✅ live-only (fixtures deleted): markets index, market page, **combos builder**, **precision pools**, portfolio with exit/cancel/realized+unrealized PnL (BBP mark) |
| **Remaining** | deploy/hosting, demo recording, human browser click-through |

**Honest summary.** Both halves of the trustless floor now work — separately. E1: the
§4 toolchain blocker is fixed, the program compiles to BPF, and the full lifecycle
(`initialize_market → deposit → settle_match (NORMAL/MINT/MERGE) → cancel fail-closed →
resolve_market → redeem`) runs green on `solana-test-validator` with balance assertions;
`sig_verify` executed for real. E2: the whole backend (matching, Postgres mirror,
crank builder, API/WS, RFQ, precision, bot, lifecycle) passes an HTTP end-to-end suite
against real Postgres. **Not yet done: the two halves have never met on devnet.** The
crank still settles in off-chain mirror mode, and E1's tx-size finding means the Go
crank needs a v0 + Address Lookup Table rework before it can submit for real. That
join — Go crank → deployed program on devnet — is the last hard step of the floor.

---

## 2. E1 — Anchor program (`programs/pitchmarket`)

Builds to BPF (`cargo build-sbf`, see §4). ✅ marks below = **exercised on a local
validator** via `tests/` (`npm test`), 8/8 passing. Not yet run on devnet.

| Instruction | State | Notes |
|---|---|---|
| `initialize_market` | ✅ | Market PDA + 2 outcome mints + pool. localnet |
| `init_vault` / `deposit` | ✅ | Vault PDA custody; USDC moved into vault ATA. localnet |
| `settle_match` NORMAL | ✅ | peer-to-peer USDC↔shares swap. localnet |
| `settle_match` MINT | ✅ | opposite-outcome buys mint a complete set into the pool. localnet |
| `settle_match` MERGE | ✅ | opposite-outcome sells burn a complete set, release pooled collateral. localnet |
| `cancel_order` | ✅ | maker cancels; a later settle of that order fails closed (`OrderClosed`). localnet |
| `resolve_market` | ✅ | **tier-a only** (operator-signed); localnet. Tiers b/d not started |
| `redeem` | ✅ | burns winning shares, pays 1:1 from pool. localnet |
| `sig_verify::verify_order_signature` | ✅ | ed25519 sysvar introspection **executed for real** in settle_match. TS borsh == `sig_verify.rs::borsh_order` proven at runtime; Go borsh pinned by golden vectors (§3) |
| `combo_accept` | 🔴 | typed stub |
| `resolve_combo` | 🔴 | typed stub |
| VOID path | 🔴 | |
| Oracle tier b (challenge) / d (TxODDS sig) | 🔴 | gated on TxODDS reply |

**Two program changes were needed to build & run** (PR #3):
- `SettleMatch` accounts are now `Box`ed — the context otherwise overflowed the 4KB BPF
  stack frame by 64 bytes (only surfaces at BPF build, not `cargo check`). ABI-unchanged:
  account order, args, and semantics are identical, so the Go crank needs no change here.
- `Cargo.toml` gained the `idl-build` feature (was missing; blocked IDL generation).

**⚠️ Cross-track finding:** the settle_match tx (2 ed25519 precompiles + the
`settle_match` ix) is **1453 bytes > the 1232 legacy limit**. It only fits as a **v0 tx
with an Address Lookup Table** — `tests/lifecycle.ts` shows how. **The Go crank
(`crank.TxBuilder`/`RPCSubmitter`) currently builds a legacy tx and must be reworked
before devnet settlement** — tracked in §5/§7.

**Program ID** `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs` — pinned in `declare_id!`
and `Anchor.toml`.

⚠️ **The keypair at `target/deploy/pitchmarket-keypair.json` is gitignored and exists on
one machine only.** Both engineers can *build* this program ID, but only whoever holds
that file can *deploy* to it. **Decide before deploy day:** `git add -f` it (fine for a
devnet hackathon) or share out of band. If it's lost, the program ID changes everywhere.

---

## 3. E2 — Go backend (`backend/`) — completed 2026-07-11

Verified with `go build ./... && go vet ./... && go test -p 1 -count=1 ./...` against a
real Postgres (Neon; each test run creates and drops a scratch database).

| Package | State | Verified how |
|---|---|---|
| `matching` | ✅ | Unified ladder: NORMAL + **MINT** (two BUYs ≥100) + **MERGE** (two SELLs ≤100), price-time priority across both populations, cancel, expiry, replay-reject, revert `Unfill`, book snapshots. 15 unit tests. |
| `models` | ✅ | borsh + sha256 hashing, ed25519 sign/verify, fee/cost formulas, base58/hex wire helpers. **Golden vectors pinned Go↔Rust** (`hash_conformance_test.go` ↔ `sig_verify.rs` tests) — with E1's runtime TS↔Rust proof, all three encoders are now cross-checked. |
| `store` | ✅ | Full Postgres layer: orders + soft-locks (BUY locks USDC, SELL locks tokens — no naked shorts), fills mirroring **lib.rs money movement exactly** (NORMAL at fill price, MINT/MERGE at own limits), RevertFill, balances, positions, combos, precision, oneliners. Integration-tested against Neon. |
| `exchange` | ✅ | The trading core: sig verify → soft-lock → match → mirror → crank → WS. Both API and bot submit through it. Revert→reconcile (§6.2 IC) proven end-to-end. |
| `crank` | 🟡→rework | Builds the §6.5 3-instruction tx byte-verified against sig_verify.rs checks; RPC submitter written. **BUT: emits a LEGACY tx — E1's §2 finding (1453 B > 1232) means it must be reworked to a v0 tx + Address Lookup Table before devnet.** Off-chain mirror mode is the default until then. |
| `ws` | ✅ | Hub with the six pinned events; slow-client drop; tested with live WS clients. |
| `api` | ✅ | Full REST surface + `/ws`. **End-to-end suite over real HTTP**: deposit → signed orders → MINT fill → crank capture → WS events → Postgres rows → portfolio → resolve → settlement endpoint. Bad sig → 401, replay → 409, double-accept → 409, post-kickoff precision → 410. |
| `rfq` | ✅ | Combo lifecycle with **mutex groups**, sig + expiry + leg-match checks, single-use accept, resolve sweep reading the same market outcomes binary settlement writes (ADR 0004 seam). On-chain `combo_accept` behind an interface (Noop until E1 lands it). |
| `precision` | ✅ | Kickoff-lock, one-entry-per-wallet, σ-normalized score k=2, pool payout ±3 micro dust, rake, VOID refunds. ADR 0006 end-to-end. |
| `mmbot` | ✅ | Two-sided **MINT liquidity** (both quotes are BUYs — bot needs only USDC), re-quote on fair ticks, RFQ quoting per the §5 formula, exposure cap, crowd-seeding distinct persona wallets. |
| `lifecycle` | ✅ | Fixture → 7 template markets → replay feed → full-time resolves all templates correctly, settles precision, sweeps combos; balances verified to the micro. Abandoned match → VOID + refunds. |
| `feed` | ✅ | `replay` drives the lifecycle test; `txodds` SSE skeleton tested against a fake server — real endpoint shapes pending TxODDS reply. |
| `oneliner` | ✅ | Claude Messages API client behind a Generator seam; 2-min ticker; tested with fake. Real API 🟡 (needs key at runtime). |
| `index` | ✅ | OrderStatus mirror processor (chain wins) tested with fake source; RPC poller written 🟡 (needs deployed program). |
| `cmd/server` | ✅ | Full wiring, env config (+ .env), graceful shutdown, `DEMO_FIXTURE` demo mode, CORS for browser clients. See `.env.example`. |
| `frontend/` | ✅ | Next.js app (E1's build) + motion-polish pass + **wired to the live exchange**: REST mapping (unified YES ladder from the outcome-indexed book), `/ws` stream (book/fill/oneliner/match_state), Privy embedded wallet behind `NEXT_PUBLIC_PRIVY_APP_ID` with a localStorage ed25519 demo wallet fallback, real order signing (borsh golden vector pinned as a 4th encoder, gated in `prebuild`). Browser-flow e2e green vs the real backend (fund → sign → 401/409 semantics → ladder → portfolio). Fixture mode still works with zero infra. |

**Test-suite caveats:** DB-backed tests are slow (~300ms RTT to Neon per statement) and
**network-flaky**: parallel packages contend on the shared endpoint, and even serialized
runs occasionally hit Neon DNS/TLS timeouts mid-suite — a failed package showing
`dial error`/`no such host` is the network, not the code; re-run it. Use
`go test -p 1 ./...`. Knowns: pgx simple-protocol needs explicit `::bigint` casts and
JSONB params as strings; `uint64` salts scan via `int64`.

---

## 4. ✅ RESOLVED — program now builds to BPF (fixed 2026-07-12, PR #3)

The `edition2024` failure was caused entirely by an **old platform-tools** (v1.43 →
rustc/cargo 1.79), which can't parse deps that Anchor 0.31.1 pulls. The fix is a
**modern Agave install** (platform-tools **v1.54** / rustc 1.89).

**How it was fixed (reproducible on a fresh machine):**
1. Install Rust (`rustup`), Agave CLI 4.1.1 (`release.anza.xyz/stable/install` →
   platform-tools **v1.54**), and Anchor via avm.
2. **Build with `cargo build-sbf` from the program dir, NOT `anchor build`.** The crux:
   `anchor build` (and `anchor idl build`) runs a toolchain override that **re-installs
   Solana 2.1.0 and repoints `active_release` back to the old v1.43 tools** — re-breaking
   the build (this is the "inconsistent state" the 07-10 note hit). After any `anchor`
   invocation, repoint:
   ```sh
   cd ~/.local/share/solana/install
   ln -sfn "$PWD/releases/stable-<hash>/solana-release" active_release && hash -r
   cargo-build-sbf --version   # must read platform-tools v1.54 / rustc 1.89
   ```
3. `cd programs/pitchmarket && cargo build-sbf` → `target/deploy/pitchmarket.so` (419 KB).

**IDL:** `anchor idl build` chokes on the two `ostatus` PDAs whose seed is a function
call on an instruction arg (`sig_verify::order_hash(&taker)`). Workaround: temporarily
swap those seeds for a plain arg field to emit the IDL, then restore. The runtime `.so`
keeps the real hash-based seeds.

**Verify on a second machine** — fixed on one clean box; E2 should reproduce.

---

## 5. Definition of done for the floor (one match, one binary market, fully trustless)

- [x] program compiles to BPF — `cargo build-sbf` (§4); 419 KB `.so`
- [x] full lifecycle green **on localnet**: signed order → settle_match (all 3 paths) →
      cancel fail-closed → resolve_market → redeem, balances asserted (`npm test` 8/8)
- [x] borsh conformance: TS↔Rust proven at runtime; Go↔Rust pinned by golden vectors
- [x] crank builds the §6.5 3-instruction tx — TS reference proven on localnet; Go
      builder byte-verified in unit tests
- [x] **Go crank reworked to v0 tx + Address Lookup Table** — per-market cached LUT
      (`crank/lut.go`); size proven in tests: legacy 1421 B ❌ vs v0 1116 B ✅ (limit 1232)
- [x] program deploys to **devnet** at the pinned ID — deployed 2026-07-15, tx
      `5Ayf6cLmSpqFue5odVvTVSBQSPMyJjyV6ndhp9FPu6F46CYDSkJucuDyPTpKMQvbpfv4XzC33v4bnfnaj4xXgVqa`
- [x] one signed order → matched → **Go crank** settles on devnet — ed25519 verify +
      fill-accounting + MINT executed, tx
      `3zNVPQJqLZhAuRpEmCzGxVfA9aqQe3mm3qT1yFzcN34rrNqM1Eu2oyuagxvdcT51xTjW86ggzjNGhrbvYoKzvdXS`
- [x] `resolve_market` → `redeem` on devnet → user's USDC moved 1:1 — txs
      `5oNcWKQBin6atteQcvAAtEkdivE5q9hXKmYXWeNiKzrXrS7X2VJN2SvSe7pxQ8oCMvjrSjBMr2T9i1uWtVJfXiK8` /
      `4qKCYL4G1VzsPighcWLQ6wgEfYBggHnFCkpHfomXBkWdCfVzrEHv4Ju3dLAwtKHNx62WEyV7Tvi2VqxeRSMWkMku`

**THE FLOOR IS DONE.** `go run ./cmd/devnet-e2e` reproduces it end-to-end (all balance
assertions green: 60/40 vault debits, complete set minted, pool 100% collateralized,
1:1 redemption). Devnet RPC note: public endpoint rate-limits aggressive status polls —
the harness and crank poll gently (1.5–2.5s) with long windows; a tx that "times out"
usually landed (check `solana confirm`).

---

## 6. Open decisions

| # | Decision | Owner | Status |
|---|---|---|---|
| 1 | ~~Commit `pitchmarket-keypair.json` or share out of band?~~ | both | **CLOSED 2026-07-12: committed** (devnet-only key; `git add -f`, on `feat/devnet-settlement`) |
| 2 | Oracle tier for demo: a (operator) vs d (TxODDS signed) | E1 | open, gated on TxODDS reply |
| 3 | TxODDS signed-data email sent? | — | **still unknown — confirm** |
| 4 | ~~Postgres vs in-memory~~ | E2 | CLOSED 2026-07-11: Postgres (Neon), wired + tested |
| 5 | Combos on-chain (`combo_accept`) vs off-chain for demo | E1 | E2 ships either way (interface seam); default off-chain per cut plan |

---

## 7. Next actions

**Full handoff (runbooks, invariants, punch list with code seams): `docs/HANDOFF.md`.**

**E2:** **frontend** — the last unstarted scope, with the deadline TODAY. The backend
serves everything it needs (HANDOFF §5.1 is the API contract); wire
`SOLANA_RPC_URL`/`OPERATOR_KEYPAIR` env to run the server in on-chain mode against the
deployed program. Then HANDOFF §5.2 (on-chain market lifecycle wiring).

**E1:** `combo_accept` / `resolve_combo` → oracle tier d if TxODDS replies (cut-safe:
combos run off-chain behind the interface seam).

**Both:** confirm the TxODDS email (decision #3); record the demo.

---

## 8. Housekeeping / paper cuts

- Neon DATABASE_URL lives in the gitignored `.env` (`.env.example` documents the shape).
  Rotate the credential after the hackathon (it was shared in chat).
- **Toolchain trap:** any `anchor` CLI invocation may silently repoint `active_release`
  to old platform-tools — see §4 step 2 before debugging "mystery" build failures.
- `docs/interface-contract.md` §6.5-above-§6 ordering still unfixed; §6.5 should also
  gain the v0+ALT requirement once the Go crank lands it.
- DB tests: `go test -p 1` (see §3 caveats).

---

## 9. Changelog

Newest first. One row per meaningful change. **Append here in the same commit as the code.**

| Date | Who | What changed | Verified how |
|---|---|---|---|
| 2026-07-16 | Prasad | **Mobile Task 9: deposit flow** — real DepositButton (two-step cosigned deposit via depositInit → wallet.signMessage(message_b64 bytes) → depositComplete, mirror-faucet fallback on 409/null, 1,000 demo USDC) replacing the Task 8 stub. | tsc --noEmit clean · npm test 6/6 · expo export web ok · on-device deposit vs live backend pending |
| 2026-07-16 | Prasad | **Mobile Task 8: TradeSheet — borsh signing + order placement.** Added `mobile/src/lib/errors.ts` (`placeErrorMessage`, maps `ApiError` status → pinned copy: 0/401/402/409/410) and replaced the Task 7 stub body of `mobile/src/components/TradeSheet.tsx` verbatim from the brief — buy/sell toggle, price/size inputs, order-signing block byte-identical to web `TradePanel.place()` (outcome fixed to 1/YES ladder, `fee_bps: 0`, `expiry: 0` GTC, `salt: Number(randomSalt())`, hex sig via `toHex`), vault-balance/insufficient-funds guard, submit states (idle/signing/placed) with "Filled"/"Resting on book" label from `res.fills.length`. Props interface unchanged from the Task 7 stub. Added the brief's stub `mobile/src/components/DepositButton.tsx` (`return null`) so this compiles ahead of Task 9. No deviations from the brief's code. | `npx tsc --noEmit` clean · `npm test` → 6/6 pass (no new tests, all pre-existing) · `npx expo export --platform web` succeeded (7 static routes) · on-device trading against the live backend (connect → deposit → resting bid → crossing fill → 409 replay) **not run** — pending device access |
| 2026-07-16 | Prasad | **Mobile Task 7: market screen — hero, ladder, one-liners, fills.** Added `mobile/src/app/market/[id].tsx`, `mobile/src/components/MatchHeader.tsx`, `mobile/src/components/Ladder.tsx` verbatim from the brief (paths adjusted for the `src/` layout; `@/*` alias unchanged). Screen consumes `useLiveMarket` (Task 5) + `usePitchWallet` (Task 4): loading/error states, match header with live score/minute, big YES price with delta, rotating one-liner, top-5-bid/ask ladder with depth bars, last-8-fills list, and a bottom "Trade" button (disabled when market isn't open) that opens a `TradeSheet`. Added the brief's stub `mobile/src/components/TradeSheet.tsx` — exact props interface (`open`, `onClose`, `marketId`, `yesPrice`, `marketStatus`, `balanceMicro`, `onPlaced`), body `return null` — so the screen compiles until Task 8 replaces it. No deviations from the brief's code. | `npx tsc --noEmit` clean · `npm test` → 6/6 pass (no new tests, all pre-existing) · `npx expo export --platform web` succeeded, now emitting 7 static routes incl. new `/market/[id]` (18KB) · on-device tap-through, live WS book updates, one-liner rotation, and AppState foreground-refresh **not run** (no device available) — pending |
| 2026-07-16 | Prasad | **Mobile Task 6: tabs shell + live Markets index.** Added `mobile/src/app/(tabs)/_layout.tsx` (expo-router `Tabs`, Markets/Portfolio with `@expo/vector-icons` `Ionicons`), `mobile/src/app/(tabs)/index.tsx` (verbatim from the brief: polls `api.listMarkets("open")` + `api.listMatches()` every 20s, fetches `api.getBook` per binary market, computes YES mid, pull-to-refresh, taps navigate to `/market/[market_id]`), `mobile/src/app/(tabs)/portfolio.tsx` stub. Deleted the Task 1 placeholder `mobile/src/app/index.tsx`. `@expo/vector-icons` was **not** actually present in `node_modules` despite being an Expo-template staple — brief assumption was wrong for this scaffold; ran `npx expo install @expo/vector-icons` to add it as a real dependency (now in `package.json`). `tsconfig.json`'s `@/*` → `./src/*` alias already existed from Task 1 (no change needed). Added `mobile/.env` (`EXPO_PUBLIC_API_URL=http://localhost:8080`, matching the Go server's default `:8080` from `backend/cmd/server/main.go`) and committed `mobile/.env.example` with the same line plus a LAN-IP note for physical devices; tightened `mobile/.gitignore` (`.env*.local` alone didn't cover plain `.env`) to `.env` + `!.env.example`. | `npx tsc --noEmit` clean · `npm test` → 6/6 pass (no new tests) · `npx expo export --platform web` succeeded (6 static routes incl. `/(tabs)` and `/(tabs)/portfolio`) · `git check-ignore` confirms `.env` ignored / `.env.example` tracked · on-device markets list + live-backend fetch + pull-to-refresh + card-tap navigation **not run** — pending Task 7 |
| 2026-07-16 | Prasad | **Mobile Task 5: ported `useLiveMarket` with AppState foreground refetch.** Added `mobile/src/lib/useLiveMarket.ts` ported from `frontend/lib/useLiveMarket.ts` byte-for-byte except: dropped `"use client"`; added a `refreshKey` state + `AppState.addEventListener("change", ...)` listener that bumps `refreshKey` on transition to `active` (mobile OSes freeze JS/kill sockets in background, so the snapshot can be stale on return), included in both the initial-load effect's deps (`[marketId, wallet, refreshKey]`) and the WS effect's deps (`[marketId, state.loading, state.errorStatus, refreshKey]`). WS event handling (`book_update`/`fill`/`oneliner`/`match_state`), reconnect backoff, and one-liner rotation left untouched — RN's global `WebSocket` matches the web API surface used. No new jest test per the brief; behavioral verification (live WS on-device) deferred to Task 7. | `npx tsc --noEmit` clean · `npm test` → 6/6 pass (no new tests, all pre-existing) — on-device WS/AppState behavior **not yet run** |
| 2026-07-16 | Prasad | **Mobile Task 4: `PitchWallet` context — SecureStore demo wallet.** Added `mobile/src/lib/wallet.tsx` verbatim from the brief: `keypairFromSeed` (ed25519 via tweetnacl), `PitchWalletProvider`/`usePitchWallet` React context, demo backend persists a 32-byte seed in `expo-secure-store` (hex via existing `toHex`/`fromHex`), `connect`/`disconnect` async (unlike web, since SecureStore is async), `signMessage` returns `nacl.sign.detached`. Wrapped `mobile/src/app/_layout.tsx`'s `<Stack>` in `<PitchWalletProvider>`, imported via the `@/lib/wallet` alias; `import "react-native-get-random-values"` stayed the first line. TDD: wrote `mobile/src/lib/__tests__/wallet.test.ts` (brief's keypair sign/verify + bs58 round-trip test) first, confirmed it failed on missing `keypairFromSeed` export, then implemented. | `npm test` → 6/6 pass (1 new wallet test + 5 existing) · `npx tsc --noEmit` clean |
| 2026-07-16 | Prasad | **Mobile Task 3: ported `api.ts` (core endpoints only)** to `mobile/src/lib/api.ts` from `frontend/lib/api.ts`, byte-for-byte except: `EXPO_PUBLIC_API_URL` replaces `NEXT_PUBLIC_API_URL` (incl. both error-message strings); dropped `{ cache: "no-store" }` from `get()` (Next-ism, RN fetch doesn't cache); deleted out-of-scope `getSettlement`, `enterPrecision`, `leaderboard`, `createRFQ`, `getRFQ`, `acceptQuote` and the `PrecisionEntry`/`RFQQuote`/`RFQ` interfaces, removed `Settlement` from the type import. `mapMarket`, `mapMatch`, `mapBook` (YES-ladder unification), `mapFill`, `getPortfolio`'s title join/filters, `postOrder`/`cancelOrder`/the three deposit calls, `PROGRAM_ID`/`DEPLOY_TX`/`explorerTx`/`explorerAddr` untouched. TDD: wrote `mobile/src/lib/__tests__/api.test.ts` (brief's 2 `mapBook` tests) first, confirmed it failed on missing module, then ported. | `npm test` → 5/5 pass (2 new `mapBook` tests + 3 existing borsh tests) · `npx tsc --noEmit` clean |
| 2026-07-16 | Prasad | **Mobile Task 2: pure libs copied from web, borsh golden vector gated.** Copied `frontend/lib/{borsh,types,format}.ts` verbatim to `mobile/src/lib/` (alias `@/*` → `./src/*`, so `mobile/lib/` from the brief becomes `mobile/src/lib/`) and `frontend/scripts/check-borsh.mjs` verbatim to `mobile/scripts/`; wrote `mobile/src/lib/base64.ts` (`b64ToBytes`, Hermes-safe, no `atob`) and `mobile/src/lib/__tests__/borsh.test.ts` (3 tests: golden vector, salt top-bit, base64 decode). Added `tweetnacl`, `bs58`, `react-native-get-random-values`, `expo-secure-store` deps; `react-native-get-random-values` imported as the first line of `mobile/src/app/_layout.tsx` (before `global.css`) so `crypto.getRandomValues` exists for `randomSalt`/tweetnacl on Hermes. `npx expo install jest-expo jest @types/jest -- --save-dev` worked as-is (landed in `dependencies`, not `devDependencies` — expo install ignores the `--save-dev` flag; left as-is, doesn't affect test/build). Added `test`/`check-borsh` npm scripts + `jest: {preset: "jest-expo"}` to `mobile/package.json`; no transformIgnorePatterns tweak was needed. One deviation from the brief: `npx tsc --noEmit` failed with `Cannot find name 'test'/'expect'` even though `@types/jest` was installed (expo's base tsconfig sets no `types` array, and ambient jest globals weren't picked up under `moduleResolution: bundler`) — fixed by adding `"types": ["jest"]` to `mobile/tsconfig.json` compilerOptions; the three copied files and the check-borsh script were not touched. | `npm test` → 3/3 pass · `npm run check-borsh` → `borsh golden vector ok (94 bytes)` · `npx tsc --noEmit` clean · re-diffed all 4 copied files against `frontend/` post-change to confirm still byte-identical |
| 2026-07-16 | Prasad | **Mobile Task 1: scaffolded Expo app** at `mobile/` (`create-expo-app@4.0.0`, SDK 57, expo-router, TS strict) + NativeWind v4 with the exact web token palette (`bg`/`ink`/`muted`/`dim`/`line`/`line2`/`accent`/`down`) in `mobile/tailwind.config.js`. Template puts routes under `src/app/` (not `app/`) so `global.css`/content globs/metro `input` were adapted to that path; `nativewind-env.d.ts` also needed `declare module "*.css"` for TS6's stricter side-effect-import check. Minimal `src/app/_layout.tsx` (Stack, dark contentStyle) + `src/app/index.tsx` (bg/ink/accent classes) per the brief. `app.json` renamed to name `PitchMarket`, slug/scheme `pitchmarket`. | `npx tsc --noEmit` clean · `npx expo export --platform web` succeeded (bundler + NativeWind config load and compile) · device/Expo-Go boot **not** run — pending |
| 2026-07-16 | Prasad | **Mobile app design spec** (`docs/superpowers/specs/2026-07-16-mobile-app-design.md`): Expo/RN app at `mobile/`, core trading loop only (markets → trade → deposit → portfolio), Privy Expo embedded wallet (SecureStore demo-wallet as cut), copied `frontend/lib` pure-TS files gated by the borsh golden vector. No code yet. | doc only — n/a |
| 2026-07-16 | Ashish | **Admin panel (`/admin`)** — operator-gated manual market control, the mitigation for the auto full-time cascade never firing on a quiet feed. Backend: `txodds.OddsSnapshot` on-demand accessor + `api.FixtureSource` wiring (provider lifted to `run()` scope, `WithFixtures`), `lifecycle.ResolveMarketManually` (single-market binary/precision resolve reusing the cascade primitives), new `internal/api/admin.go` — operator-wallet challenge→session auth (ed25519, in-memory token) + handlers for fixtures/odds/create-markets/resolve/close/clear-orders/resolve-from-score/ops. `ADMIN_PUBKEY` env (defaults to operator). Frontend: `app/admin/page.tsx` + `lib/adminApi.ts` reusing `usePitchWallet.signMessage`, design tokens, TopBar, VerifyLink. | `go build`/`vet` ✅ · `go test ./internal/api -run TestAdmin` ✅ (auth 401/403/200 + replay-reject; binary→settled, precision→settled, bad outcome→400, unauthed→401) · frontend `tsc --noEmit` ✅ · `npm run build` ✅ · live click-through pending |
| 2026-07-15 | Ashish | **Adversarial / demo-readiness pass** (`docs/DEMO-READINESS.md`). Found + fixed a real **uint64 overflow exploit**: a crafted order size wrapped `BuyCost` near zero, locking ~$0.25 for a 30-trillion-share order (accepted as 200) — now capped at `models.MaxOrderSize` in the engine + store. New tests: exchange invariants (concurrency no-over-fill, money conservation to the micro, replay-once, revert-reconcile), 28 HTTP attack-vectors (all clean 4xx, never 5xx), whole-server concurrent HTTP load (120 in-flight reqs, 0×5xx, WS stays live). Also fixed mirror-divergence (engine unwinds on rejected mirror write; bot quotes expire) and CreateIdempotent settle ATAs. | `go test ./internal/exchange` (chaos) ✅ · `./internal/api` (attack + load) ✅ · full regression green · live server restarted on the fixed binary, covering England vs Argentina |
| 2026-07-15 | Ashish | **Everything real, end to end.** TxLINE integration via the free World Cup tier (guest JWT → on-chain `subscribe` to txoracle `6pW64…` → activation; self-provisioning, cached) — England vs Argentina fixture live-priced (dnb 54¢). New TxLINE-priced templates (dnb_home w/ VOID, ou_1h_075) + per-market VOID resolution. Server on-chain mode: markets initialized/resolved on devnet, crank settles UI fills (CreateIdempotent ATA fix), two-step cosigned deposits, bot's own on-chain vault. Realized PnL on sells (+migration). Gemini one-liners. Frontend: fixtures.ts DELETED, markets index, combos RFQ builder (mutex greying, quote countdown), precision page (entry, distribution, leaderboard), portfolio exit/cancel + realized/unrealized PnL at best-bid mark, real deposit flow in TradePanel. | Scripted product e2e vs the LIVE stack: real deposit txs; fill → devnet settle `27VgrXKiLby34HiPorxTZYRBhK2R8zjn191sxK3MRtuADqJ3cBkHnaxV5txqHWFvpucq7ktMfAemXWydQj83M4qQ`; exit/cancel/precision ✓; combo quoted $5→$14.17 from live TxLINE prices, mutex rejected ✓ · Go suites + golden vector + `npm run build` ✅ · caveat: public devnet RPC 429s under load (index poll now 30s) |
| 2026-07-15 | Ashish | Frontend motion-polish pass (press feedback, state crossfades, sliding tab underline, scaleX depth bars, MotionConfig reduced-motion, touch-gated hovers); **wired frontend to the live exchange** (REST mapping + WS + Privy/demo wallet + real signing); CORS on the Go API; TS borsh encoder pinned to the golden vector in `npm run build` | `npm run build` ✅ (golden vector gate) · scripted browser-flow e2e vs `go run ./cmd/server` ✅: deposit → signed order accepted → bad sig 401 → replay 409 → ladder shows the bid → portfolio row |
| 2026-07-15 | Ashish | **Program deployed to devnet** at the pinned ID; **Go crank settled a real match on devnet** (v0+ALT); gentler RPC polling in crank/harness (public devnet endpoint rate-limits) | `cmd/devnet-e2e` ✅ full run: initialize→vaults→deposits→engine MINT fill→**settle_match**→resolve→redeem, every balance asserted; tx sigs in §5 |
| 2026-07-12 | Ashish | **Crank v0 + per-market ALT** (`crank/lut.go`, `BuildSettleMatchTxV0`); chain builders (initialize_market/init_vault/deposit/resolve/redeem) + `cmd/devnet-e2e` harness; committed program keypair (decision #1 closed); reproduced §4 fix on 2nd machine (Agave 4.1.1 → `.so` 419,400 B); pinned v0+ALT in interface-contract §6.5. **Devnet deploy blocked only on faucet SOL.** | `go test ./internal/crank` ✅ (v0 1116 B ≤ 1232, legacy 1421 B rejected; layout tests) · `cargo-build-sbf` ✅ on this machine · devnet run pending funds |
| 2026-07-12 | Ashish | Merged PR #3 into main; reconciled this file across both tracks (E1 localnet results + E2 backend state + v0/ALT crank rework now tracked in §5/§7) | host `cargo test -p pitchmarket` ✅ · `go build`/`vet` + targeted Go suites ✅ on the merged tree |
| 2026-07-13 | E1 | Added MERGE + cancel_order tests; refactored the TS harness into `tests/helpers.ts` (single borsh impl) | `npm test` **8/8 ✅** on `solana-test-validator` — all settle paths + cancel fail-closed |
| 2026-07-12 | E1 | Fixed §4 build blocker (platform-tools v1.54); Boxed `SettleMatch` accounts (BPF stack overflow); added `idl-build` feature; added TS lifecycle test harness (`tests/`, `package.json`) | `cargo build-sbf` ✅ · `npm test` 5/5 ✅ on `solana-test-validator` (initialize→deposit→settle MINT+NORMAL→resolve→redeem, balances asserted) |
| 2026-07-11 | Ashish | **E2 backend complete**: matching MINT/MERGE + cancel/expiry/unfill; Postgres store on Neon; exchange core with revert→reconcile; crank §6.5 tx builder + RPC submitter + off-chain mode; WS hub; full REST API; RFQ with mutex groups; precision; MM bot; lifecycle; txodds SSE skeleton; Claude one-liners; chain-index poller; server wiring; borsh golden vectors Go↔Rust; demo replay fixture | `go build` ✅ `go vet` ✅ · every test package green against real Postgres (11 pkgs incl. HTTP e2e + WS + revert path); two packages hit Neon network timeouts in the one full serialized run and passed on immediate re-run (§3 caveat) · `cargo test -p pitchmarket` ✅ 4/4 · `anchor build` ❌ at the time (fixed next day, §4) |
| 2026-07-10 | Ashish | Added `progress.md` + `CLAUDE.md`; trimmed stale README status; untracked `.DS_Store`; committed the E1/E2 scaffold | `cargo check` ✅ · `go build ./... && go vet ./...` ✅ · `anchor build` ❌ (§4) |
| 2026-07-09 | E1 | Implemented `sig_verify::verify_order_signature`; pinned settle_match tx layout in interface-contract §6.5 | `cargo check` only — never executed |
| 2026-07-08 | E1/E2 | Anchor program scaffold; Go matching engine, crank skeleton, order API, replay feed, Postgres schema | `cargo check` · `go build` |
