"use client";

import type { Market, Match } from "@/lib/types";
import { useRelatedMarkets } from "@/lib/useRelatedMarkets";
import { BinaryCard, GlobalBinaryCard, PrecisionCard } from "@/components/MarketCard";

// Related markets strip on the market page: other markets on the same fixture
// (shown first), then markets involving either team. Renders nothing when there
// is nothing related, so it never leaves an empty section on the page.
export function RelatedMarkets({
  current,
  match,
}: {
  current: Market | null;
  match: Match | null;
}) {
  const { markets, matchById } = useRelatedMarkets(current, match);
  if (markets.length === 0) return null;

  return (
    <section className="rule-t py-10">
      <div className="mb-4 eyebrow">Related markets</div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {markets.map((m) => {
          const mt = m.match_id ? matchById.get(m.match_id) : undefined;
          if (m.type === "precision") {
            return <PrecisionCard key={m.market_id} m={m} match={mt} />;
          }
          return mt ? (
            <BinaryCard key={m.market_id} m={m} match={mt} />
          ) : (
            <GlobalBinaryCard key={m.market_id} m={m} />
          );
        })}
      </div>
    </section>
  );
}
