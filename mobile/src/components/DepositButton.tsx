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
