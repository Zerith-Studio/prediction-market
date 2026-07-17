# PitchMarket Mobile — consumer app design

**Date:** 2026-07-16 · **Status:** approved
**Goal:** a consumer mobile app covering the core trading loop of the existing web
frontend, as a new client on the frozen, verified Go backend API. No backend changes.

## Context

The floor is done (progress.md §5): program deployed on devnet, Go backend complete
with full REST + WS surface, web frontend live and wired (markets, trading, portfolio,
deposits, Privy + demo wallet, real signing). Judging is by 2026-07-29; the mobile app
is submission polish built in that window.

## Decisions (settled during brainstorming)

| Decision | Choice |
|---|---|
| Platform | Expo / React Native (iOS + Android) |
| Scope | Core trading loop only — markets index, market page + trade, deposit, portfolio. Combos, precision, admin stay web-only |
| Wallet | Privy Expo SDK (`@privy-io/expo`), email login → embedded Solana wallet. Fallback cut: SecureStore ed25519 demo wallet |
| Code sharing | Standalone `mobile/` app; copy the pure-TS lib files from `frontend/lib/` and wire the borsh golden-vector check into the mobile build |

## 1. Architecture

- New `mobile/` directory at the repo root: Expo app, TypeScript, `expo-router`.
- **Privy Expo SDK requires native modules → no plain Expo Go.** The app runs via an
  Expo dev-client build (`npx expo prebuild` + local build or EAS). One-time setup cost.
- Styling: NativeWind, importing the web app's design tokens (colors/spacing/type from
  `frontend/tailwind.config.ts` and `globals.css`) so both apps read as one product.
- Copied libs: `frontend/lib/{api.ts,borsh.ts,types.ts,format.ts}` →
  `mobile/lib/`, adapted only where DOM/Next-specific. `scripts/check-borsh.mjs`
  is copied and wired as a `prebuild`-style gate in `mobile/package.json` — the mobile
  encoder becomes the fifth cross-checked borsh encoder; drift fails the build.

## 2. Screens

1. **Onboarding / login** — Privy email login creates the embedded Solana wallet.
   Browsing is allowed logged-out; trading/portfolio prompt login.
2. **Markets tab** — live index: fixture, market title, YES/NO prices, match-state
   badge, one-liner ticker. Pull-to-refresh + WS-driven updates.
3. **Market screen** — price header, unified YES depth ladder (same REST mapping the
   web uses), one-liners, match state. Bottom-sheet **trade panel**: outcome/side,
   price, size, cost + fee preview → Privy `signMessage` over borsh order bytes →
   POST `/orders` → fill toast via WS.
4. **Deposit modal** — mirrors the web's two-step cosigned deposit: backend endpoints
   build the tx, Privy `signTransaction`, submit, poll balance.
5. **Portfolio tab** — USDC balance, open orders with cancel, positions with
   realized/unrealized PnL at best-bid mark, exit (market-sell). Same semantics as web.

Navigation: two tabs (Markets, Portfolio) + stack for market screen; deposit and trade
panel are modals/sheets.

## 3. Data flow

- `EXPO_PUBLIC_API_URL` / `EXPO_PUBLIC_WS_URL` in `app.config.ts`, pointing at the
  existing Go server. Zero backend changes.
- A `useLiveMarket` equivalent on React Native's built-in WebSocket, subscribed to the
  same six pinned WS events. Reconnect with backoff; on app foreground, refetch the
  REST snapshot before resuming the stream (backgrounding pauses sockets).

## 4. Wallet & signing

- Privy Expo SDK, email login, embedded Solana wallet. The wallet pubkey is the account
  identity (balances, portfolio) exactly as on web.
- Orders: raw ed25519 `signMessage` over the exact borsh bytes — indistinguishable from
  a web order at the backend.
- Deposits: `signTransaction` on the backend-built cosigned transaction.

## 5. Error handling

- Map the pinned API semantics to toasts: 401 bad signature, 409 replay, 410
  post-kickoff.
- WS drop / offline: show a stale-data banner and pause trading affordances rather than
  silently freezing prices.

## 6. Verification plan (progress.md rules apply)

- Borsh golden-vector gate in the mobile build — mechanical, runs every build.
- Scripted flow against the live backend: login → deposit → signed order accepted →
  ladder shows the bid → portfolio row → exit.
- Manual on-device click-through before anything is marked ✅.
- `progress.md` gains a `mobile/` component row + changelog entries in the same commits.

## 7. Risks & cut order

1. **Privy Expo native setup** (dev-client build, device provisioning) is the biggest
   risk. If it stalls more than a day: cut to a SecureStore-held ed25519 demo wallet
   (drop-in with the same signing code), keep Privy behind a flag.
2. **Deposit flow** second: fall back to "fund on web, trade on mobile" — the account
   is the wallet pubkey, so a Privy user funded on web can trade on mobile.
3. Combos/precision are already out of scope; do not pull them in.
