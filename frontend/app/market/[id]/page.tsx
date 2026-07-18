"use client";

import Link from "next/link";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { useLiveMarket } from "@/lib/useLiveMarket";
import { TopBar } from "@/components/TopBar";
import { MatchHero } from "@/components/MatchHero";
import { MatchCentre } from "@/components/MatchCentre";
import { PriceChart } from "@/components/PriceChart";
import { OrderBook } from "@/components/OrderBook";
import { RecentFills } from "@/components/RecentFills";
import { Comments } from "@/components/Comments";
import { TradePanel } from "@/components/TradePanel";
import { MarketPositions } from "@/components/MarketPositions";
import { RelatedMarkets } from "@/components/RelatedMarkets";
import { PitchTicker } from "@/components/PitchTicker";
import { MarketSkeleton } from "@/components/Skeletons";
import { StarButton } from "@/components/StarButton";
import { usePitchWallet } from "@/lib/wallet";
import { useState } from "react";

export default function MarketPage({
  params,
  searchParams,
}: {
  params: { id: string };
  searchParams: { o?: string };
}) {
  const wallet = usePitchWallet();
  const m = useLiveMarket(params.id, wallet.address);
  const up = m.priceDelta >= 0;
  // Bumped whenever an order is placed/exited/cancelled so the market-level
  // position panel refetches immediately instead of waiting for its poll.
  const [posRefresh, setPosRefresh] = useState(0);
  // A card's Yes/No button deep-links here with ?o=yes|no to preselect the side.
  const initialOutcome: 0 | 1 = searchParams?.o === "no" ? 0 : 1;

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={m.balanceMicro} />
      <main className="mx-auto max-w-[1200px] px-5 sm:px-8">
        {m.loading && <MarketSkeleton />}
        {!m.loading && m.errorStatus && <NotFound status={m.errorStatus} />}

        {!m.loading && !m.errorStatus && m.market && m.match && m.book && (
          <>
            {m.market.match_id ? (
              <>
                <MatchHero match={m.match} />

                {/* real TxLINE match detail — live stats + team sheets */}
                <MatchCentre match={m.match} />
              </>
            ) : (
              <GlobalMarketHero market={m.market} />
            )}

            {/* chart (2/3) + trade panel (1/3), side by side */}
            <section className="rule-t pt-8">
              <div className="grid gap-8 lg:grid-cols-3 lg:gap-10">
                {/* left: price header + chart */}
                <div className="min-w-0 lg:col-span-2">
                  <div className="mb-8 flex items-end justify-between gap-6">
                    <div>
                      <div className="mb-3 flex items-center gap-2">
                        <div className="flex items-baseline gap-2">
                          <h1 className="text-[15px] font-semibold text-ink">
                            {m.market.title}
                          </h1>
                          <span className="eyebrow">binary · on-chain</span>
                        </div>
                        <StarButton marketId={params.id} />
                      </div>
                      <div className="flex items-baseline gap-3">
                        <span className="font-mono text-[46px] font-light leading-none text-ink tnum sm:text-[64px]">
                          {m.yesPrice}
                          <span className="ml-0.5 text-[22px] text-dim sm:text-[28px]">¢</span>
                        </span>
                        <span
                          className={`flex items-center gap-0.5 font-mono text-[13px] tnum ${
                            up ? "text-accent" : "text-down"
                          }`}
                        >
                          {up ? <ArrowUpRight size={14} /> : <ArrowDownRight size={14} />}
                          {Math.abs(m.priceDelta)}¢
                        </span>
                      </div>
                      <p className="mt-2.5 font-mono text-[12px] text-muted">
                        YES · implied {m.yesPrice}% ·{" "}
                        <span className="text-dim">NO {100 - m.yesPrice}¢</span>
                      </p>
                    </div>
                    <div className="hidden text-right font-mono text-[12px] text-dim sm:block">
                      <div className="text-muted tnum">
                        {"$" + (m.fills.reduce((a, f) => a + f.price * f.size, 0) / 100).toLocaleString()}
                      </div>
                      <div>session volume</div>
                    </div>
                  </div>

                  <PriceChart data={m.history} up={up} />
                </div>

                {/* right: trade panel + your position in this market */}
                <div className="lg:col-span-1">
                  <div className="space-y-5 lg:sticky lg:top-[76px]">
                    <TradePanel
                      marketId={params.id}
                      marketTitle={m.market.title}
                      yesPrice={m.yesPrice}
                      balanceMicro={m.balanceMicro}
                      marketStatus={m.market.status}
                      initialOutcome={initialOutcome}
                      onPlaced={() => {
                        m.refreshBalance();
                        setPosRefresh((n) => n + 1);
                      }}
                    />
                    <MarketPositions
                      marketId={params.id}
                      resolvedOutcome={
                        m.market.outcome?.void
                          ? "VOID"
                          : (m.market.outcome?.winner ?? null)
                      }
                      refreshKey={posRefresh}
                    />
                  </div>
                </div>
              </div>
            </section>

            <div className="rule-t rule-b">
              <PitchTicker lines={m.oneliners} index={m.onelinerIdx} />
            </div>

            {/* book + trades */}
            <div className="grid gap-10 py-10 sm:grid-cols-2 lg:gap-12">
              <OrderBook book={m.book} flashId={m.lastFillId} flashSide={m.lastFillSide} />
              <RecentFills fills={m.fills} yesPrice={m.yesPrice} />
            </div>

            {/* other markets on this fixture / involving either team */}
            <RelatedMarkets current={m.market} match={m.match} />

            {/* per-market discussion */}
            <Comments marketId={params.id} />

            <footer className="rule-t py-6 font-mono text-[11px] text-dim">
              {m.market.rule}
            </footer>
          </>
        )}
      </main>
    </div>
  );
}

function GlobalMarketHero({
  market,
}: {
  market: {
    title: string;
    scope?: string;
    competition_id?: string;
    subject_type?: string;
    subject_id?: string;
    resolution_source?: string;
  };
}) {
  const subject = market.subject_id ? titleCase(market.subject_id) : "";
  const scope = market.scope ?? "custom";
  const source =
    market.resolution_source === "manual_required"
      ? "admin-assisted resolution"
      : market.resolution_source ?? "resolution pending";

  return (
    <section className="py-7 sm:py-9">
      <div className="mb-5 flex items-center justify-between gap-3">
        <div className="eyebrow">World Cup market</div>
        <div className="min-w-0 truncate font-mono text-[11px] uppercase tracking-[0.14em] text-dim">
          {scope}
          {market.competition_id ? ` · competition ${market.competition_id}` : ""}
        </div>
      </div>

      <div className="max-w-[820px]">
        <h2 className="text-[30px] font-bold tracking-tight text-ink sm:text-[44px]">
          {market.title}
        </h2>
        <div className="mt-4 flex flex-wrap gap-x-5 gap-y-2 font-mono text-[12px] text-muted">
          {subject && (
            <span>
              <span className="text-dim">subject</span> {subject}
            </span>
          )}
          {market.subject_type && (
            <span>
              <span className="text-dim">type</span> {market.subject_type}
            </span>
          )}
          <span>
            <span className="text-dim">source</span> {source}
          </span>
        </div>
      </div>
    </section>
  );
}

function titleCase(s: string): string {
  return s
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function NotFound({ status }: { status: number }) {
  const is404 = status === 404;
  return (
    <div className="mx-auto max-w-md py-32 text-center">
      <div className="mb-4 eyebrow">Error {status}</div>
      <h1 className="mb-3 text-2xl font-semibold tracking-tight">
        {is404 ? "Market not found" : "Couldn’t load this market"}
      </h1>
      <p className="mb-8 text-[14px] leading-relaxed text-muted">
        {is404
          ? "This market doesn’t exist or was never created on-chain."
          : "The exchange didn’t respond — likely a transient network issue."}
      </p>
      <Link
        href="/"
        className="font-mono text-[13px] text-accent transition-[filter] hover:brightness-125"
      >
        ← Back to markets
      </Link>
    </div>
  );
}
