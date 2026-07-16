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
