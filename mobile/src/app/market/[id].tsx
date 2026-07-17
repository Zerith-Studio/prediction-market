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
