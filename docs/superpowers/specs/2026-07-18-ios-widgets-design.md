# iOS Home-Screen Widgets — Portfolio & Active Market

**Date:** 2026-07-18 · **Scope:** `mobile/` only (plus this doc + progress.md) · **Approach:** A — SwiftUI WidgetKit extension via `@bacons/apple-targets`, widgets fetch the backend API directly on WidgetKit's timeline.

## Goal

Two pinnable iOS home-screen widgets for the PitchMarket Expo app:

1. **Portfolio widget** (medium) — available balance, position value, unrealised / realised PnL, locked funds, and the open-orders count.
2. **Active-market widget** (large) — the user's **biggest open position by value**: market title, current price (big), entry (avg bought) price, side/size, unrealised + realised PnL for that market, and a proper price chart of recent fills.

Refresh model (agreed): **periodic** — WidgetKit timeline every ~15 min, plus an immediate reload whenever the app updates shared state (wallet connect, foreground refresh). No streaming; widgets show a "as of HH:MM" stamp.

## Why Approach A

- `ios/` is **gitignored and regenerated** by `expo prebuild` — a hand-added Xcode widget target would be destroyed. `@bacons/apple-targets` declares the target in `mobile/targets/widget/` with an `expo-target.config.js`, and its config plugin re-links it into the Xcode project on every prebuild.
- Widgets fetch the API themselves (URLSession in the `TimelineProvider`), so data stays fresh even when the app hasn't been opened — the alternative (app-written snapshots only) goes stale.
- Rejected: RN widget wrapper libraries (poorly maintained vs current SDKs, same mechanism underneath).

## Architecture

```
mobile/
  targets/widget/
    expo-target.config.js      # type: "widget", App Group, deployment target
    Widgets.swift              # @main WidgetBundle: PortfolioWidget + ActiveMarketWidget
    Provider.swift             # shared TimelineProvider-ish helpers: config load, fetch, decode
    Api.swift                  # wire DTOs (Decodable, snake_case) + PnL math
    PortfolioViews.swift       # SwiftUI medium view
    ActiveMarketViews.swift    # SwiftUI large view + price chart (Path)
  src/lib/widgetBridge.ts      # RN side: write shared config, reload widgets
  app.json                     # + "@bacons/apple-targets" plugin, App Group entitlement
```

### Data flow

1. **RN → shared storage.** `widgetBridge.ts` writes `{ wallet, apiUrl }` to the App Group (`group.com.pitchmarket.app`) via `@bacons/apple-targets`'s `ExtensionStorage`, and calls `ExtensionStorage.reloadWidget()`. Called from `PitchWalletProvider` whenever the wallet address changes (connect/disconnect, both demo and Privy backends) — one `useEffect` in `wallet.tsx`. `apiUrl` is `process.env.EXPO_PUBLIC_API_URL`. On iOS only; no-op elsewhere (`Platform.OS` guard + lazy require, mirroring the Privy lazy-require pattern).
2. **Widget timeline.** Each widget's `TimelineProvider.getTimeline`:
   - reads `wallet`/`apiUrl` from the App Group `UserDefaults`; if absent → "Open PitchMarket to connect" placeholder entry, retry in 30 min;
   - `GET {apiUrl}/portfolio?wallet={wallet}` and `GET {apiUrl}/markets` (title join — the portfolio wire rows carry only `market_id`);
   - active-market widget additionally `GET {apiUrl}/markets/{id}/fills` for the picked market;
   - builds **one** entry, `policy: .after(now + 15 min)`. Network failure → keep an error/stale entry, retry after 5 min.
3. **Tap targets.** Portfolio widget → `pitchmarket://portfolio` (well-known scheme, already registered). Active-market widget → `pitchmarket://market/{market_id}` via `widgetURL`. (expo-router handles both paths.)

### Wire decoding & PnL math (mirrors `mobile/src/lib/api.ts` / web `portfolio.tsx`)

Portfolio wire: `balance.usdc_available` (micro), `positions[] {market_id, yes, no, yes_locked, no_locked, avg_cost, realized, best_bid}`, `orders[] {market_id, outcome, side, price, size, remaining, status}`, `fills[]`.

Per open position (`yes>0 || no>0`):

- side = yes>0 ? YES : NO; qty = that side's shares
- `cur` = best_bid>0 ? (YES ? best_bid : 100−best_bid) : avg_cost
- `valueMicro = qty × cur × 10_000`; `unrealizedMicro = valueMicro − qty × avg_cost × 10_000`

Aggregates: **Available** = `balance.usdc_available`; **Position value** = Σ valueMicro; **Unrealised** = Σ unrealizedMicro; **Realised** = Σ realized (all positions, incl. flat ones); **Locked** = Σ over live BUY orders of `remaining × price × 10_000` (USDC soft-locked on the book); **Open orders** = count of live orders. Active market = open position with max valueMicro.

Chart: last ≤100 fills for that market from `/markets/{id}/fills`, plotted price-vs-ts, YES terms (fills are stored in YES terms already — same as `RecentFills`), with a dashed reference line at the entry (avg_cost) price so the chart reads as "price vs where I bought"; flat line at `cur` if <2 fills.

### Visual

Dark, matching the app palette (`bg #0A0A0A`-family, ink/muted/dim grays, accent green for gains, red for losses — copy hexes from `mobile/tailwind.config.js` into a Swift `Palette` enum). Monospaced digits (`.monospacedDigit()`), "as of HH:MM" footer, `containerBackground` API (iOS 17+, matches the existing `deploymentTarget: 17.5`).

- **Portfolio medium (only size):** 4-stat grid (Available · Value · Unrealised · Realised, PnL signed + tinted) with a footer line `N open orders · $X locked · as of HH:MM`.
- **Active-market large (only size):** header = market title + side·qty badge; price block = current price big (e.g. `47¢`) with entry beside it (`bought 42¢`); PnL row = unrealised (tinted, dominant) + realised-for-this-market when nonzero; price chart fills the lower half with the dashed entry line and gain/loss-tinted stroke.
- **Empty states:** no wallet → "Open PitchMarket to connect"; no positions (active-market) → "No open positions".

## Config plugin changes (`app.json`)

- Add `"@bacons/apple-targets"` plugin (with `appleTeamId` — required for signing the extension; dev-client builds need the user's team id, ask/placeholder via env or `EXPO_APPLE_TEAM_ID`).
- App Group `group.com.pitchmarket.app` on both the app (`ios.entitlements`) and the widget target config.
- New dep: `@bacons/apple-targets` only. No backend, `frontend/`, or program changes.

## Error handling

- Missing `EXPO_PUBLIC_API_URL` at bridge-write time → skip write, widget shows the connect placeholder.
- API/network failures inside the provider → previous data shown if the entry can carry it, else an unobtrusive "Couldn't reach exchange" state; always reschedule (5 min).
- `Number` fields decoded as `Int64`/`Double` defensively; unknown JSON fields ignored (Decodable default).
- Widget never signs or writes anything — read-only; the wallet secret never enters the App Group.

## Testing / verification

- **Swift unit-testability is out of scope** (no test target on a 0-day budget); PnL math is a direct port of already-tested TS.
- RN side: jest test for `widgetBridge` no-op guard is trivial/optional; `tsc --noEmit`, `npm test` (existing 7), `npm run check-borsh` must stay green.
- **Real verification:** `npx expo prebuild -p ios --clean` regenerates the project with the widget target → `npx expo run:ios` → in the simulator, pin both widgets to the home screen; confirm placeholder→data transition after connecting a wallet in the app against a live `cmd/server`. Simulator pass = 🟡→✅ for rendering; physical-device pinning stays a human step, recorded in progress.md.
- progress.md: component row + changelog in the same commit(s), per the working agreement.

## Cut lines (in order, if time runs out)

1. Chart → simple sparkline without the entry reference line.
2. Chart → flat avg→cur bar (drop the fills fetch entirely).
3. Locked stat + orders count (drop; the only stats needing the orders array).
