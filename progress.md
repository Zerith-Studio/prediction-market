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
| `mobile/` | 🟡 lib e2e ✅, UI unverified | Expo core loop: markets, market+trade, deposit, portfolio; Privy behind `EXPO_PUBLIC_PRIVY_APP_ID` + SecureStore demo wallet default; borsh vector gated (5th encoder, TS↔Rust↔Go↔web↔mobile). **`mobile/scripts/e2e-flow.ts` (`npm run e2e`) ran GREEN 3/3 vs a live `cmd/server`** (see §9 Jul-16 row for exact setup — no Neon `.env` on this machine, so this ran off-chain-mirror mode against a local throwaway Postgres container instead of Neon; same Go store/exchange code path either way). **Caught and fixed a real bug while doing it**: `randomSalt()` produced a 63-bit value, but `api.postOrder` wire-encodes salt via `Number(salt)` — JS silently rounds anything ≥2^53, so the signed bytes and the wire salt diverged and the backend 401'd on almost every order (~99.9% of draws). Fixed by narrowing `randomSalt()`'s range to <2^53 so the bigint→Number→JSON→uint64 round-trip is exact; added a regression test. **The identical bug exists in `frontend/lib/borsh.ts`'s `randomSalt()`/`TradePanel.place()`, untouched here (out of scope — mobile/ and progress.md only); flagging for a follow-up fix.** All 8 on-device click-through flows (Step 3 of the brief) are **unrun — no physical device available in this environment** — every UI screen (markets, market+ladder, trade, deposit, portfolio) stays 🟡 written-but-unverified-on-device until a human does that pass. |

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
| 2026-07-18 | Prasad | **iOS home-screen widgets design spec** (`docs/superpowers/specs/2026-07-18-ios-widgets-design.md`): WidgetKit extension via `@bacons/apple-targets` (survives gitignored-`ios/` prebuild), portfolio widget (available/value/unrealised/realised/locked) + active-market widget (biggest position, sparkline from `/markets/{id}/fills`), App Group shares `{wallet, apiUrl}` written from `wallet.tsx`, ~15-min timeline refresh. No code yet. | doc only — n/a |
| 2026-07-18 | Prasad | **Fix Vercel frontend deploy** — `npm install` failed ERESOLVE: `@solana-program/memo@^0.8.0` (peer `@solana/kit@^3.0`) conflicted with kit 5.5.1 forced by Privy's dep tree; the local lockfile had shipped that inconsistent pair, which Vercel's strict peer check rejects. memo is unused in our code (only fills Privy's optional peer `>=0.8.0`), so bumped it to `^0.10.0` (peer kit `^5.0`) and regenerated `frontend/package-lock.json`. | `npm install --strict-peer-deps` ✅ clean · `npm run build` ✅ (golden-vector prebuild gate + all 8 routes) · Vercel redeploy 🟡 pending push |
| 2026-07-17 | Prasad | **Mobile: dev-client build for Privy** — `npx expo prebuild`/`run:ios` built the native iOS dev client (`ios/` gitignored; package.json ios/android scripts now `expo run:*`). App boots natively with Privy active; Privy login currently blocked on a dashboard setting (`com.pitchmarket.app` must be allowlisted as a native app identifier) — error pinned in Metro logs. | dev client built + installed on iPhone 17 simulator, our bundle loads (markets live); Privy OAuth 🔴 pending dashboard allowlist |
| 2026-07-16 | Prasad | **Mobile: first native (simulator) boot** — two real native-bundle fixes: (1) Privy's transitive dep `jose` resolves to its Node build on native (no `react-native` export condition) and crashed the bundle on `require("buffer")` — added `buffer` dep + `resolver.unstable_conditionNames = ["browser","require","react-native"]` in `metro.config.js` per Privy's RN setup docs; (2) web preview crash fixed earlier the same day (seedStore shim). | iOS bundle builds (3359 modules) · app boots in the iPhone 17 Pro simulator via Expo Go: markets index renders live data from the local demo backend (screenshot) · manual click-through in progress |
| 2026-07-16 | Prasad | **Mobile: web-preview fix** — `expo-secure-store` has no web implementation, so the demo wallet's restore effect crashed `expo start` web preview at mount (`getValueWithKeyAsync is not a function`). Added a `seedStore` platform shim in `mobile/src/lib/wallet.tsx`: localStorage on web (mirroring the web app's demo wallet), SecureStore on native. Native path unchanged. | `tsc --noEmit` clean · `npm test` 7/7 · **browser click-through on the web build (Playwright, 390×844)**: markets index → liquid ladder (47/43 spread) → connect → deposit $1,000 → buy 10 YES @ 45¢ → resting bid visible on ladder + portfolio row with Cancel, vault $995.50 — screenshots captured. On-device (Expo Go) pass still pending |
| 2026-07-16 | Prasad | **Mobile Task 12: lib-level e2e vs a live backend (final task).** Added `mobile/scripts/e2e-flow.ts` (`npm run e2e`, `tsx` devDep) — drives the shipped `mobile/src/lib/api.ts`/`borsh.ts` through wallet keygen → two-step/mirror deposit → find an open binary market → sign+place a resting bid via `borshOrder` → see it on `getBook`/`getPortfolio` → cancel it, asserting at each step. **No root `.env` exists on this machine** (no Neon `DATABASE_URL` anywhere — checked shell profiles, `~/.config`, git history; genuinely absent, not network flake), so `backend/cmd/server` couldn't start against Neon as the brief assumed. Ran it instead against a disposable local Postgres 16 (Docker, dropped after) with `DATABASE_URL` passed as a process env var only — **root `.env` was never touched, never created**. Same `DEMO_FIXTURE=demo-final` off-chain-mirror server everyone else uses; the store/exchange code path is identical to Neon, only the Postgres endpoint differs. `demo-final`'s replay fixture is only 15s of recorded match time, which at the default `REPLAY_SPEED=60` resolves every market before a script can even connect — set `REPLAY_SPEED=0.05` (a knob already in `cmd/server`, no code change) to hold markets open long enough. First run 401'd with "ed25519 signature does not verify" — root-caused to a **real bug**, not an environment issue: `randomSalt()` in `mobile/src/lib/borsh.ts` (ported from `frontend/lib/borsh.ts` in Task 2) clears only the top bit of a random uint64, but `api.postOrder`'s wire DTO sends `salt: Number(salt)` — `Number(bigint)` silently rounds any value ≥2^53, so ~999/1000 real orders sign one salt and transmit a different (rounded) one, and the backend's signature check on the reconstructed `Order` always fails. Fixed by narrowing `randomSalt()`'s range to stay under 2^53 (still clears the sign bit for the BIGINT column; comment explains why) and added a jest regression test (`bigint -> Number -> JSON -> uint64` round-trips exactly, 64 draws). **This same bug is present in `frontend/lib/borsh.ts` — untouched, out of scope for this task (mobile/ + progress.md only) — flagging here for a follow-up fix**, since the 07-15 "browser-flow e2e green" changelog entry likely got lucky on a small salt draw or predates the current `randomSalt()`. After the fix, `npm run e2e` ran green **3/3 consecutive times** against the live server. `npm test` 7/7 (1 new test), `npx tsc --noEmit` clean, `npm run check-borsh` ok. Server started/stopped manually (`go run ./cmd/server` in background, `lsof -ti:8080 \| xargs kill`); Postgres container `docker rm -f`'d after. **Step 3 (on-device click-through, the ✅ gate for the UI) was not run — no physical device in this environment** — every mobile UI flow (markets, market+ladder+trade, deposit, portfolio) stays 🟡 pending a human pass. | `mobile/scripts/e2e-flow.ts` green 3/3 vs `cmd/server` (off-chain mirror, local Postgres substituting for the unavailable Neon `.env`) · `npm test` 7/7 (new salt round-trip regression test) · `npx tsc --noEmit` clean · `npm run check-borsh` ok · on-device click-through **not run** — pending |
| 2026-07-16 | Prasad | **Mobile Task 11: Privy Expo embedded wallet behind `EXPO_PUBLIC_PRIVY_APP_ID`.** Extended `mobile/src/lib/wallet.tsx`'s `PitchWalletProvider` to pick a `PrivyProvider`/`PrivyBridge` backend when the env var is set (demo `SecureStore` wallet otherwise, unchanged, still the default) — mirrors `frontend/lib/wallet.tsx`'s selector. Checked current Privy Expo docs first (package list, hook names, message-signing shape all moved since the brief was written): `@privy-io/expo` v0.70.3 exports `PrivyProvider`, `usePrivy` (`{user, isReady, logout}`), `useLoginWithOAuth` (`login({provider})`), `useEmbeddedSolanaWallet` (`{wallets, status, create}`) — confirmed against the installed package's `.d.ts`, not just docs prose. **Deviation from the brief:** `connect()` uses `useLoginWithOAuth({provider:"google"})`, not the brief's sketched `useLogin({loginMethods:["email"]})` (that hook doesn't exist in `@privy-io/expo`) — the real Expo email flow is `useLoginWithEmail`'s two-call `sendCode`/`loginWithCode({code, email})`, which needs a form and doesn't fit `PitchWallet.connect(): Promise<void>`'s no-arg contract; OAuth is the one login hook that's a single call. `signMessage` base64-encodes the message (added `bytesToB64` to `mobile/src/lib/base64.ts`) because the installed SDK's `provider.request({method:"signMessage"})` takes `params.message: string` and always returns `{signature: string}` (base64) — never raw bytes as the brief's sketch guessed — decoded back via the existing `b64ToBytes`. `PitchWallet` mapping (ready/address/isDemo/connect/disconnect/signMessage→64-byte sig) kept byte-identical to what every screen already consumes. Installed `@privy-io/expo @privy-io/expo-native-extensions expo-application expo-crypto expo-linking expo-web-browser expo-apple-authentication react-native-webview react-native-passkeys expo-build-properties fast-text-encoding @ethersproject/shims` (the brief's install command omitted `expo-apple-authentication`/`react-native-passkeys`, which the OAuth chunk imports transitively — omitting them broke `expo export --platform web` with an unresolvable-module error; adding them fixed it). `mobile/app.json`: `com.pitchmarket.app` iOS bundle id / Android package, `expo-build-properties` plugin (`ios.deploymentTarget: "17.5"`), `expo-web-browser` plugin. `@privy-io/expo` is `require()`d lazily inside the `PRIVY_APP_ID` branch (not statically imported) as a defensive measure against it being pulled into Privy-less builds — though the actual web-export breakage traced to the missing peer deps above, not the import style; kept the lazy require anyway since it's free insurance and documents the risk inline. Added `EXPO_PUBLIC_PRIVY_APP_ID`/`EXPO_PUBLIC_PRIVY_CLIENT_ID` placeholders to `mobile/.env.example` (both blank — no real `.env` committed). **No Privy dashboard credentials and no physical device/dev-client build were available in this environment** — did not run `expo prebuild`/`expo run:ios`/`expo run:android`, per the brief's explicit no-device scope. | 🟡 **Privy path written but UNVERIFIED** — never run against a real Privy app, no dashboard app created, no OAuth login/wallet-creation/silent-signing exercised on a device. Demo path **regression-verified**: `npx tsc --noEmit` clean · `npm test` → 6/6 pass (all pre-existing, no new tests) · `npm run check-borsh` → golden vector ok · `npx expo export --platform web` succeeds **with no Privy env vars set** (7 static routes, same as before this task) — confirms the demo-wallet path and its bundle are untouched by the new Privy code path. |
| 2026-07-16 | Prasad | **Mobile Task 10: Portfolio screen — balance, positions, exit, open orders, cancel.** Replaced the Task 6 stub body of `mobile/src/app/(tabs)/portfolio.tsx` verbatim from the brief: vault balance + `DepositButton`, positions list with avg→current, unrealized PnL (`(current − avg_cost) × yes × 10_000` micro) and realized PnL when nonzero, an "Exit at {price}" button per position, open orders list with cancel. Exit signs a SELL YES order at `p.current` for the full `p.yes` shares — same borsh/postOrder field discipline as Task 8's TradeSheet (outcome 1, side 1, fee_bps 0, expiry 0, `salt: Number(salt)`, hex sig via `toHex`). No new libs needed — all consumed exports (`api.getPortfolio/cancelOrder/postOrder`, `usePitchWallet`, `DepositButton`, `borshOrder/fromHex/randomSalt/toHex`, `placeErrorMessage`, `cents/shares/usd`, `Portfolio`/`Position` types) already existed from Tasks 2–9. No deviations from the brief's code. | `npx tsc --noEmit` clean · `npm test` → 6/6 pass (no new tests, all pre-existing) · `npx expo export --platform web` succeeded (7 static routes incl. `/(tabs)/portfolio`) · on-device portfolio view (balance/positions/PnL matching web portfolio, cancel shrinking the ladder, exit filling against the bot's bid) against the live backend **not run** — pending device access |
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
| 2026-07-16 | Ashish | **Resolution durability (reconciler)** — fixed the core fragility that resolution fired only on the ephemeral SSE `full_time` event, so a missed event (service down / stream drop / TxLINE never emits it) left a match stuck `live` forever (fixture `18241006`). Added event-driven-fast-path **+ reconciliation guarantee**: `txodds.FinalState` (folds the authoritative score snapshot → final score + finished), `store.UnresolvedMatches`, idempotent `ResolveFixture` (skips settled markets — safe re-runs, no double payout), and `cmd/server reconcileResolutions` (startup + 3-min ticker; hybrid policy: resolve on finished, else auto-resolve after 40-min score stability, else leave stale). Stale matches surfaced in `/admin/ops`. Modeled on the existing chain-index reconciler — not naive polling (idle when healthy). **Note: `total_passes` still unmapped from the feed → passes pool needs an admin value.** | `go build`/`vet` ✅ · `go test ./internal/lifecycle -run 'TestResolveFixtureIdempotent\|TestUnresolvedMatchesRecovery'` ✅ (idempotent re-resolve = no double payout; unresolved query surfaces + drops) · reconciler loop itself exercised only on restart (not yet observed recovering a live stuck match) |
| 2026-07-16 | Ashish | **Frontend cards + Yes/No trading** — Markets/Precision/Combos rendered as bordered card tiles (hover-lift, press feedback); binary cards carry Yes/No buttons that deep-link into the trade panel with the outcome preselected (`?o=yes\|no`); `TradePanel` made **outcome-aware** (YES/NO toggle, submits outcome 0/1 at its price — was YES-only). Fixed a card display bug where settled precision pools still showed "POOL" (now show the winning value / locked / pool via `PrecisionState`). Privy embedded wallet enabled (`NEXT_PUBLIC_PRIVY_APP_ID` in gitignored `frontend/.env`; secret NOT stored — server-side only). | `tsc --noEmit` ✅ · `npm run build` ✅ (all routes incl. `/admin`) · Privy app ID verified inlined into the client bundle · login click-test + Privy-dashboard Solana/origin config pending (user) |
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
