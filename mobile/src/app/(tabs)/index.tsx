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
