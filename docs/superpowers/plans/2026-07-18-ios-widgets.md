# iOS Home-Screen Widgets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two pinnable iOS home-screen widgets for the Expo app — Portfolio (medium) and Active Market (large, with price chart) — per `docs/superpowers/specs/2026-07-18-ios-widgets-design.md`.

**Architecture:** A WidgetKit extension target declared in `mobile/targets/widget/` via the `@bacons/apple-targets` config plugin (survives the gitignored-`ios/` prebuild). The RN app writes `{wallet, apiUrl}` to App Group `group.com.pitchmarket.app`; the Swift `TimelineProvider`s fetch `/portfolio?wallet=`, `/markets`, and `/markets/{id}/fills` directly every ~15 min. Read-only; no secrets in the App Group.

**Tech Stack:** `@bacons/apple-targets@^5.0.0` (config plugin + `ExtensionStorage`), SwiftUI/WidgetKit (iOS 17.5 target, `containerBackground`), existing Go REST API (no backend changes).

## Global Constraints

- Scope: `mobile/` + `progress.md` + this plan only. No `backend/`, `frontend/`, or program changes.
- App Group id: `group.com.pitchmarket.app`. Shared keys: `wallet` (base58 string), `apiUrl` (string).
- Deep-link scheme: `pitchmarket` (already registered). Portfolio → `pitchmarket://portfolio`, market → `pitchmarket://market/{market_id}`.
- Palette (from `mobile/tailwind.config.js`): bg `#0a0a0b`, ink `#f4f5f7`, muted `#9297a0`, dim `#565b63`, line `#1b1c20`, accent `#34d399`, down `#f2637e`.
- PnL math must mirror `frontend/app/portfolio/page.tsx` `calc()`: side = yes>0?YES:NO; cur = best_bid>0 ? (YES?bb:100−bb) : avg_cost; valueMicro = qty×cur×10_000; unrealized = value − qty×avg_cost×10_000. Locked = Σ live BUY orders `remaining×price×10_000`.
- Decode all numeric JSON as `Double` in Swift (defensive against int/float variance); ignore unknown fields.
- Regression gates after each RN-side task: `npx tsc --noEmit`, `npm test`, `npm run check-borsh` (run in `mobile/`).
- Commits: `type(scope): summary`, no attribution trailers; update `progress.md` in the same commit as code.
- `appleTeamId` comes from env `EXPO_APPLE_TEAM_ID` via `app.config.js` (optional — simulator builds don't sign).

---

### Task 1: RN bridge — shared state write + wallet integration

**Files:**
- Create: `mobile/src/lib/widgetBridge.ts`
- Create: `mobile/src/lib/__tests__/widgetBridge.test.ts`
- Modify: `mobile/src/app/_layout.tsx`
- Modify: `mobile/package.json` (dep)

**Interfaces:**
- Produces: `syncWidgetState(wallet: string | null): void` and `APP_GROUP = "group.com.pitchmarket.app"`. Task 3's Swift reads keys `wallet` / `apiUrl` from that App Group (absent or removed keys ⇒ "not connected").

- [ ] **Step 1: Install the dependency**

```bash
cd mobile && npm i @bacons/apple-targets
```

- [ ] **Step 2: Write the failing test**

`mobile/src/lib/__tests__/widgetBridge.test.ts` — the native module doesn't exist under jest, so the contract is "never throws, regardless of platform/module availability":

```ts
import { syncWidgetState, APP_GROUP } from "../widgetBridge";

test("app group id is pinned", () => {
  expect(APP_GROUP).toBe("group.com.pitchmarket.app");
});

test("syncWidgetState never throws when the native module is unavailable", () => {
  expect(() => syncWidgetState("8x9yK3mP2qR5tV7wA1bC4dE6fG8hJ9kL2mN4pQ6rS8t")).not.toThrow();
  expect(() => syncWidgetState(null)).not.toThrow();
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd mobile && npx jest src/lib/__tests__/widgetBridge.test.ts`
Expected: FAIL — cannot find module `../widgetBridge`.

- [ ] **Step 4: Implement `widgetBridge.ts`**

```ts
// Pushes the widget extension's config into the shared App Group and asks
// WidgetKit to reload. iOS-only: ExtensionStorage is a native module that
// exists only in dev-client/release iOS builds — every call is guarded so
// Expo Go, Android, web, and jest all no-op instead of crashing.
import { Platform } from "react-native";

export const APP_GROUP = "group.com.pitchmarket.app";

export function syncWidgetState(wallet: string | null): void {
  if (Platform.OS !== "ios") return;
  try {
    // Lazy require, same pattern as the Privy backend in wallet.tsx.
    const { ExtensionStorage } = require("@bacons/apple-targets");
    const storage = new ExtensionStorage(APP_GROUP);
    const apiUrl = process.env.EXPO_PUBLIC_API_URL ?? "";
    if (wallet && apiUrl) {
      storage.set("wallet", wallet);
      storage.set("apiUrl", apiUrl);
    } else {
      storage.remove("wallet");
    }
    ExtensionStorage.reloadWidget();
  } catch {
    // Native module absent (Expo Go / simulator without the target / jest).
  }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd mobile && npx jest src/lib/__tests__/widgetBridge.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 6: Wire into the app root**

`mobile/src/app/_layout.tsx` — a null-rendering child inside the provider covers both wallet backends (demo + Privy) with one hook site:

```tsx
import "react-native-get-random-values";
import "../global.css";
import { useEffect } from "react";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { PitchWalletProvider, usePitchWallet } from "@/lib/wallet";
import { syncWidgetState } from "@/lib/widgetBridge";

// Mirrors the wallet address into the iOS widget App Group whenever it
// changes (connect/disconnect, either backend). Renders nothing.
function WidgetSync() {
  const wallet = usePitchWallet();
  useEffect(() => {
    if (wallet.ready) syncWidgetState(wallet.address);
  }, [wallet.ready, wallet.address]);
  return null;
}

export default function RootLayout() {
  return (
    <PitchWalletProvider>
      <WidgetSync />
      <StatusBar style="light" />
      <Stack
        screenOptions={{
          headerShown: false,
          contentStyle: { backgroundColor: "#0a0a0b" },
        }}
      />
    </PitchWalletProvider>
  );
}
```

- [ ] **Step 7: Regression gates**

Run: `cd mobile && npx tsc --noEmit && npm test && npm run check-borsh`
Expected: tsc clean; jest all green (7 existing + 2 new); golden vector ok.

- [ ] **Step 8: Commit**

```bash
git add mobile/src/lib/widgetBridge.ts mobile/src/lib/__tests__/widgetBridge.test.ts mobile/src/app/_layout.tsx mobile/package.json mobile/package-lock.json progress.md
git commit -m "feat(mobile): widget bridge — share wallet/apiUrl with iOS App Group"
```

(progress.md: append a changelog row for this task, or fold Tasks 1–4 into one row at the final commit if executed in one session — the rule is code+progress.md land together on anything pushed.)

---

### Task 2: Widget target scaffolding + app config

**Files:**
- Create: `mobile/targets/widget/expo-target.config.js`
- Create: `mobile/app.config.js`
- Modify: `mobile/app.json` (plugin + main-app App Group entitlement)
- Modify: `mobile/.env.example` (document `EXPO_APPLE_TEAM_ID`)

**Interfaces:**
- Produces: an Xcode widget target named `widget` (bundle id `com.pitchmarket.app.widget`) that compiles every `.swift` file in `mobile/targets/widget/`; App Group entitlement on both app and extension.

- [ ] **Step 1: Target config**

`mobile/targets/widget/expo-target.config.js`:

```js
/** @type {import('@bacons/apple-targets/app.plugin').Config} */
module.exports = (config) => ({
  type: "widget",
  name: "widget",
  displayName: "PitchMarket",
  bundleIdentifier: ".widget", // → com.pitchmarket.app.widget
  deploymentTarget: "17.5", // matches the app's expo-build-properties target
  frameworks: ["SwiftUI", "WidgetKit"],
  colors: {
    $widgetBackground: "#0a0a0b",
    $accent: "#34d399",
  },
  entitlements: {
    "com.apple.security.application-groups":
      config.ios.entitlements["com.apple.security.application-groups"],
  },
});
```

- [ ] **Step 2: app.json — plugin + main-app entitlement**

In `mobile/app.json`, add to `expo.ios`:

```json
"entitlements": {
  "com.apple.security.application-groups": ["group.com.pitchmarket.app"]
}
```

and append `"@bacons/apple-targets"` to `expo.plugins`.

- [ ] **Step 3: app.config.js — optional team id from env**

`mobile/app.config.js` (Expo merges this over app.json; keeps the static file intact):

```js
// Injects the Apple Team ID (needed to code-sign the widget extension on a
// physical device) from the environment. Simulator builds work unsigned, so
// this is optional locally — set EXPO_APPLE_TEAM_ID for device builds.
module.exports = ({ config }) => ({
  ...config,
  ios: {
    ...config.ios,
    ...(process.env.EXPO_APPLE_TEAM_ID
      ? { appleTeamId: process.env.EXPO_APPLE_TEAM_ID }
      : {}),
  },
});
```

Append to `mobile/.env.example`:

```
# Apple Team ID — required only for signed device builds of the widget extension
EXPO_APPLE_TEAM_ID=
```

- [ ] **Step 4: Verify the resolved config**

Run: `cd mobile && npx expo config --type prebuild 2>/dev/null | grep -E "appleTeamId|application-groups|apple-targets" -A1`
Expected: the App Group array appears under ios entitlements; no `appleTeamId` (env unset) — and with `EXPO_APPLE_TEAM_ID=X npx expo config …` it appears. Plugin listed without errors. (Full prebuild is deferred to Task 5 — the target needs Swift sources first.)

- [ ] **Step 5: Commit**

```bash
git add mobile/targets/widget/expo-target.config.js mobile/app.config.js mobile/app.json mobile/.env.example
git commit -m "feat(mobile): declare WidgetKit extension target via @bacons/apple-targets"
```

---

### Task 3: Swift data layer — DTOs, PnL math, loader

**Files:**
- Create: `mobile/targets/widget/Api.swift`

**Interfaces:**
- Consumes: App Group keys `wallet`/`apiUrl` (Task 1).
- Produces (used by Task 4): `Palette` (static SwiftUI colors), `LoadState<T>` enum (`noWallet | failure | data(T)`), `PortfolioSummary` (availableMicro/valueMicro/unrealizedMicro/realizedMicro/lockedMicro/openOrders: all Double or Int, positions: [PositionCalc] sorted desc by value), `PositionCalc` (marketID/title/side/qty/entry/cur/valueMicro/unrealizedMicro/realizedMicro), `PricePoint` (date: Date, cents: Double), `WidgetLoader.loadPortfolio() async -> LoadState<PortfolioSummary>`, `WidgetLoader.loadActiveMarket() async -> LoadState<ActiveMarket>` where `ActiveMarket = (calc: PositionCalc, points: [PricePoint])`, and `Fmt.usd/signedUsd/cents/asOf` formatters.

- [ ] **Step 1: Write `Api.swift`** (complete file):

```swift
import SwiftUI

// MARK: - Palette (mobile/tailwind.config.js — keep in lockstep)

enum Palette {
    static let bg = Color(hex: 0x0A0A0B)
    static let ink = Color(hex: 0xF4F5F7)
    static let muted = Color(hex: 0x9297A0)
    static let dim = Color(hex: 0x565B63)
    static let line = Color(hex: 0x1B1C20)
    static let accent = Color(hex: 0x34D399)
    static let down = Color(hex: 0xF2637E)
}

extension Color {
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }
}

// MARK: - Wire DTOs (backend/internal/api JSON, snake_case; numbers as Double
// defensively — Go may emit int or float)

struct WirePortfolio: Decodable {
    struct Balance: Decodable { let usdc_available: Double? }
    struct Position: Decodable {
        let market_id: String
        let yes: Double?
        let no: Double?
        let avg_cost: Double?
        let realized: Double?
        let best_bid: Double?
    }
    struct Order: Decodable {
        let side: Double?      // 0 buy, 1 sell
        let price: Double?
        let remaining: Double?
        let status: String?
    }
    let balance: Balance?
    let positions: [Position]?
    let orders: [Order]?
}

struct WireMarkets: Decodable {
    struct Market: Decodable {
        let market_id: String
        let title: String?
    }
    let markets: [Market]?
}

struct WireFills: Decodable {
    struct Fill: Decodable {
        let price: Double?
        let ts: String?
    }
    let fills: [Fill]?
}

// MARK: - View models + PnL math (mirrors frontend/app/portfolio/page.tsx calc())

struct PositionCalc {
    let marketID: String
    var title: String
    let side: String      // "YES" | "NO"
    let qty: Double       // shares
    let entry: Double     // avg cost, cents (in held side's terms)
    let cur: Double       // exit mark (BBP), cents
    let valueMicro: Double
    let unrealizedMicro: Double
    let realizedMicro: Double
}

struct PortfolioSummary {
    let availableMicro: Double
    let valueMicro: Double
    let unrealizedMicro: Double
    let realizedMicro: Double
    let lockedMicro: Double
    let openOrders: Int
    let positions: [PositionCalc] // open only, sorted by valueMicro desc
}

struct PricePoint {
    let date: Date
    let cents: Double
}

typealias ActiveMarket = (calc: PositionCalc, points: [PricePoint])

enum LoadState<T> {
    case noWallet
    case failure
    case data(T)
}

func summarize(_ w: WirePortfolio, titles: [String: String]) -> PortfolioSummary {
    var positions: [PositionCalc] = []
    var realizedTotal = 0.0
    for p in w.positions ?? [] {
        let yes = p.yes ?? 0, no = p.no ?? 0
        realizedTotal += p.realized ?? 0
        guard yes > 0 || no > 0 else { continue }
        let side = yes > 0 ? "YES" : "NO"
        let qty = yes > 0 ? yes : no
        let avg = p.avg_cost ?? 0
        let bb = p.best_bid ?? 0
        let cur = bb > 0 ? (side == "YES" ? bb : 100 - bb) : avg
        let value = qty * cur * 10_000
        positions.append(PositionCalc(
            marketID: p.market_id,
            title: titles[p.market_id] ?? shortID(p.market_id),
            side: side,
            qty: qty,
            entry: avg,
            cur: cur,
            valueMicro: value,
            unrealizedMicro: value - qty * avg * 10_000,
            realizedMicro: p.realized ?? 0
        ))
    }
    positions.sort { $0.valueMicro > $1.valueMicro }
    let live = (w.orders ?? []).filter { $0.status == "live" }
    let locked = live
        .filter { ($0.side ?? 0) == 0 } // BUY soft-locks USDC
        .reduce(0.0) { $0 + ($1.remaining ?? 0) * ($1.price ?? 0) * 10_000 }
    return PortfolioSummary(
        availableMicro: w.balance?.usdc_available ?? 0,
        valueMicro: positions.reduce(0) { $0 + $1.valueMicro },
        unrealizedMicro: positions.reduce(0) { $0 + $1.unrealizedMicro },
        realizedMicro: realizedTotal,
        lockedMicro: locked,
        openOrders: live.count,
        positions: positions
    )
}

func shortID(_ hex: String) -> String {
    hex.count > 10 ? "\(hex.prefix(6))…\(hex.suffix(4))" : hex
}

// MARK: - Loader

struct WidgetLoader {
    static let appGroup = "group.com.pitchmarket.app"

    static func config() -> (wallet: String, apiURL: String)? {
        let d = UserDefaults(suiteName: appGroup)
        guard let w = d?.string(forKey: "wallet"), !w.isEmpty,
              let u = d?.string(forKey: "apiUrl"), !u.isEmpty
        else { return nil }
        return (w, u)
    }

    static func fetch<T: Decodable>(_ url: URL, as type: T.Type) async throws -> T {
        var req = URLRequest(url: url)
        req.timeoutInterval = 10
        let (data, resp) = try await URLSession.shared.data(for: req)
        guard let http = resp as? HTTPURLResponse, http.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    static func loadPortfolio() async -> LoadState<PortfolioSummary> {
        guard let cfg = config() else { return .noWallet }
        guard let pfURL = URL(string: "\(cfg.apiURL)/portfolio?wallet=\(cfg.wallet)"),
              let mkURL = URL(string: "\(cfg.apiURL)/markets")
        else { return .failure }
        do {
            let pf = try await fetch(pfURL, as: WirePortfolio.self)
            // Titles are a nice-to-have — portfolio still renders with ids.
            let titles = (try? await fetch(mkURL, as: WireMarkets.self))
                .flatMap { m -> [String: String]? in
                    var t: [String: String] = [:]
                    for mk in m.markets ?? [] { t[mk.market_id] = mk.title }
                    return t
                } ?? [:]
            return .data(summarize(pf, titles: titles))
        } catch {
            return .failure
        }
    }

    static func loadActiveMarket() async -> LoadState<ActiveMarket?> {
        switch await loadPortfolio() {
        case .noWallet: return .noWallet
        case .failure: return .failure
        case .data(let s):
            guard let top = s.positions.first else { return .data(nil) }
            guard let cfg = config(),
                  let url = URL(string: "\(cfg.apiURL)/markets/\(top.marketID)/fills")
            else { return .data((top, [])) }
            let wire = (try? await fetch(url, as: WireFills.self))?.fills ?? []
            let iso = ISO8601DateFormatter()
            iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            let isoPlain = ISO8601DateFormatter()
            var points: [PricePoint] = wire.compactMap { f in
                guard let p = f.price, let ts = f.ts,
                      let d = iso.date(from: ts) ?? isoPlain.date(from: ts)
                else { return nil }
                return PricePoint(date: d, cents: p) // fills are in YES terms
            }
            points.sort { $0.date < $1.date }
            return .data((top, points))
        }
    }
}

// MARK: - Formatting

enum Fmt {
    static func usd(_ micro: Double) -> String {
        let f = NumberFormatter()
        f.numberStyle = .currency
        f.currencyCode = "USD"
        f.maximumFractionDigits = 2
        return f.string(from: NSNumber(value: micro / 1_000_000)) ?? "$0.00"
    }
    static func signedUsd(_ micro: Double) -> String {
        (micro >= 0 ? "+" : "−") + usd(abs(micro))
    }
    static func cents(_ c: Double) -> String {
        "\(Int(c.rounded()))¢"
    }
    static func shares(_ q: Double) -> String {
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.maximumFractionDigits = 0
        return f.string(from: NSNumber(value: q)) ?? "0"
    }
    static func asOf(_ d: Date) -> String {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return "as of \(f.string(from: d))"
    }
}
```

Note: `loadActiveMarket` returns `LoadState<ActiveMarket?>` — `.data(nil)` is the "connected but flat" empty state.

- [ ] **Step 2: Commit**

```bash
git add mobile/targets/widget/Api.swift
git commit -m "feat(mobile): widget data layer — wire DTOs, PnL math, App Group loader"
```

(Compile verification deferred to Task 5 — no Swift toolchain check exists before prebuild.)

---

### Task 4: Swift widgets — providers, views, bundle

**Files:**
- Create: `mobile/targets/widget/Widgets.swift`

**Interfaces:**
- Consumes: everything in Task 3's Produces list.
- Produces: `@main PitchWidgetBundle` exposing `PortfolioWidget` (kind `PortfolioWidget`, `.systemMedium`) and `ActiveMarketWidget` (kind `ActiveMarketWidget`, `.systemLarge`).

- [ ] **Step 1: Write `Widgets.swift`** (complete file):

```swift
import WidgetKit
import SwiftUI

// MARK: - Timeline plumbing (shared shape for both widgets)

struct PortfolioEntry: TimelineEntry {
    let date: Date
    let state: LoadState<PortfolioSummary>
}

struct ActiveMarketEntry: TimelineEntry {
    let date: Date
    let state: LoadState<ActiveMarket?>
}

private func schedule(_ ok: Bool) -> Date {
    // 15 min on success, 5 min after a failure, per the design spec.
    Date().addingTimeInterval(ok ? 15 * 60 : 5 * 60)
}

struct PortfolioProvider: TimelineProvider {
    func placeholder(in _: Context) -> PortfolioEntry {
        PortfolioEntry(date: .now, state: .data(PortfolioSummary(
            availableMicro: 1_000_000_000, valueMicro: 250_000_000,
            unrealizedMicro: 12_500_000, realizedMicro: 4_200_000,
            lockedMicro: 50_000_000, openOrders: 3, positions: [])))
    }
    func getSnapshot(in _: Context, completion: @escaping (PortfolioEntry) -> Void) {
        Task { completion(PortfolioEntry(date: .now, state: await WidgetLoader.loadPortfolio())) }
    }
    func getTimeline(in _: Context, completion: @escaping (Timeline<PortfolioEntry>) -> Void) {
        Task {
            let state = await WidgetLoader.loadPortfolio()
            let ok = if case .failure = state { false } else { true }
            completion(Timeline(
                entries: [PortfolioEntry(date: .now, state: state)],
                policy: .after(schedule(ok))))
        }
    }
}

struct ActiveMarketProvider: TimelineProvider {
    func placeholder(in _: Context) -> ActiveMarketEntry {
        ActiveMarketEntry(date: .now, state: .data(nil))
    }
    func getSnapshot(in _: Context, completion: @escaping (ActiveMarketEntry) -> Void) {
        Task { completion(ActiveMarketEntry(date: .now, state: await WidgetLoader.loadActiveMarket())) }
    }
    func getTimeline(in _: Context, completion: @escaping (Timeline<ActiveMarketEntry>) -> Void) {
        Task {
            let state = await WidgetLoader.loadActiveMarket()
            let ok = if case .failure = state { false } else { true }
            completion(Timeline(
                entries: [ActiveMarketEntry(date: .now, state: state)],
                policy: .after(schedule(ok))))
        }
    }
}

// MARK: - Shared view bits

struct Eyebrow: View {
    let text: String
    var body: some View {
        Text(text.uppercased())
            .font(.system(size: 9, weight: .semibold))
            .tracking(1.1)
            .foregroundStyle(Palette.dim)
    }
}

struct StatCell: View {
    let label: String
    let value: String
    var tone: Color = Palette.ink
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Eyebrow(text: label)
            Text(value)
                .font(.system(size: 16, weight: .light, design: .monospaced))
                .foregroundStyle(tone)
                .minimumScaleFactor(0.6)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct CenteredNote: View {
    let title: String
    let sub: String
    var body: some View {
        VStack(spacing: 6) {
            Text(title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Palette.ink)
            Text(sub).font(.system(size: 11)).foregroundStyle(Palette.muted)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

func pnlColor(_ v: Double) -> Color { v >= 0 ? Palette.accent : Palette.down }

// MARK: - Portfolio widget (medium)

struct PortfolioView: View {
    let entry: PortfolioEntry
    var body: some View {
        switch entry.state {
        case .noWallet:
            CenteredNote(title: "PitchMarket", sub: "Open the app and connect a wallet")
        case .failure:
            CenteredNote(title: "Portfolio", sub: "Couldn’t reach the exchange")
        case .data(let s):
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .top) {
                    StatCell(label: "Available", value: Fmt.usd(s.availableMicro))
                    StatCell(label: "Value · BBP", value: Fmt.usd(s.valueMicro))
                }
                HStack(alignment: .top) {
                    StatCell(label: "Unrealised P&L",
                             value: Fmt.signedUsd(s.unrealizedMicro),
                             tone: pnlColor(s.unrealizedMicro))
                    StatCell(label: "Realised P&L",
                             value: Fmt.signedUsd(s.realizedMicro),
                             tone: pnlColor(s.realizedMicro))
                }
                Spacer(minLength: 0)
                HStack(spacing: 4) {
                    Text("\(s.openOrders) open order\(s.openOrders == 1 ? "" : "s")")
                    Text("·").foregroundStyle(Palette.dim)
                    Text("\(Fmt.usd(s.lockedMicro)) locked")
                    Spacer()
                    Text(Fmt.asOf(entry.date)).foregroundStyle(Palette.dim)
                }
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(Palette.muted)
            }
        }
    }
}

struct PortfolioWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "PortfolioWidget", provider: PortfolioProvider()) { entry in
            PortfolioView(entry: entry)
                .containerBackground(Palette.bg, for: .widget)
                .widgetURL(URL(string: "pitchmarket://portfolio"))
        }
        .configurationDisplayName("Portfolio")
        .description("Balance, position value, and P&L.")
        .supportedFamilies([.systemMedium])
    }
}

// MARK: - Price chart (recent fills + dashed entry line)

struct PriceChart: View {
    let points: [PricePoint]
    let entry: Double // cents
    let gain: Bool
    var body: some View {
        GeometryReader { geo in
            let cents = points.map(\.cents) + [entry]
            let lo = max(0, (cents.min() ?? 0) - 3)
            let hi = min(100, (cents.max() ?? 100) + 3)
            let span = max(hi - lo, 1)
            let y: (Double) -> CGFloat = { c in
                geo.size.height * (1 - CGFloat((c - lo) / span))
            }
            ZStack {
                // dashed entry reference
                Path { p in
                    p.move(to: CGPoint(x: 0, y: y(entry)))
                    p.addLine(to: CGPoint(x: geo.size.width, y: y(entry)))
                }
                .stroke(Palette.dim, style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                // price line
                if points.count > 1 {
                    Path { p in
                        for (i, pt) in points.enumerated() {
                            let x = geo.size.width * CGFloat(i) / CGFloat(points.count - 1)
                            let pos = CGPoint(x: x, y: y(pt.cents))
                            if i == 0 { p.move(to: pos) } else { p.addLine(to: pos) }
                        }
                    }
                    .stroke(gain ? Palette.accent : Palette.down,
                            style: StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
                } else {
                    Path { p in
                        let level = y(points.first?.cents ?? entry)
                        p.move(to: CGPoint(x: 0, y: level))
                        p.addLine(to: CGPoint(x: geo.size.width, y: level))
                    }
                    .stroke(gain ? Palette.accent : Palette.down, lineWidth: 1.5)
                }
            }
        }
    }
}

// MARK: - Active-market widget (large)

struct ActiveMarketView: View {
    let entry: ActiveMarketEntry
    var body: some View {
        switch entry.state {
        case .noWallet:
            CenteredNote(title: "PitchMarket", sub: "Open the app and connect a wallet")
        case .failure:
            CenteredNote(title: "Active market", sub: "Couldn’t reach the exchange")
        case .data(nil):
            CenteredNote(title: "No open positions", sub: "Your biggest position shows up here")
        case .data(let am?):
            let c = am.calc
            let gain = c.unrealizedMicro >= 0
            VStack(alignment: .leading, spacing: 8) {
                // header
                HStack(alignment: .top) {
                    Text(c.title)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Palette.ink)
                        .lineLimit(2)
                    Spacer()
                    Text("\(c.side) · \(Fmt.shares(c.qty))")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(c.side == "YES" ? Palette.accent : Palette.down)
                        .padding(.horizontal, 6).padding(.vertical, 3)
                        .background(Palette.line, in: RoundedRectangle(cornerRadius: 4))
                }
                // price block
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(Fmt.cents(c.cur))
                        .font(.system(size: 34, weight: .light, design: .monospaced))
                        .foregroundStyle(Palette.ink)
                    Text("bought \(Fmt.cents(c.entry))")
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(Palette.muted)
                }
                // pnl row
                HStack(spacing: 10) {
                    Text(Fmt.signedUsd(c.unrealizedMicro))
                        .font(.system(size: 15, design: .monospaced))
                        .foregroundStyle(pnlColor(c.unrealizedMicro))
                    Eyebrow(text: "Unrealised")
                    if c.realizedMicro != 0 {
                        Text(Fmt.signedUsd(c.realizedMicro))
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(pnlColor(c.realizedMicro))
                        Eyebrow(text: "Realised")
                    }
                    Spacer()
                }
                PriceChart(points: am.points, entry: c.entry, gain: gain)
                    .frame(maxHeight: .infinity)
                Text(Fmt.asOf(entry.date))
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(Palette.dim)
            }
            .widgetURL(URL(string: "pitchmarket://market/\(c.marketID)"))
        }
    }
}

struct ActiveMarketWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "ActiveMarketWidget", provider: ActiveMarketProvider()) { entry in
            ActiveMarketView(entry: entry)
                .containerBackground(Palette.bg, for: .widget)
        }
        .configurationDisplayName("Active market")
        .description("Your biggest position — price, entry, P&L, and chart.")
        .supportedFamilies([.systemLarge])
    }
}

// MARK: - Bundle

@main
struct PitchWidgetBundle: WidgetBundle {
    var body: some Widget {
        PortfolioWidget()
        ActiveMarketWidget()
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add mobile/targets/widget/Widgets.swift
git commit -m "feat(mobile): portfolio + active-market WidgetKit widgets"
```

---

### Task 5: Prebuild, simulator verification, progress.md

**Files:**
- Modify: `progress.md` (component row + changelog)
- Possibly modify: any of the above, fixing compile errors surfaced by the real build

- [ ] **Step 1: Regenerate the native project with the widget target**

Run: `cd mobile && npx expo prebuild -p ios --clean`
Expected: succeeds; `ios/` contains a `widget` extension target (check: `grep -r "com.pitchmarket.app.widget" ios/*.xcodeproj/project.pbxproj | head -1`).

- [ ] **Step 2: Build + boot in the simulator**

Run: `cd mobile && npx expo run:ios` (background; boots the iPhone simulator dev client).
Expected: app compiles **including the Swift widget target** and launches. Fix any Swift compile errors here (this is the first real Swift compile) and amend the Task 3/4 commits or add a fix commit.

- [ ] **Step 3: Manual/simulator widget check**

With a live backend (`go run ./cmd/server` with `DEMO_FIXTURE`, `EXPO_PUBLIC_API_URL` pointed at it):
1. In the app: connect the demo wallet, deposit, place an order so a position exists.
2. Home screen → long-press → add both PitchMarket widgets (Portfolio medium, Active market large).
3. Confirm: placeholder → real data after the app pushed the App Group state; PnL/labels match the in-app portfolio; tapping deep-links to the right screens.

Simulator-verified items become ✅-on-simulator; physical-device pinning stays a human pass, noted 🟡 in progress.md.

- [ ] **Step 4: Regression gates**

Run: `cd mobile && npx tsc --noEmit && npm test && npm run check-borsh`
Expected: all green.

- [ ] **Step 5: progress.md + final commit**

Update `progress.md`: mobile row mentions the widgets, append a changelog row stating exactly what was verified (simulator build/pin) and what wasn't (physical device).

```bash
git add progress.md
git commit -m "docs: progress — iOS widgets built and simulator-verified"
```

---

## Self-review

- **Spec coverage:** bridge write on wallet change ✓ (T1); plugin/App Group/team-id ✓ (T2); DTO decode + PnL/locked/orders-count math ✓ (T3); medium portfolio + large active-market views, chart with dashed entry line, deep links, empty/error states, 15/5-min policy ✓ (T4); prebuild + simulator verification + progress.md ✓ (T5).
- **Placeholder scan:** none — all code complete.
- **Type consistency:** `LoadState<ActiveMarket?>` naming consistent between T3/T4; `Fmt`/`Palette`/`WidgetLoader` referenced as defined; `syncWidgetState(wallet: string | null)` matches call site.
