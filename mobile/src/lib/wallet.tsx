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
