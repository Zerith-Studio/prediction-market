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
