# PitchMarket Mobile App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An Expo/React Native consumer app at `mobile/` covering the core trading loop (markets → market page + trade → deposit → portfolio) against the existing, unchanged Go backend.

**Architecture:** Standalone Expo app with `expo-router` tabs and NativeWind styling using the web app's design tokens. The pure-TS lib files (`borsh.ts`, `types.ts`, `format.ts`) are copied verbatim from `frontend/lib/` and pinned by the same borsh golden vector; `api.ts` and `useLiveMarket.ts` are ports. Wallet is a context with the same `PitchWallet` interface as web — a SecureStore demo wallet first (works in Expo Go), Privy Expo layered on afterwards (requires a dev-client build).

**Tech Stack:** Expo SDK (latest, ≥53), expo-router, TypeScript, NativeWind v4, tweetnacl, bs58, react-native-get-random-values, expo-secure-store, jest-expo, `@privy-io/expo` (Task 11 only).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-16-mobile-app-design.md`. Scope is the core trading loop ONLY — no combos, no precision, no admin.
- **Zero backend changes.** The Go API/WS surface is frozen and verified; the mobile app adapts to it, never the reverse.
- **Borsh bytes are sacred.** `mobile/lib/borsh.ts` must stay byte-identical to `frontend/lib/borsh.ts` / `backend/internal/models/hash.go` / `sig_verify.rs`. The golden vector (`scripts/check-borsh.mjs`) is copied in and wired as a test + pre-start gate. Never "improve" the encoder.
- Money is integer micro-USDC (1 USDC = 1_000_000 micro; 1 cent-share = 10_000 micro). Prices are integer cents 1..99. Ids/hashes are hex; wallet addresses are base58.
- Env: `EXPO_PUBLIC_API_URL` (http) — WS URL is derived (`http→ws` + `/ws`), same as web. On a physical device this must be the machine's LAN IP, not `localhost`.
- **CLAUDE.md rule:** every commit that changes code must update `progress.md` in the same commit (append a Changelog row; keep the `mobile/` component row honest — 🟡 until an on-device click-through, per the working agreement). Steps below say "commit"; the progress.md append is implied in every one.
- Commits: `type(scope): summary`, no Co-Authored-By / attribution trailers.
- All commands run from `mobile/` unless a path is shown.
- Status honesty: nothing gets ✅ in progress.md until it ran on a device/simulator against the live backend.

## File Structure

```
mobile/
  app.json / app.config.ts        Expo config (name PitchMarket, scheme pitchmarket)
  babel.config.js, metro.config.js, tailwind.config.js, global.css   NativeWind
  app/_layout.tsx                 polyfill import, WalletProvider, stack shell
  app/(tabs)/_layout.tsx          tab bar: Markets, Portfolio
  app/(tabs)/index.tsx            markets index screen
  app/(tabs)/portfolio.tsx        portfolio screen
  app/market/[id].tsx             market screen (hero, ladder, fills, trade button)
  components/MatchHeader.tsx      fixture, score, minute, status badge
  components/Ladder.tsx           unified YES ladder (asks/spread/bids)
  components/TradeSheet.tsx       modal trade panel: side/price/size/sign/post
  components/DepositButton.tsx    two-step cosigned deposit (mirror fallback)
  lib/borsh.ts                    COPY VERBATIM from frontend/lib/borsh.ts
  lib/types.ts                    COPY VERBATIM from frontend/lib/types.ts
  lib/format.ts                   COPY VERBATIM from frontend/lib/format.ts
  lib/base64.ts                   b64→bytes (no atob dependence on Hermes)
  lib/api.ts                      port of frontend/lib/api.ts (core endpoints only)
  lib/errors.ts                   ApiError→human message mapping
  lib/wallet.tsx                  PitchWallet context: SecureStore demo + Privy (T11)
  lib/useLiveMarket.ts            port of frontend/lib/useLiveMarket.ts + AppState refetch
  lib/__tests__/                  jest-expo unit tests (borsh vector, mapBook, wallet)
  scripts/check-borsh.mjs         COPY VERBATIM from frontend/scripts/check-borsh.mjs
  scripts/e2e-flow.ts             lib-level e2e vs the live backend (tsx)
```

---

### Task 1: Scaffold the Expo app with NativeWind and the design tokens

**Files:**
- Create: `mobile/` via create-expo-app, then `mobile/tailwind.config.js`, `mobile/global.css`, `mobile/babel.config.js`, `mobile/metro.config.js`, `mobile/nativewind-env.d.ts`
- Modify: `mobile/package.json` (scripts), `mobile/app.json` (name/scheme)

**Interfaces:**
- Produces: a booting Expo Go app with NativeWind classes working and the token palette (`bg`, `ink`, `muted`, `dim`, `line`, `line2`, `accent`, `down`) available to every later task.

- [ ] **Step 1: Scaffold**

Run from the repo root:
```bash
npx create-expo-app@latest mobile
cd mobile && npm run reset-project   # strips example screens; keeps expo-router
rm -rf app-example
```
(If `reset-project` isn't in the generated package.json, just delete the example screens under `app/` leaving `app/_layout.tsx` and `app/index.tsx`.)

In `app.json` set `"name": "PitchMarket"`, `"slug": "pitchmarket"`, `"scheme": "pitchmarket"`.

- [ ] **Step 2: Install and configure NativeWind**

```bash
npm i nativewind tailwindcss@^3.4.17
npx expo install react-native-reanimated react-native-safe-area-context
```

`tailwind.config.js` — the web tokens, verbatim colors from `frontend/tailwind.config.ts`:
```js
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  presets: [require("nativewind/preset")],
  theme: {
    extend: {
      colors: {
        bg: "#0a0a0b",
        ink: "#f4f5f7",
        muted: "#9297a0",
        dim: "#565b63",
        line: "#1b1c20",
        line2: "#292b30",
        accent: "#34d399",
        down: "#f2637e",
      },
    },
  },
  plugins: [],
};
```

`global.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

`babel.config.js`:
```js
module.exports = function (api) {
  api.cache(true);
  return {
    presets: [
      ["babel-preset-expo", { jsxImportSource: "nativewind" }],
      "nativewind/babel",
    ],
  };
};
```

`metro.config.js`:
```js
const { getDefaultConfig } = require("expo/metro-config");
const { withNativeWind } = require("nativewind/metro");
const config = getDefaultConfig(__dirname);
module.exports = withNativeWind(config, { input: "./global.css" });
```

`nativewind-env.d.ts`:
```ts
/// <reference types="nativewind/types" />
```

- [ ] **Step 3: Minimal root layout proving the tokens**

`app/_layout.tsx`:
```tsx
import "../global.css";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";

export default function RootLayout() {
  return (
    <>
      <StatusBar style="light" />
      <Stack
        screenOptions={{
          headerShown: false,
          contentStyle: { backgroundColor: "#0a0a0b" },
        }}
      />
    </>
  );
}
```

`app/index.tsx` (temporary — replaced by tabs in Task 6):
```tsx
import { Text, View } from "react-native";

export default function Index() {
  return (
    <View className="flex-1 items-center justify-center bg-bg">
      <Text className="text-ink text-lg font-semibold">PitchMarket</Text>
      <Text className="text-accent mt-2">tokens ok</Text>
    </View>
  );
}
```

- [ ] **Step 4: Verify it boots**

```bash
npx tsc --noEmit
npx expo start
```
Expected: typecheck clean; in Expo Go (simulator or phone) the screen shows near-black background, "PitchMarket" in near-white, "tokens ok" in green. If the green text is unstyled, NativeWind config is wrong — fix before committing.

- [ ] **Step 5: Commit**

```bash
git add mobile ../progress.md
git commit -m "feat(mobile): scaffold Expo app with NativeWind + design tokens"
```

---

### Task 2: Copy the pure libs and pin the borsh golden vector

**Files:**
- Create: `mobile/lib/borsh.ts`, `mobile/lib/types.ts`, `mobile/lib/format.ts` (all verbatim copies), `mobile/lib/base64.ts`, `mobile/scripts/check-borsh.mjs` (verbatim copy), `mobile/lib/__tests__/borsh.test.ts`
- Modify: `mobile/package.json` (deps, jest preset, scripts), `mobile/app/_layout.tsx` (polyfill import)

**Interfaces:**
- Produces: `borshOrder(o: OrderMsg): Uint8Array`, `randomSalt(): bigint`, `toHex`/`fromHex` (from borsh.ts); all types from `types.ts`; `usd/usdBare/cents/prob/shares/shortHash/buyCostMicro/maxPayoutMicro` (from format.ts); `b64ToBytes(b64: string): Uint8Array` (base64.ts). Later tasks import exactly these names.

- [ ] **Step 1: Install runtime deps and the RNG polyfill**

```bash
npm i tweetnacl bs58 react-native-get-random-values
npx expo install expo-secure-store
```

Add as the FIRST line of `app/_layout.tsx` (before any other import — `randomSalt` and tweetnacl need `crypto.getRandomValues` on Hermes):
```tsx
import "react-native-get-random-values";
```

- [ ] **Step 2: Copy the three pure files verbatim**

```bash
cp ../frontend/lib/borsh.ts ../frontend/lib/types.ts ../frontend/lib/format.ts lib/
mkdir -p scripts && cp ../frontend/scripts/check-borsh.mjs scripts/
```
Do not edit them. Any future change must be mirrored in `frontend/lib/` and re-checked against the vector on both sides.

- [ ] **Step 3: Write `lib/base64.ts`** (Hermes `atob` availability varies; don't depend on it)

```ts
const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
const LOOKUP = new Uint8Array(128);
for (let i = 0; i < ALPHABET.length; i++) LOOKUP[ALPHABET.charCodeAt(i)] = i;

/** Decode standard base64 (with optional padding) to bytes. */
export function b64ToBytes(b64: string): Uint8Array {
  const clean = b64.replace(/=+$/, "");
  const out = new Uint8Array(Math.floor((clean.length * 3) / 4));
  let o = 0;
  for (let i = 0; i + 1 < clean.length; i += 4) {
    const a = LOOKUP[clean.charCodeAt(i)];
    const b = LOOKUP[clean.charCodeAt(i + 1)];
    const c = i + 2 < clean.length ? LOOKUP[clean.charCodeAt(i + 2)] : 0;
    const d = i + 3 < clean.length ? LOOKUP[clean.charCodeAt(i + 3)] : 0;
    out[o++] = (a << 2) | (b >> 4);
    if (i + 2 < clean.length) out[o++] = ((b & 15) << 4) | (c >> 2);
    if (i + 3 < clean.length) out[o++] = ((c & 3) << 6) | d;
  }
  return out;
}
```

- [ ] **Step 4: Set up jest-expo and write the failing test**

```bash
npx expo install jest-expo jest @types/jest -- --save-dev
```
In `package.json` add:
```json
"scripts": { "test": "jest", "check-borsh": "node scripts/check-borsh.mjs" },
"jest": { "preset": "jest-expo" }
```

`lib/__tests__/borsh.test.ts`:
```ts
import { borshOrder, randomSalt, toHex } from "../borsh";
import { b64ToBytes } from "../base64";

// Same golden vector as scripts/check-borsh.mjs, hash_conformance_test.go,
// sig_verify.rs tests, and frontend/scripts/check-borsh.mjs.
const GOLDEN =
  "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
  "2122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f40" +
  "01003d0040420f0000000000000000f1536500000000efbeadde00000000";

test("borshOrder matches the pinned golden vector", () => {
  const maker = new Uint8Array(32).map((_, i) => i + 1);
  const marketId = new Uint8Array(32).map((_, i) => i + 33);
  const bytes = borshOrder({
    maker, marketId,
    outcome: 1, side: 0, price: 61,
    size: 1_000_000n, feeBps: 0,
    expiry: 1_700_000_000n, salt: 0xdeadbeefn,
  });
  expect(bytes.length).toBe(94);
  expect(toHex(bytes)).toBe(GOLDEN);
});

test("randomSalt clears the top bit (signed BIGINT column)", () => {
  for (let i = 0; i < 32; i++) expect(randomSalt() < 2n ** 63n).toBe(true);
});

test("b64ToBytes decodes standard base64", () => {
  expect(Array.from(b64ToBytes("AQIDBA=="))).toEqual([1, 2, 3, 4]);
  expect(Array.from(b64ToBytes("aGVsbG8="))).toEqual([104, 101, 108, 108, 111]);
});
```

- [ ] **Step 5: Run tests + the vector gate**

```bash
npm test
npm run check-borsh
```
Expected: 3 tests pass; `borsh golden vector ok (94 bytes)`. (If the borsh test fails, the copy step went wrong — the encoder is proven on web.)

- [ ] **Step 6: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): pure libs copied from web, borsh golden vector gated"
```

---

### Task 3: Port `api.ts` (core endpoints only)

**Files:**
- Create: `mobile/lib/api.ts`, `mobile/lib/__tests__/api.test.ts`

**Interfaces:**
- Consumes: types from `lib/types.ts`.
- Produces: `configured(): boolean`, `wsUrl(): string`, `class ApiError { status: number }`, `mapBook(w: WireBook): Book`, `mapMatch(w: WireMatch): Match`, and `api.{listMatches, listMarkets, getMarket, getMatch, getBook, getFills, getOneliners, getBalance, getPortfolio, postOrder, cancelOrder, depositInit, depositComplete, depositMirror}` — signatures identical to `frontend/lib/api.ts`.

- [ ] **Step 1: Write the port**

Copy `frontend/lib/api.ts` to `mobile/lib/api.ts`, then make exactly these changes:

1. `const BASE = process.env.EXPO_PUBLIC_API_URL ?? "";` (was `NEXT_PUBLIC_API_URL`; also update the two error-message strings that name the env var).
2. In `get()`, drop the `{ cache: "no-store" }` fetch option (Next-ism; RN fetch doesn't cache).
3. Delete out-of-scope code: `getSettlement`, `enterPrecision`, `leaderboard`, `createRFQ`, `getRFQ`, `acceptQuote`, and the `PrecisionEntry`/`RFQQuote`/`RFQ` interfaces. Remove `Settlement` from the type import.
4. Keep everything else byte-for-byte: `mapMarket`, `mapMatch`, `mapBook` (the YES-ladder unification is load-bearing), `mapFill`, `getPortfolio`'s title join and filters, `postOrder`, `cancelOrder`, the three deposit calls, `PROGRAM_ID`, `DEPLOY_TX`, `explorerTx`/`explorerAddr`.

- [ ] **Step 2: Write the mapBook test**

`lib/__tests__/api.test.ts`:
```ts
import { mapBook } from "../api";

test("mapBook unifies the outcome-indexed book into one YES ladder", () => {
  // Go book: [0]=NO, [1]=YES. YES bids = BUY YES ∪ complement(SELL NO);
  // YES asks = SELL YES ∪ complement(BUY NO).
  const book = mapBook({
    bids: [
      [{ price: 40, size: 10 }], // BUY NO 40 → YES ask at 60
      [{ price: 55, size: 5 }],  // BUY YES 55 → YES bid 55
    ],
    asks: [
      [{ price: 45, size: 7 }],  // SELL NO 45 → YES bid at 55 (merges with above)
      [{ price: 61, size: 3 }],  // SELL YES 61 → YES ask 61
    ],
  });
  expect(book.bids).toEqual([{ price: 55, size: 12 }]); // merged 5 + 7
  expect(book.asks).toEqual([
    { price: 60, size: 10 },
    { price: 61, size: 3 },
  ]); // ascending
});

test("mapBook tolerates missing sides", () => {
  const book = mapBook({ bids: [[], []], asks: [[], []] });
  expect(book).toEqual({ bids: [], asks: [] });
});
```

- [ ] **Step 3: Run**

```bash
npm test && npx tsc --noEmit
```
Expected: all tests pass, typecheck clean.

- [ ] **Step 4: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): port api client (core endpoints, unified YES ladder)"
```

---

### Task 4: Wallet context — SecureStore demo wallet

**Files:**
- Create: `mobile/lib/wallet.tsx`, `mobile/lib/__tests__/wallet.test.ts`
- Modify: `mobile/app/_layout.tsx` (wrap in provider)

**Interfaces:**
- Consumes: `toHex`/`fromHex` from `lib/borsh.ts`.
- Produces:
```ts
export interface PitchWallet {
  ready: boolean;
  address: string | null;      // base58 pubkey
  isDemo: boolean;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  signMessage: (message: Uint8Array) => Promise<Uint8Array>;
}
export function usePitchWallet(): PitchWallet;
export function PitchWalletProvider(props: { children: React.ReactNode }): JSX.Element;
```
(Note: `connect`/`disconnect` are async here, unlike web — SecureStore is async. Task 11 keeps this interface for Privy.)

- [ ] **Step 1: Write the failing test** (pure signing logic, extracted so jest needn't render RN)

`lib/__tests__/wallet.test.ts`:
```ts
import nacl from "tweetnacl";
import bs58 from "bs58";
import { keypairFromSeed } from "../wallet";

test("seed-derived keypair signs verifiably (same scheme the backend checks)", () => {
  const seed = new Uint8Array(32).map((_, i) => i * 7 % 251);
  const kp = keypairFromSeed(seed);
  const msg = new Uint8Array([1, 2, 3, 4]);
  const sig = nacl.sign.detached(msg, kp.secretKey);
  expect(nacl.sign.detached.verify(msg, sig, kp.publicKey)).toBe(true);
  expect(bs58.decode(bs58.encode(kp.publicKey))).toEqual(kp.publicKey);
});
```

Run: `npm test` — Expected: FAIL (`keypairFromSeed` not exported).

- [ ] **Step 2: Write `lib/wallet.tsx`**

```tsx
// Wallet layer with the same PitchWallet interface as frontend/lib/wallet.tsx.
// Demo backend: an ed25519 keypair persisted in expo-secure-store. The backend
// accepts any well-signed order, so this exercises the REAL exchange with zero
// credentials. Privy Expo (real product path) is added behind
// EXPO_PUBLIC_PRIVY_APP_ID in a later task; both sign the same borsh bytes.
import {
  createContext, useCallback, useContext, useEffect, useMemo, useState,
} from "react";
import * as SecureStore from "expo-secure-store";
import nacl from "tweetnacl";
import bs58 from "bs58";
import { fromHex, toHex } from "./borsh";

export interface PitchWallet {
  ready: boolean;
  address: string | null;
  isDemo: boolean;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  signMessage: (message: Uint8Array) => Promise<Uint8Array>;
}

export function keypairFromSeed(seed: Uint8Array): nacl.SignKeyPair {
  return nacl.sign.keyPair.fromSeed(seed);
}

const WalletCtx = createContext<PitchWallet | null>(null);

export function usePitchWallet(): PitchWallet {
  const ctx = useContext(WalletCtx);
  if (!ctx) throw new Error("usePitchWallet outside <PitchWalletProvider>");
  return ctx;
}

const SEED_KEY = "pm_demo_wallet_seed";

export function PitchWalletProvider({ children }: { children: React.ReactNode }) {
  return <DemoWalletProvider>{children}</DemoWalletProvider>;
}

function DemoWalletProvider({ children }: { children: React.ReactNode }) {
  const [keys, setKeys] = useState<nacl.SignKeyPair | null>(null);
  const [ready, setReady] = useState(false);

  // Restore a previous session's wallet; creation is explicit via connect().
  useEffect(() => {
    (async () => {
      const existing = await SecureStore.getItemAsync(SEED_KEY);
      if (existing && existing.length === 64) setKeys(keypairFromSeed(fromHex(existing)));
      setReady(true);
    })();
  }, []);

  const connect = useCallback(async () => {
    let hex = await SecureStore.getItemAsync(SEED_KEY);
    if (!hex || hex.length !== 64) {
      const seed = new Uint8Array(32);
      crypto.getRandomValues(seed);
      hex = toHex(seed);
      await SecureStore.setItemAsync(SEED_KEY, hex);
    }
    setKeys(keypairFromSeed(fromHex(hex)));
  }, []);

  const disconnect = useCallback(async () => {
    await SecureStore.deleteItemAsync(SEED_KEY);
    setKeys(null);
  }, []);

  const value = useMemo<PitchWallet>(
    () => ({
      ready,
      address: keys ? bs58.encode(keys.publicKey) : null,
      isDemo: true,
      connect,
      disconnect,
      signMessage: async (message) => {
        if (!keys) throw new Error("wallet not connected");
        return nacl.sign.detached(message, keys.secretKey);
      },
    }),
    [ready, keys, connect, disconnect]
  );
  return <WalletCtx.Provider value={value}>{children}</WalletCtx.Provider>;
}
```

Wrap the app — `app/_layout.tsx` returns:
```tsx
<PitchWalletProvider>
  <StatusBar style="light" />
  <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: "#0a0a0b" } }} />
</PitchWalletProvider>
```

- [ ] **Step 3: Run tests + typecheck**

```bash
npm test && npx tsc --noEmit
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): PitchWallet context with SecureStore demo wallet"
```

---

### Task 5: Port `useLiveMarket` with foreground refetch

**Files:**
- Create: `mobile/lib/useLiveMarket.ts`

**Interfaces:**
- Consumes: `api`, `ApiError`, `configured`, `mapBook`, `mapMatch`, `wsUrl`, `WireBook` from `lib/api.ts`; types from `lib/types.ts`.
- Produces: `useLiveMarket(marketId: string, wallet: string | null): LiveMarket` with the exact `LiveMarket` shape from `frontend/lib/useLiveMarket.ts` (loading, errorStatus, market, match, book, fills, history, oneliners, onelinerIdx, yesPrice, priceDelta, lastFillId, lastFillSide, balanceMicro, refreshBalance).

- [ ] **Step 1: Port the hook**

Copy `frontend/lib/useLiveMarket.ts` to `mobile/lib/useLiveMarket.ts` and make exactly these changes:

1. Delete the `"use client"` directive.
2. Add a foreground-refetch: mobile OSes freeze JS and kill sockets in the background, so on return to `active` the snapshot may be stale. Add a `refreshKey` state and an AppState listener, and include `refreshKey` in BOTH data effects' dependency arrays (initial load and WS effect):

```ts
import { AppState } from "react-native";
// inside useLiveMarket, alongside the other state:
const [refreshKey, setRefreshKey] = useState(0);
useEffect(() => {
  const sub = AppState.addEventListener("change", (s) => {
    if (s === "active") setRefreshKey((k) => k + 1);
  });
  return () => sub.remove();
}, []);
// initial-load effect deps: [marketId, wallet, refreshKey]
// WS effect deps: [marketId, state.loading, state.errorStatus, refreshKey]
```
Everything else (event handling for `book_update`/`fill`/`oneliner`/`match_state`, reconnect backoff, one-liner rotation) stays identical — RN's global `WebSocket` has the same API surface the hook uses (`onopen`/`onmessage`/`onclose`).

- [ ] **Step 2: Verify**

```bash
npx tsc --noEmit && npm test
```
Expected: clean. (Behavioral verification happens on-device in Task 7 — a hook driving live WS state has no meaningful jest test without mocking away everything it does.)

- [ ] **Step 3: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): port useLiveMarket with AppState foreground refetch"
```

---

### Task 6: Tabs shell + Markets index screen

**Files:**
- Create: `mobile/app/(tabs)/_layout.tsx`, `mobile/app/(tabs)/index.tsx`, `mobile/app/(tabs)/portfolio.tsx` (stub), `mobile/.env` (gitignored) with `EXPO_PUBLIC_API_URL`
- Delete: `mobile/app/index.tsx` (Task 1 placeholder)
- Modify: `mobile/.gitignore` (ensure `.env` ignored)

**Interfaces:**
- Consumes: `api.listMarkets`, `api.listMatches`, `api.getBook`, `configured` from `lib/api.ts`; `cents` from `lib/format.ts`.
- Produces: route `/(tabs)` (Markets, Portfolio tabs); market cards navigate to `/market/[id]` (Task 7's route). Portfolio tab is a placeholder replaced in Task 10.

- [ ] **Step 1: Tab layout**

`app/(tabs)/_layout.tsx`:
```tsx
import { Tabs } from "expo-router";
import { Ionicons } from "@expo/vector-icons";

export default function TabLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarStyle: { backgroundColor: "#0a0a0b", borderTopColor: "#1b1c20" },
        tabBarActiveTintColor: "#34d399",
        tabBarInactiveTintColor: "#565b63",
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: "Markets",
          tabBarIcon: ({ color, size }) => <Ionicons name="list" size={size} color={color} />,
        }}
      />
      <Tabs.Screen
        name="portfolio"
        options={{
          title: "Portfolio",
          tabBarIcon: ({ color, size }) => <Ionicons name="wallet" size={size} color={color} />,
        }}
      />
    </Tabs>
  );
}
```

`app/(tabs)/portfolio.tsx` stub (replaced in Task 10):
```tsx
import { Text, View } from "react-native";
export default function Portfolio() {
  return (
    <View className="flex-1 items-center justify-center bg-bg">
      <Text className="text-dim">Portfolio — coming in Task 10</Text>
    </View>
  );
}
```

- [ ] **Step 2: Markets index screen**

`app/(tabs)/index.tsx`:
```tsx
import { useCallback, useEffect, useState } from "react";
import { FlatList, Pressable, RefreshControl, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { api, configured } from "@/lib/api";
import { cents } from "@/lib/format";
import type { Market, Match } from "@/lib/types";

interface Row {
  market: Market;
  match: Match | null;
  yesPrice: number | null; // mid of the unified ladder, null if empty book
}

function midOf(bids: { price: number }[], asks: { price: number }[]): number | null {
  if (bids[0] && asks[0]) return Math.round((bids[0].price + asks[0].price) / 2);
  return bids[0]?.price ?? asks[0]?.price ?? null;
}

export default function Markets() {
  const [rows, setRows] = useState<Row[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!configured()) {
      setError("EXPO_PUBLIC_API_URL is not set");
      setLoading(false);
      return;
    }
    try {
      const [markets, matches] = await Promise.all([api.listMarkets("open"), api.listMatches()]);
      const byId = new Map(matches.map((m) => [m.id, m]));
      const binary = markets.filter((m) => m.type === "binary");
      const books = await Promise.all(
        binary.map((m) => api.getBook(m.market_id).catch(() => ({ bids: [], asks: [] })))
      );
      setRows(
        binary.map((market, i) => ({
          market,
          match: byId.get(market.match_id) ?? null,
          yesPrice: midOf(books[i].bids, books[i].asks),
        }))
      );
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load markets");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 20_000);
    return () => clearInterval(t);
  }, [load]);

  return (
    <SafeAreaView className="flex-1 bg-bg" edges={["top"]}>
      <View className="px-4 pb-3 pt-2 border-b border-line">
        <Text className="text-ink text-xl font-bold">PitchMarket</Text>
        <Text className="text-dim text-xs mt-0.5">Football prediction exchange · devnet</Text>
      </View>
      {error ? (
        <View className="flex-1 items-center justify-center px-8">
          <Text className="text-down text-center">{error}</Text>
        </View>
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(r) => r.market.id}
          refreshControl={
            <RefreshControl refreshing={loading} onRefresh={load} tintColor="#34d399" />
          }
          ListEmptyComponent={
            loading ? null : (
              <Text className="text-dim text-center mt-16">No open markets right now.</Text>
            )
          }
          renderItem={({ item }) => (
            <Pressable
              onPress={() => router.push(`/market/${item.market.market_id}`)}
              className="px-4 py-3.5 border-b border-line active:bg-line"
            >
              <View className="flex-row items-center justify-between">
                <View className="flex-1 pr-3">
                  {item.match && (
                    <Text className="text-dim text-[11px] mb-0.5">
                      {item.match.home} vs {item.match.away}
                      {item.match.status === "live" &&
                        `  ·  ${item.match.live_state.home_score}–${item.match.live_state.away_score}${
                          item.match.live_state.minute ? ` ${item.match.live_state.minute}'` : ""
                        }`}
                    </Text>
                  )}
                  <Text className="text-ink text-[15px] font-medium" numberOfLines={2}>
                    {item.market.title}
                  </Text>
                </View>
                <View className="items-end">
                  <Text className="text-accent text-lg font-bold">
                    {item.yesPrice !== null ? cents(item.yesPrice) : "—"}
                  </Text>
                  <Text className="text-dim text-[10px]">YES</Text>
                </View>
              </View>
            </Pressable>
          )}
        />
      )}
    </SafeAreaView>
  );
}
```
Also delete `app/index.tsx` and confirm `@/*` path alias exists in `tsconfig.json` (Expo's default template has `"paths": { "@/*": ["./*"] }` — add it if missing).

- [ ] **Step 3: Point at the live backend and verify on device**

Create `mobile/.env` (confirm `.env` is in `mobile/.gitignore`; add it if not):
```
EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080
```
(Use the port the Go server actually runs on — check the repo-root `.env` / `cmd/server` config. `localhost` only works on a simulator on the same machine.)

```bash
npx tsc --noEmit
npx expo start
```
Expected: Markets tab lists the live markets with YES mid prices; pull-to-refresh works; tapping a card navigates (to a 404/blank route until Task 7 — that's fine).

- [ ] **Step 4: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): tabs shell + live markets index"
```

---

### Task 7: Market screen — hero, ladder, one-liners, fills

**Files:**
- Create: `mobile/app/market/[id].tsx`, `mobile/components/MatchHeader.tsx`, `mobile/components/Ladder.tsx`

**Interfaces:**
- Consumes: `useLiveMarket` (Task 5), `usePitchWallet` (Task 4), `cents`, `shares`, `shortHash` from `lib/format.ts`.
- Produces: route `/market/[id]` where `id` is the 64-hex `market_id`; a "Trade" button whose `onPress` opens Task 8's `TradeSheet` (this task renders the button disabled-with-alert until Task 8 replaces the stub import).

- [ ] **Step 1: `components/MatchHeader.tsx`**

```tsx
import { Text, View } from "react-native";
import type { Match } from "@/lib/types";

export function MatchHeader({ match }: { match: Match | null }) {
  if (!match) return null;
  const live = match.status === "live";
  const s = match.live_state;
  return (
    <View className="flex-row items-center justify-between px-4 py-3 border-b border-line">
      <Text className="text-ink text-[15px] font-semibold flex-1" numberOfLines={1}>
        {match.home} <Text className="text-dim">vs</Text> {match.away}
      </Text>
      <View className="flex-row items-center">
        {(live || match.status === "ft") && (
          <Text className="text-ink text-[15px] font-bold mr-2">
            {s.home_score}–{s.away_score}
          </Text>
        )}
        {live ? (
          <View className="flex-row items-center">
            <View className="h-1.5 w-1.5 rounded-full bg-accent mr-1" />
            <Text className="text-accent text-[11px] font-semibold">
              {s.minute ? `${s.minute}'` : "LIVE"}
            </Text>
          </View>
        ) : (
          <Text className="text-dim text-[11px] font-semibold uppercase">{match.status}</Text>
        )}
      </View>
    </View>
  );
}
```

- [ ] **Step 2: `components/Ladder.tsx`** — top 5 asks (reversed), spread row, top 5 bids

```tsx
import { Text, View } from "react-native";
import type { Book } from "@/lib/types";
import { cents, shares } from "@/lib/format";

const DEPTH = 5;

function Row({ price, size, max, side }: { price: number; size: number; max: number; side: "bid" | "ask" }) {
  const pct = max > 0 ? Math.max(4, (size / max) * 100) : 0;
  const color = side === "bid" ? "bg-accent/15" : "bg-down/15";
  const text = side === "bid" ? "text-accent" : "text-down";
  return (
    <View className="flex-row items-center h-7 px-4">
      <View className={`absolute right-4 top-1 bottom-1 ${color}`} style={{ width: `${pct * 0.5}%` }} />
      <Text className={`${text} font-mono text-[13px] w-14`}>{cents(price)}</Text>
      <Text className="text-muted font-mono text-[13px] ml-auto">{shares(size)}</Text>
    </View>
  );
}

export function Ladder({ book }: { book: Book | null }) {
  if (!book) return null;
  const asks = book.asks.slice(0, DEPTH).reverse();
  const bids = book.bids.slice(0, DEPTH);
  const max = Math.max(1, ...[...asks, ...bids].map((l) => l.size));
  const spread =
    book.bids[0] && book.asks[0] ? book.asks[0].price - book.bids[0].price : null;
  return (
    <View className="py-2">
      <View className="flex-row justify-between px-4 pb-1">
        <Text className="text-dim text-[10px] uppercase">Price (YES)</Text>
        <Text className="text-dim text-[10px] uppercase">Size</Text>
      </View>
      {asks.map((l) => (
        <Row key={`a${l.price}`} {...l} max={max} side="ask" />
      ))}
      <View className="flex-row justify-center py-1 border-y border-line my-1">
        <Text className="text-dim text-[11px]">
          {spread !== null ? `spread ${spread}¢` : "empty book"}
        </Text>
      </View>
      {bids.map((l) => (
        <Row key={`b${l.price}`} {...l} max={max} side="bid" />
      ))}
    </View>
  );
}
```

- [ ] **Step 3: `app/market/[id].tsx`**

```tsx
import { useState } from "react";
import { Pressable, ScrollView, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router, useLocalSearchParams } from "expo-router";
import { useLiveMarket } from "@/lib/useLiveMarket";
import { usePitchWallet } from "@/lib/wallet";
import { MatchHeader } from "@/components/MatchHeader";
import { Ladder } from "@/components/Ladder";
import { TradeSheet } from "@/components/TradeSheet";
import { cents, shares, shortHash } from "@/lib/format";

export default function MarketScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wallet = usePitchWallet();
  const live = useLiveMarket(id!, wallet.address);
  const [sheetOpen, setSheetOpen] = useState(false);

  if (live.loading) {
    return (
      <SafeAreaView className="flex-1 bg-bg items-center justify-center">
        <Text className="text-dim">Loading…</Text>
      </SafeAreaView>
    );
  }
  if (live.errorStatus || !live.market) {
    return (
      <SafeAreaView className="flex-1 bg-bg items-center justify-center px-8">
        <Text className="text-down text-center">
          {live.errorStatus === 404 ? "Market not found." : "Couldn't reach the exchange."}
        </Text>
        <Pressable onPress={() => router.back()} className="mt-4">
          <Text className="text-accent">Back</Text>
        </Pressable>
      </SafeAreaView>
    );
  }

  const delta = live.priceDelta;
  return (
    <SafeAreaView className="flex-1 bg-bg" edges={["top"]}>
      <View className="flex-row items-center px-4 py-2">
        <Pressable onPress={() => router.back()} hitSlop={12}>
          <Text className="text-muted text-[15px]">‹ Markets</Text>
        </Pressable>
      </View>
      <MatchHeader match={live.match} />
      <ScrollView className="flex-1">
        <View className="px-4 pt-4 pb-2">
          <Text className="text-ink text-lg font-semibold">{live.market.title}</Text>
          <Text className="text-dim text-[12px] mt-1">{live.market.rule}</Text>
          <View className="flex-row items-baseline mt-3">
            <Text className="text-ink text-3xl font-bold">{cents(live.yesPrice)}</Text>
            <Text
              className={`ml-2 text-[13px] font-semibold ${
                delta >= 0 ? "text-accent" : "text-down"
              }`}
            >
              {delta >= 0 ? "+" : ""}
              {delta}¢
            </Text>
            <Text className="text-dim text-[11px] ml-2">YES</Text>
          </View>
          {live.oneliners.length > 0 && (
            <Text className="text-muted text-[13px] italic mt-2" numberOfLines={2}>
              “{live.oneliners[live.onelinerIdx]}”
            </Text>
          )}
        </View>
        <Ladder book={live.book} />
        <View className="px-4 pt-3 pb-24">
          <Text className="text-dim text-[10px] uppercase mb-1">Recent fills</Text>
          {live.fills.length === 0 && <Text className="text-dim text-[12px]">None yet.</Text>}
          {live.fills.slice(0, 8).map((f, i) => (
            <View key={`${f.taker_hash}${i}`} className="flex-row justify-between py-1">
              <Text className="text-muted font-mono text-[12px]">
                {shortHash(f.taker_hash)} · {f.match_type}
              </Text>
              <Text className="text-ink font-mono text-[12px]">
                {shares(f.size)} @ {cents(f.price)}
              </Text>
            </View>
          ))}
        </View>
      </ScrollView>
      <View className="absolute bottom-0 left-0 right-0 px-4 pb-8 pt-3 bg-bg border-t border-line">
        <Pressable
          onPress={() => setSheetOpen(true)}
          disabled={live.market.status !== "open"}
          className={`h-12 items-center justify-center ${
            live.market.status === "open" ? "bg-accent" : "bg-line2"
          }`}
        >
          <Text className="text-bg text-[15px] font-bold">
            {live.market.status === "open" ? "Trade" : "Market closed"}
          </Text>
        </Pressable>
      </View>
      <TradeSheet
        open={sheetOpen}
        onClose={() => setSheetOpen(false)}
        marketId={id!}
        yesPrice={live.yesPrice}
        marketStatus={live.market.status}
        balanceMicro={live.balanceMicro}
        onPlaced={live.refreshBalance}
      />
    </SafeAreaView>
  );
}
```
Until Task 8 lands, create a stub `components/TradeSheet.tsx` so this compiles:
```tsx
export function TradeSheet(_props: {
  open: boolean; onClose: () => void; marketId: string; yesPrice: number;
  marketStatus: string; balanceMicro: number; onPlaced: () => void;
}) {
  return null;
}
```

- [ ] **Step 4: Verify on device**

```bash
npx tsc --noEmit && npm test
npx expo start
```
Expected: tapping a market shows title/rule, the big YES price, the ladder mirroring the web ladder for the same market (compare side by side in the browser), rotating one-liner, and live updates when the MM bot re-quotes (book rows move without refresh). Background the app, foreground it → data refreshes (AppState path).

- [ ] **Step 5: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): market screen with live ladder, one-liners, fills"
```

---

### Task 8: TradeSheet — sign and place orders

**Files:**
- Create: `mobile/lib/errors.ts`
- Modify: `mobile/components/TradeSheet.tsx` (replace the Task 7 stub)

**Interfaces:**
- Consumes: `borshOrder`, `randomSalt`, `fromHex`, `toHex` (lib/borsh.ts); `api`, `ApiError` (lib/api.ts); `usePitchWallet` (lib/wallet.tsx); `buyCostMicro`, `maxPayoutMicro`, `usd` (lib/format.ts); `b64ToBytes` (lib/base64.ts, used by Task 9's fund flow which lives in this component).
- Produces: `TradeSheet` component with the props pinned in Task 7's stub. Order placement is byte-identical to web `TradePanel.place()`: outcome fixed to 1 (YES ladder), `fee_bps: 0`, `expiry: 0` (GTC), `salt: Number(randomSalt())`, hex sig.

- [ ] **Step 1: `lib/errors.ts`**

```ts
import { ApiError } from "./api";
import type { Side } from "./types";

/** Human message for an order-placement failure (pinned API semantics). */
export function placeErrorMessage(e: unknown, side: Side): string {
  if (e instanceof ApiError) {
    switch (e.status) {
      case 0: return "Exchange not configured.";
      case 401: return "Signature rejected — reconnect your wallet.";
      case 402: return side === "buy" ? "Insufficient vault balance." : "Not enough shares to sell.";
      case 409: return "Duplicate order — try again.";
      case 410: return "Locked at kickoff.";
    }
    return e.message || "Order failed.";
  }
  return e instanceof Error ? e.message : "Order failed.";
}
```

- [ ] **Step 2: Replace the TradeSheet stub**

`components/TradeSheet.tsx`:
```tsx
import { useEffect, useMemo, useState } from "react";
import {
  KeyboardAvoidingView, Modal, Platform, Pressable, Text, TextInput, View,
} from "react-native";
import bs58 from "bs58";
import { api } from "@/lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "@/lib/borsh";
import { placeErrorMessage } from "@/lib/errors";
import { buyCostMicro, maxPayoutMicro, usd } from "@/lib/format";
import { usePitchWallet } from "@/lib/wallet";
import type { Side } from "@/lib/types";
import { DepositButton } from "@/components/DepositButton";

interface Props {
  open: boolean;
  onClose: () => void;
  marketId: string; // 64-hex
  yesPrice: number;
  marketStatus: string;
  balanceMicro: number;
  onPlaced: () => void;
}

function clampInt(s: string, lo: number, hi: number): number {
  const n = Math.floor(Number(s) || 0);
  return Math.max(lo, Math.min(hi, n));
}

export function TradeSheet({
  open, onClose, marketId, yesPrice, marketStatus, balanceMicro, onPlaced,
}: Props) {
  const wallet = usePitchWallet();
  const [side, setSide] = useState<Side>("buy");
  const [price, setPrice] = useState(String(yesPrice));
  const [touchedPrice, setTouchedPrice] = useState(false);
  const [size, setSize] = useState("10");
  const [submit, setSubmit] = useState<"idle" | "signing" | "placed">("idle");
  const [placedLabel, setPlacedLabel] = useState("");
  const [serverError, setServerError] = useState<string | null>(null);

  useEffect(() => {
    if (!touchedPrice) setPrice(String(yesPrice));
  }, [yesPrice, touchedPrice]);

  const locked = marketStatus !== "open";
  const connected = !!wallet.address;
  const p = clampInt(price, 1, 99);
  const n = Math.max(0, Math.floor(Number(size) || 0));
  const costMicro = buyCostMicro(p, n);
  const insufficient = side === "buy" && connected && costMicro > balanceMicro;

  const error = useMemo(() => {
    if (locked || serverError) return serverError;
    if (!connected || n <= 0) return null;
    if (side === "buy" && balanceMicro === 0) return "Vault is empty — deposit first.";
    if (insufficient) return "Insufficient vault balance.";
    return null;
  }, [locked, serverError, connected, n, side, balanceMicro, insufficient]);

  async function place() {
    if (locked || submit !== "idle") return;
    if (!connected) {
      await wallet.connect();
      return;
    }
    if (n <= 0 || insufficient) return;
    setServerError(null);
    setSubmit("signing");
    try {
      const salt = randomSalt();
      const msg = borshOrder({
        maker: bs58.decode(wallet.address!),
        marketId: fromHex(marketId),
        outcome: 1, // this sheet trades the YES ladder, same as web TradePanel
        side: side === "buy" ? 0 : 1,
        price: p,
        size: BigInt(n),
        feeBps: 0,
        expiry: 0n,
        salt,
      });
      const sig = await wallet.signMessage(msg);
      const res = await api.postOrder({
        maker: wallet.address!,
        market_id: marketId,
        outcome: 1,
        side: side === "buy" ? 0 : 1,
        price: p,
        size: n,
        fee_bps: 0,
        expiry: 0,
        salt: Number(salt),
        sig: toHex(sig),
      });
      setPlacedLabel(res.fills.length ? "Filled" : "Resting on book");
      setSubmit("placed");
      onPlaced();
      setTimeout(() => setSubmit("idle"), 2600);
    } catch (e) {
      setSubmit("idle");
      setServerError(placeErrorMessage(e, side));
    }
  }

  const cta = !connected
    ? "Connect wallet"
    : submit === "signing"
      ? "Signing…"
      : submit === "placed"
        ? placedLabel
        : side === "buy"
          ? `Buy YES · ${usd(costMicro)}`
          : `Sell YES · max ${usd(maxPayoutMicro(n))}`;

  return (
    <Modal visible={open} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable className="flex-1 bg-black/60" onPress={onClose} />
      <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : undefined}>
        <View className="bg-bg border-t border-line2 px-4 pt-4 pb-10">
          <View className="flex-row items-center justify-between mb-4">
            <Text className="text-ink text-[15px] font-semibold">Trade YES</Text>
            <View className="flex-row items-center">
              <Text className="text-dim text-[11px] mr-3">Vault {usd(balanceMicro)}</Text>
              <DepositButton onFunded={onPlaced} />
            </View>
          </View>

          <View className="flex-row mb-4">
            {(["buy", "sell"] as Side[]).map((s) => (
              <Pressable
                key={s}
                onPress={() => { setSide(s); setServerError(null); }}
                className={`flex-1 h-10 items-center justify-center border ${
                  side === s
                    ? s === "buy" ? "border-accent bg-accent/10" : "border-down bg-down/10"
                    : "border-line"
                }`}
              >
                <Text className={side === s ? (s === "buy" ? "text-accent" : "text-down") : "text-muted"}>
                  {s === "buy" ? "Buy" : "Sell"}
                </Text>
              </Pressable>
            ))}
          </View>

          <View className="flex-row gap-3 mb-4">
            <View className="flex-1">
              <Text className="text-dim text-[10px] uppercase mb-1">Price (¢)</Text>
              <TextInput
                value={price}
                onChangeText={(t) => { setPrice(t); setTouchedPrice(true); setServerError(null); }}
                keyboardType="number-pad"
                className="border border-line text-ink font-mono h-11 px-3"
                placeholderTextColor="#565b63"
              />
            </View>
            <View className="flex-1">
              <Text className="text-dim text-[10px] uppercase mb-1">Size (shares)</Text>
              <TextInput
                value={size}
                onChangeText={(t) => { setSize(t); setServerError(null); }}
                keyboardType="number-pad"
                className="border border-line text-ink font-mono h-11 px-3"
                placeholderTextColor="#565b63"
              />
            </View>
          </View>

          {error && <Text className="text-down text-[12px] mb-3">{error}</Text>}

          <Pressable
            onPress={place}
            disabled={locked || submit === "signing"}
            className={`h-12 items-center justify-center ${
              submit === "placed" ? "bg-line2" : side === "buy" ? "bg-accent" : "bg-down"
            }`}
          >
            <Text className="text-bg text-[15px] font-bold">{cta}</Text>
          </Pressable>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}
```
Until Task 9 lands, stub `components/DepositButton.tsx` so this compiles:
```tsx
export function DepositButton(_props: { onFunded: () => void }) {
  return null;
}
```

- [ ] **Step 3: Verify against the live backend**

```bash
npx tsc --noEmit && npm test
npx expo start
```
On device: connect (creates the demo wallet), attempt a buy with an empty vault → "Vault is empty — deposit first." (deposit arrives in Task 9; fund this wallet via the web UI or `curl -X POST $API/wallet/deposit -d '{"wallet":"<addr>","amount":1000000000}'` if the server runs in mirror mode). Then: place a bid below the ask → "Resting on book" and the ladder shows the level grow; place a crossing order → "Filled" and a fill row appears. Force a replay by rapid double-tap — second returns the 409 message.

- [ ] **Step 4: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): trade sheet — borsh signing + order placement on the YES ladder"
```

---

### Task 9: Deposit flow

**Files:**
- Modify: `mobile/components/DepositButton.tsx` (replace stub)

**Interfaces:**
- Consumes: `api.depositInit/depositComplete/depositMirror`, `usePitchWallet`, `b64ToBytes`, `toHex`.
- Produces: `DepositButton({ onFunded: () => void })` — deposits 1,000 demo USDC via the two-step cosigned flow (server builds tx, wallet signs `message_b64` bytes, server submits) with automatic mirror-faucet fallback when the server runs off-chain (`depositInit` returns null on 409). Identical protocol to web `TradePanel.fund()`.

- [ ] **Step 1: Replace the stub**

`components/DepositButton.tsx`:
```tsx
import { useState } from "react";
import { ActivityIndicator, Alert, Pressable, Text } from "react-native";
import { api } from "@/lib/api";
import { b64ToBytes } from "@/lib/base64";
import { toHex } from "@/lib/borsh";
import { usePitchWallet } from "@/lib/wallet";

const AMOUNT_MICRO = 1_000_000_000; // 1,000 demo USDC, same as web

export function DepositButton({ onFunded }: { onFunded: () => void }) {
  const wallet = usePitchWallet();
  const [busy, setBusy] = useState(false);

  async function fund() {
    if (busy) return;
    if (!wallet.address) {
      await wallet.connect();
      return;
    }
    setBusy(true);
    try {
      // Real deposit: the server builds an operator-cosigned devnet tx; the
      // wallet signs its message bytes. Mirror faucet when server is off-chain.
      const init = await api.depositInit(wallet.address, AMOUNT_MICRO);
      if (init) {
        const sig = await wallet.signMessage(b64ToBytes(init.message_b64));
        await api.depositComplete(init.deposit_id, wallet.address, AMOUNT_MICRO, toHex(sig));
      } else {
        await api.depositMirror(wallet.address, AMOUNT_MICRO);
      }
      onFunded();
    } catch (e) {
      Alert.alert("Deposit failed", e instanceof Error ? e.message : "Try again.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Pressable
      onPress={fund}
      disabled={busy}
      className="h-8 px-3 items-center justify-center border border-accent"
    >
      {busy ? (
        <ActivityIndicator size="small" color="#34d399" />
      ) : (
        <Text className="text-accent text-[12px] font-semibold">Deposit</Text>
      )}
    </Pressable>
  );
}
```

- [ ] **Step 2: Verify against the live backend**

On device with a FRESH demo wallet (disconnect → connect): tap Deposit → vault balance in the sheet header becomes $1,000.00 (via `onFunded → refreshBalance`). If the server runs on-chain, verify the devnet tx exists (server logs / explorer). Then place a buy — full loop now works phone-only.

```bash
npx tsc --noEmit && npm test
```

- [ ] **Step 3: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): two-step cosigned deposit with mirror fallback"
```

---

### Task 10: Portfolio screen — balance, positions, exit, open orders, cancel

**Files:**
- Modify: `mobile/app/(tabs)/portfolio.tsx` (replace stub)

**Interfaces:**
- Consumes: `api.getPortfolio/cancelOrder/postOrder`, `usePitchWallet`, `DepositButton`, `borshOrder/randomSalt/fromHex/toHex`, `usd/cents/shares`, `placeErrorMessage`.
- Produces: the portfolio tab. Exit = signed SELL YES order at the position's `current` (best-bid mark) for the full YES size — the same semantics as the web portfolio's exit. Unrealized PnL = `(current − avg_cost) × yes × 10_000` micro.

- [ ] **Step 1: Replace the stub**

`app/(tabs)/portfolio.tsx`:
```tsx
import { useCallback, useState } from "react";
import { Alert, Pressable, RefreshControl, ScrollView, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useFocusEffect } from "expo-router";
import bs58 from "bs58";
import { api } from "@/lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "@/lib/borsh";
import { placeErrorMessage } from "@/lib/errors";
import { cents, shares, usd } from "@/lib/format";
import { usePitchWallet } from "@/lib/wallet";
import { DepositButton } from "@/components/DepositButton";
import type { Portfolio, Position } from "@/lib/types";

const EMPTY: Portfolio = { balance_micro: 0, positions: [], orders: [], history: [] };

export default function PortfolioScreen() {
  const wallet = usePitchWallet();
  const [pf, setPf] = useState<Portfolio>(EMPTY);
  const [loading, setLoading] = useState(false);
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!wallet.address) { setPf(EMPTY); return; }
    setLoading(true);
    try { setPf(await api.getPortfolio(wallet.address)); }
    catch { /* keep last data; pull-to-refresh retries */ }
    finally { setLoading(false); }
  }, [wallet.address]);

  useFocusEffect(useCallback(() => { load(); }, [load]));

  async function exit(p: Position) {
    if (busyKey || !wallet.address || p.yes <= 0 || p.current <= 0) return;
    setBusyKey(p.market_id);
    try {
      const salt = randomSalt();
      const msg = borshOrder({
        maker: bs58.decode(wallet.address),
        marketId: fromHex(p.market_id),
        outcome: 1,
        side: 1, // SELL
        price: p.current, // exit at the best-bid mark
        size: BigInt(p.yes),
        feeBps: 0,
        expiry: 0n,
        salt,
      });
      const sig = await wallet.signMessage(msg);
      await api.postOrder({
        maker: wallet.address, market_id: p.market_id,
        outcome: 1, side: 1, price: p.current, size: p.yes,
        fee_bps: 0, expiry: 0, salt: Number(salt), sig: toHex(sig),
      });
      await load();
    } catch (e) {
      Alert.alert("Exit failed", placeErrorMessage(e, "sell"));
    } finally { setBusyKey(null); }
  }

  async function cancel(orderHash: string) {
    if (busyKey || !wallet.address) return;
    setBusyKey(orderHash);
    try { await api.cancelOrder(orderHash, wallet.address); await load(); }
    catch (e) { Alert.alert("Cancel failed", e instanceof Error ? e.message : "Try again."); }
    finally { setBusyKey(null); }
  }

  const unrealized = (p: Position) => (p.current - p.avg_cost) * p.yes * 10_000;

  return (
    <SafeAreaView className="flex-1 bg-bg" edges={["top"]}>
      <ScrollView
        refreshControl={<RefreshControl refreshing={loading} onRefresh={load} tintColor="#34d399" />}
      >
        <View className="px-4 pt-2 pb-4 border-b border-line">
          <Text className="text-ink text-xl font-bold">Portfolio</Text>
          {wallet.address ? (
            <View className="flex-row items-end justify-between mt-3">
              <View>
                <Text className="text-dim text-[10px] uppercase">Vault balance</Text>
                <Text className="text-ink text-2xl font-bold">{usd(pf.balance_micro)}</Text>
                <Text className="text-dim text-[10px] font-mono mt-1">
                  {wallet.address.slice(0, 6)}…{wallet.address.slice(-4)}
                  {wallet.isDemo ? " · demo" : ""}
                </Text>
              </View>
              <DepositButton onFunded={load} />
            </View>
          ) : (
            <Pressable
              onPress={() => wallet.connect()}
              className="h-11 items-center justify-center bg-accent mt-3"
            >
              <Text className="text-bg font-bold">Connect wallet</Text>
            </Pressable>
          )}
        </View>

        <View className="px-4 pt-4">
          <Text className="text-dim text-[10px] uppercase mb-2">Positions</Text>
          {pf.positions.length === 0 && (
            <Text className="text-dim text-[12px] mb-4">No positions.</Text>
          )}
          {pf.positions.map((p) => (
            <View key={p.market_id} className="border border-line p-3 mb-2">
              <Text className="text-ink text-[14px] font-medium" numberOfLines={1}>{p.title}</Text>
              <View className="flex-row justify-between mt-2">
                <Text className="text-muted text-[12px] font-mono">
                  {shares(p.yes)} YES @ {cents(p.avg_cost)} → {cents(p.current)}
                </Text>
                <Text
                  className={`text-[12px] font-mono ${unrealized(p) >= 0 ? "text-accent" : "text-down"}`}
                >
                  {unrealized(p) >= 0 ? "+" : ""}{usd(unrealized(p))}
                </Text>
              </View>
              {p.realized !== 0 && (
                <Text className="text-dim text-[11px] font-mono mt-1">
                  realized {p.realized >= 0 ? "+" : ""}{usd(p.realized)}
                </Text>
              )}
              {p.yes > 0 && p.current > 0 && (
                <Pressable
                  onPress={() => exit(p)}
                  disabled={busyKey === p.market_id}
                  className="h-9 items-center justify-center border border-down mt-2"
                >
                  <Text className="text-down text-[12px] font-semibold">
                    {busyKey === p.market_id ? "Exiting…" : `Exit at ${cents(p.current)}`}
                  </Text>
                </Pressable>
              )}
            </View>
          ))}
        </View>

        <View className="px-4 pt-4 pb-16">
          <Text className="text-dim text-[10px] uppercase mb-2">Open orders</Text>
          {pf.orders.length === 0 && <Text className="text-dim text-[12px]">None.</Text>}
          {pf.orders.map((o) => (
            <View
              key={o.order_hash}
              className="flex-row items-center justify-between border-b border-line py-2.5"
            >
              <View className="flex-1 pr-2">
                <Text className="text-ink text-[13px]" numberOfLines={1}>{o.title}</Text>
                <Text className="text-muted text-[11px] font-mono">
                  {o.side.toUpperCase()} {o.outcome} · {shares(o.remaining)}/{shares(o.size)} @ {cents(o.price)}
                </Text>
              </View>
              <Pressable
                onPress={() => cancel(o.order_hash)}
                disabled={busyKey === o.order_hash}
                hitSlop={8}
              >
                <Text className="text-down text-[12px] font-semibold">
                  {busyKey === o.order_hash ? "…" : "Cancel"}
                </Text>
              </Pressable>
            </View>
          ))}
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
```

- [ ] **Step 2: Verify against the live backend**

```bash
npx tsc --noEmit && npm test
```
On device: after Task 8/9's trades, the Portfolio tab shows the balance, the position with avg→current and unrealized PnL matching the web portfolio for the same wallet-state, and the resting order from Task 8. Cancel it → it disappears and the ladder level shrinks. Exit a position → SELL fills against the bot's bid, realized PnL appears, balance grows.

- [ ] **Step 3: Commit**

```bash
git add -A mobile ../progress.md
git commit -m "feat(mobile): portfolio — positions with exit, open orders with cancel"
```

---

### Task 11: Privy Expo integration (real product wallet path)

**Files:**
- Modify: `mobile/lib/wallet.tsx` (add the Privy backend behind env flag), `mobile/app.json` (native config), `mobile/package.json` (deps), `mobile/.env` (add Privy ids)

**Interfaces:**
- Consumes / Produces: the `PitchWallet` interface is UNCHANGED — every screen keeps working against it. `PitchWalletProvider` now picks Privy when `EXPO_PUBLIC_PRIVY_APP_ID` is set, demo otherwise (exactly the web's selection logic).

**⚠️ Pre-flight (do this first):** check the current Privy Expo docs (https://docs.privy.io/basics/react-native/setup) for the exact package list and Solana signing API — the SDK moves fast. The code below reflects the documented shape (`@privy-io/expo`, `useEmbeddedSolanaWallet`); adjust names to what the installed version exports, keeping the `PitchWallet` mapping identical. Create/reuse a Privy app (dashboard) with a **mobile client** — you need both `appId` and `clientId`. **This SDK does not run in Expo Go** — a dev-client build is required. If this task stalls more than a day, stop: the demo wallet is the shipped cut (spec §7).

- [ ] **Step 1: Install and configure**

```bash
npx expo install @privy-io/expo expo-application expo-crypto expo-linking expo-web-browser react-native-webview @privy-io/expo-native-extensions expo-build-properties
```
In `app.json` add iOS/Android identifiers and build properties:
```json
"ios": { "bundleIdentifier": "com.pitchmarket.app", "supportsTablet": false },
"android": { "package": "com.pitchmarket.app" },
"plugins": [
  "expo-router",
  "expo-secure-store",
  ["expo-build-properties", { "ios": { "deploymentTarget": "17.5" } }]
]
```
`.env` gains:
```
EXPO_PUBLIC_PRIVY_APP_ID=<from dashboard>
EXPO_PUBLIC_PRIVY_CLIENT_ID=<from dashboard>
```

- [ ] **Step 2: Add the Privy backend to `lib/wallet.tsx`**

Replace `PitchWalletProvider` with the selector + bridge (DemoWalletProvider stays untouched below it):
```tsx
import { PrivyProvider, usePrivy, useLogin } from "@privy-io/expo";
import { useEmbeddedSolanaWallet } from "@privy-io/expo";

const PRIVY_APP_ID = process.env.EXPO_PUBLIC_PRIVY_APP_ID ?? "";
const PRIVY_CLIENT_ID = process.env.EXPO_PUBLIC_PRIVY_CLIENT_ID ?? "";

export function PitchWalletProvider({ children }: { children: React.ReactNode }) {
  if (!PRIVY_APP_ID) return <DemoWalletProvider>{children}</DemoWalletProvider>;
  return (
    <PrivyProvider appId={PRIVY_APP_ID} clientId={PRIVY_CLIENT_ID}>
      <PrivyBridge>{children}</PrivyBridge>
    </PrivyProvider>
  );
}

function PrivyBridge({ children }: { children: React.ReactNode }) {
  const { user, isReady, logout } = usePrivy();
  const { login } = useLogin();
  const solana = useEmbeddedSolanaWallet();
  const wallet = solana.wallets?.[0];

  const value = useMemo<PitchWallet>(
    () => ({
      ready: isReady,
      address: user && wallet ? wallet.address : null,
      isDemo: false,
      connect: async () => {
        await login({ loginMethods: ["email"] });
        if (solana.status === "not-created" && solana.create) await solana.create();
      },
      disconnect: async () => { await logout(); },
      signMessage: async (message) => {
        if (!wallet) throw new Error("wallet not connected");
        const provider = await wallet.getProvider();
        const { signature } = await provider.request({
          method: "signMessage",
          params: { message },
        });
        // Depending on SDK version the signature is bytes or base64 — normalize:
        return typeof signature === "string" ? b64ToBytes(signature) : new Uint8Array(signature);
      },
    }),
    [isReady, user, wallet, login, logout, solana]
  );
  return <WalletCtx.Provider value={value}>{children}</WalletCtx.Provider>;
}
```
(Add `import { b64ToBytes } from "./base64";` at the top.)

- [ ] **Step 3: Dev-client build and on-device verification**

```bash
npx expo prebuild
npx expo run:ios      # or run:android — a dev client replaces Expo Go
```
On device: Connect → Privy email login → embedded Solana wallet created → deposit → place order → 200. Cross-check: sign in with the SAME Privy account on the web app — same address, same portfolio. Then unset `EXPO_PUBLIC_PRIVY_APP_ID` and confirm the demo-wallet path still works (the cut stays alive).

- [ ] **Step 4: Regression + commit**

```bash
npx tsc --noEmit && npm test && npm run check-borsh
git add -A mobile ../progress.md
git commit -m "feat(mobile): Privy Expo embedded wallet behind env flag (demo wallet fallback)"
```

---

### Task 12: Lib-level e2e script + final verification + progress.md

**Files:**
- Create: `mobile/scripts/e2e-flow.ts`
- Modify: `mobile/package.json` (script + tsx devDep), `progress.md` (component row + Jul-checklist honesty + changelog)

**Interfaces:**
- Consumes: `mobile/lib/api.ts`, `mobile/lib/borsh.ts` — the point is that the SHIPPED mobile lib code drives a real backend flow in CI-able form (Node ≥20 has `fetch` and `crypto.getRandomValues`).

- [ ] **Step 1: Write the e2e script**

```bash
npm i -D tsx
```
Add script: `"e2e": "tsx scripts/e2e-flow.ts"`.

`scripts/e2e-flow.ts`:
```ts
// Lib-level e2e: drives the mobile lib against a LIVE backend.
// Run: EXPO_PUBLIC_API_URL=http://localhost:8080 npm run e2e
import nacl from "tweetnacl";
import bs58 from "bs58";
import { api, configured } from "../lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "../lib/borsh";

function assert(cond: unknown, msg: string): asserts cond {
  if (!cond) { console.error(`✗ ${msg}`); process.exit(1); }
  console.log(`✓ ${msg}`);
}

async function main() {
  assert(configured(), "EXPO_PUBLIC_API_URL configured");

  const kp = nacl.sign.keyPair();
  const addr = bs58.encode(kp.publicKey);
  console.log(`wallet ${addr}`);

  // deposit (two-step if on-chain, mirror otherwise)
  const init = await api.depositInit(addr, 1_000_000_000);
  if (init) {
    const msg = Uint8Array.from(Buffer.from(init.message_b64, "base64"));
    const sig = nacl.sign.detached(msg, kp.secretKey);
    await api.depositComplete(init.deposit_id, addr, 1_000_000_000, toHex(sig));
  } else {
    await api.depositMirror(addr, 1_000_000_000);
  }
  assert((await api.getBalance(addr)) === 1_000_000_000, "deposit credited 1,000 USDC");

  // pick an open binary market
  const market = (await api.listMarkets("open")).find((m) => m.type === "binary");
  assert(market, "an open binary market exists");

  // place a deep bid (won't cross), see it on the book, then cancel it
  const salt = randomSalt();
  const order = {
    maker: bs58.decode(addr), marketId: fromHex(market!.market_id),
    outcome: 1, side: 0, price: 2, size: 10n, feeBps: 0, expiry: 0n, salt,
  };
  const sig = nacl.sign.detached(borshOrder(order), kp.secretKey);
  const res = await api.postOrder({
    maker: addr, market_id: market!.market_id, outcome: 1, side: 0,
    price: 2, size: 10, fee_bps: 0, expiry: 0, salt: Number(salt), sig: toHex(sig),
  });
  assert(res.order_hash, "order accepted");

  const book = await api.getBook(market!.market_id);
  assert(book.bids.some((l) => l.price === 2), "bid visible on the unified ladder");

  const pf = await api.getPortfolio(addr);
  assert(pf.orders.some((o) => o.order_hash === res.order_hash), "order in portfolio");

  await api.cancelOrder(res.order_hash, addr);
  const pf2 = await api.getPortfolio(addr);
  assert(!pf2.orders.some((o) => o.order_hash === res.order_hash), "cancel removed it");

  console.log("\nmobile lib e2e: ALL GREEN");
}

main().catch((e) => { console.error(e); process.exit(1); });
```

- [ ] **Step 2: Run it against the live backend**

```bash
EXPO_PUBLIC_API_URL=http://localhost:8080 npm run e2e   # match the real server port
```
Expected: `mobile lib e2e: ALL GREEN`.

- [ ] **Step 3: Full on-device click-through (the ✅ gate)**

On a real phone against the live backend, in one sitting:
1. Fresh install → Markets tab shows live markets with prices.
2. Open a market → ladder matches the web ladder side-by-side; one-liner rotates.
3. Connect → deposit → balance $1,000.
4. Buy YES crossing the ask → "Filled" → fill appears in Recent fills.
5. Place a resting bid → visible on ladder → Portfolio shows it → cancel it.
6. Portfolio position shows sane avg/current/unrealized → Exit → realized PnL books.
7. Background the app 30s during a live match → foreground → prices refresh.
8. Airplane mode → prices stop → toggling back reconnects (WS backoff).

- [ ] **Step 4: Final progress.md update + commit**

Add to the §3 component table (or a new §3b "Mobile" table):
`| mobile/ | ✅/🟡 per click-through | Expo core loop: markets, market+trade, deposit, portfolio; Privy + demo wallet; borsh vector gated (5th encoder); lib e2e green vs live backend |`
— mark ✅ ONLY the flows that passed Step 3 on-device; anything unrun stays 🟡 with a note. Append the changelog row with exactly what was verified and how.

```bash
git add -A mobile ../progress.md
git commit -m "test(mobile): lib e2e vs live backend + on-device verification pass"
```

---

## Self-Review (done)

- **Spec coverage:** architecture (T1), copied libs + golden vector (T2), api port (T3), wallet demo/Privy (T4/T11), WS hook + foreground refetch (T5), markets index (T6), market screen + ladder + one-liners (T7), trade panel + error semantics 401/409/410 (T8), two-step deposit + mirror fallback (T9), portfolio with cancel/exit/PnL (T10), verification plan incl. on-device gate + progress.md honesty (T12), cut order honored (demo wallet ships before Privy; deposit falls back to web-funding since the account is the pubkey).
- **Placeholder scan:** the two intentional stubs (TradeSheet in T7, DepositButton in T8) exist only to keep intermediate commits compiling and are replaced by the named later task with full code — not placeholders left to the engineer.
- **Type consistency:** `PitchWallet` (async connect/disconnect) is defined once in T4 and consumed unchanged in T7/T8/T10/T11; `TradeSheet`/`DepositButton` prop shapes match between stub and implementation; api signatures are pinned to the web client.
