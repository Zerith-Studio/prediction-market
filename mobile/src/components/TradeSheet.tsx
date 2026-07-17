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
