"use client";

// Wallet layer with two interchangeable backends behind one context:
//
//   1. Privy embedded wallet (real product path) — active when
//      NEXT_PUBLIC_PRIVY_APP_ID is set. Login creates an embedded Solana
//      wallet; orders are signed silently (showWalletUIs: false) — the
//      "trading is silent, only deposit pops a modal" product promise.
//   2. Local demo wallet — an ed25519 keypair persisted in localStorage.
//      The backend accepts any well-signed order, so this exercises the REAL
//      exchange end-to-end with zero credentials (demo/CI path).
//
// Both sign the same borsh bytes; the backend cannot tell them apart.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { PrivyProvider, usePrivy } from "@privy-io/react-auth";
import { useSignMessage, useWallets } from "@privy-io/react-auth/solana";
import nacl from "tweetnacl";
import bs58 from "bs58";
import { fromHex, toHex } from "./borsh";

export interface PitchWallet {
  ready: boolean;
  /** base58 pubkey, null when not connected */
  address: string | null;
  isDemo: boolean;
  connect: () => void;
  disconnect: () => void;
  signMessage: (message: Uint8Array) => Promise<Uint8Array>;
}

const WalletCtx = createContext<PitchWallet | null>(null);

export function usePitchWallet(): PitchWallet {
  const ctx = useContext(WalletCtx);
  if (!ctx) throw new Error("usePitchWallet outside <PitchWalletProvider>");
  return ctx;
}

const PRIVY_APP_ID = process.env.NEXT_PUBLIC_PRIVY_APP_ID ?? "";

export function PitchWalletProvider({ children }: { children: React.ReactNode }) {
  if (!PRIVY_APP_ID) return <DemoWalletProvider>{children}</DemoWalletProvider>;
  return (
    <PrivyProvider
      appId={PRIVY_APP_ID}
      config={{
        appearance: { theme: "dark", accentColor: "#34d399" },
        embeddedWallets: { solana: { createOnLogin: "users-without-wallets" } },
      }}
    >
      <PrivyBridge>{children}</PrivyBridge>
    </PrivyProvider>
  );
}

function PrivyBridge({ children }: { children: React.ReactNode }) {
  const { ready, authenticated, login, logout } = usePrivy();
  const { wallets } = useWallets();
  const { signMessage } = useSignMessage();
  const wallet = wallets[0];

  const value = useMemo<PitchWallet>(
    () => ({
      ready,
      address: authenticated && wallet ? wallet.address : null,
      isDemo: false,
      connect: login,
      disconnect: logout,
      signMessage: async (message) => {
        if (!wallet) throw new Error("wallet not connected");
        const { signature } = await signMessage({
          message,
          wallet,
          // Silent signing: the order slip IS the confirmation UI.
          options: { uiOptions: { showWalletUIs: false } },
        });
        return signature;
      },
    }),
    [ready, authenticated, wallet, login, logout, signMessage]
  );
  return <WalletCtx.Provider value={value}>{children}</WalletCtx.Provider>;
}

// --- local demo wallet -------------------------------------------------------

const SEED_KEY = "pm_demo_wallet_seed";

function loadOrCreateSeed(): Uint8Array {
  const existing = window.localStorage.getItem(SEED_KEY);
  if (existing && existing.length === 64) return fromHex(existing);
  const seed = new Uint8Array(32);
  crypto.getRandomValues(seed);
  window.localStorage.setItem(SEED_KEY, toHex(seed));
  return seed;
}

function DemoWalletProvider({ children }: { children: React.ReactNode }) {
  const [keys, setKeys] = useState<nacl.SignKeyPair | null>(null);
  const [ready, setReady] = useState(false);

  // Restore a previous session's wallet; creation is explicit via connect().
  useEffect(() => {
    const existing = window.localStorage.getItem(SEED_KEY);
    if (existing && existing.length === 64) {
      setKeys(nacl.sign.keyPair.fromSeed(fromHex(existing)));
    }
    setReady(true);
  }, []);

  const connect = useCallback(() => {
    setKeys(nacl.sign.keyPair.fromSeed(loadOrCreateSeed()));
  }, []);

  const disconnect = useCallback(() => {
    window.localStorage.removeItem(SEED_KEY);
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
