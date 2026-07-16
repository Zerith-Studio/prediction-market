// Wallet layer with two interchangeable backends behind one context, mirroring
// frontend/lib/wallet.tsx:
//
//   1. Privy Expo embedded wallet (real product path) — active when
//      EXPO_PUBLIC_PRIVY_APP_ID is set. Login is OAuth (`login({ provider: "google" })`
//      via useLoginWithOAuth) rather than the web's silent-signing flow, because
//      the PitchWallet.connect() interface takes no arguments — there's nowhere
//      to collect an email/OTP pair the way the Expo SDK's useLoginWithEmail
//      wants. OAuth is the one Expo login hook that fits a single no-arg call.
//   2. Local demo wallet: an ed25519 keypair persisted in expo-secure-store. The
//      backend accepts any well-signed order, so this exercises the REAL
//      exchange with zero credentials.
//
// Both sign the same borsh bytes; the backend cannot tell them apart.
//
// `@privy-io/expo` has no web support (see its README) and requires a
// dev-client build — it cannot run in Expo Go. It's `require()`d lazily
// inside the PRIVY_APP_ID branch, not imported statically at module scope, so
// a Privy-less build never pays for it. That alone wasn't sufficient, though:
// Metro resolves every `require()` call it can see statically regardless of
// where it sits, so `expo export --platform web` still failed until its full
// transitive peer-dependency list was actually installed — the OAuth chunk
// pulls in `expo-apple-authentication` and `react-native-passkeys`, neither
// of which is in the brief's/quickstart's headline install command. Verified:
// with those installed and the lazy require, `expo export --platform web`
// (no Privy env vars set) succeeds.
//
// UNVERIFIED: this Privy path has never run against a real Privy app (no
// dashboard credentials, no device, no dev-client build available in this
// environment) — see progress.md.
import {
  createContext, useCallback, useContext, useEffect, useMemo, useState,
} from "react";
import * as SecureStore from "expo-secure-store";
import nacl from "tweetnacl";
import bs58 from "bs58";
import { fromHex, toHex } from "./borsh";
import { b64ToBytes, bytesToB64 } from "./base64";
import type * as PrivyExpo from "@privy-io/expo";

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

const PRIVY_APP_ID = process.env.EXPO_PUBLIC_PRIVY_APP_ID ?? "";
const PRIVY_CLIENT_ID = process.env.EXPO_PUBLIC_PRIVY_CLIENT_ID ?? "";

export function PitchWalletProvider({ children }: { children: React.ReactNode }) {
  if (!PRIVY_APP_ID) return <DemoWalletProvider>{children}</DemoWalletProvider>;
  // Lazy require — see the file-header note on why this can't be a static import.
  const { PrivyProvider } = require("@privy-io/expo") as typeof PrivyExpo;
  return (
    <PrivyProvider appId={PRIVY_APP_ID} clientId={PRIVY_CLIENT_ID || undefined}>
      <PrivyBridge>{children}</PrivyBridge>
    </PrivyProvider>
  );
}

function PrivyBridge({ children }: { children: React.ReactNode }) {
  const { usePrivy, useLoginWithOAuth, useEmbeddedSolanaWallet } =
    require("@privy-io/expo") as typeof PrivyExpo;
  const { user, isReady, logout } = usePrivy();
  const { login } = useLoginWithOAuth();
  const solana = useEmbeddedSolanaWallet();
  const wallet = solana.wallets?.[0];

  const value = useMemo<PitchWallet>(
    () => ({
      ready: isReady,
      address: user && wallet ? wallet.address : null,
      isDemo: false,
      connect: async () => {
        await login({ provider: "google" });
        if (solana.status === "not-created" && solana.create) await solana.create();
      },
      disconnect: async () => {
        await logout();
      },
      signMessage: async (message) => {
        if (!wallet) throw new Error("wallet not connected");
        const provider = await wallet.getProvider();
        // The Expo SDK's signMessage RPC takes/returns base64 strings, not raw
        // bytes; the PitchWallet contract signs/returns raw Uint8Arrays.
        const { signature } = await provider.request({
          method: "signMessage",
          params: { message: bytesToB64(message) },
        });
        return b64ToBytes(signature);
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [isReady, user, wallet, login, logout, solana]
  );
  return <WalletCtx.Provider value={value}>{children}</WalletCtx.Provider>;
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
