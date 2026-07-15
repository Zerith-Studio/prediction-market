"use client";

import Link from "next/link";
import { useLiveMarket } from "@/lib/useLiveMarket";
import { TopBar } from "@/components/TopBar";
import { MatchHero } from "@/components/MatchHero";
import { OddsMeter } from "@/components/OddsMeter";
import { OrderBook } from "@/components/OrderBook";
import { RecentFills } from "@/components/RecentFills";
import { TradePanel } from "@/components/TradePanel";
import { PitchTicker } from "@/components/PitchTicker";
import { MarketSkeleton } from "@/components/Skeletons";

export default function MarketPage({ params }: { params: { id: string } }) {
  const m = useLiveMarket(params.id);

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={m.balanceMicro} />
      <main className="mx-auto max-w-[1180px] px-5 py-5 sm:py-6">
        {m.loading && <MarketSkeleton />}

        {!m.loading && m.errorStatus && (
          <NotFound status={m.errorStatus} />
        )}

        {!m.loading && !m.errorStatus && m.market && m.match && m.book && (
          <div className="space-y-4">
            <MatchHero match={m.match} />

            <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
              {/* left: market read + book */}
              <div className="space-y-4">
                <div className="panel p-5">
                  <div className="mb-4 flex items-start justify-between gap-4">
                    <div>
                      <h1 className="text-[17px] font-extrabold tracking-tight">
                        {m.market.title}
                      </h1>
                      <p className="mt-1 max-w-[60ch] text-[12.5px] leading-relaxed text-muted">
                        {m.market.rule}
                      </p>
                    </div>
                    <span className="shrink-0 rounded-md border border-line2 px-2 py-1 font-mono text-[10px] uppercase tracking-[0.1em] text-dim">
                      binary · on-chain
                    </span>
                  </div>
                  <OddsMeter
                    yesPrice={m.yesPrice}
                    priceDelta={m.priceDelta}
                    volumeMicro={18_400_000_000}
                  />
                </div>

                <PitchTicker lines={m.oneliners} index={m.onelinerIdx} />

                <div className="grid gap-4 sm:grid-cols-2">
                  <OrderBook
                    book={m.book}
                    flashId={m.lastFillId}
                    flashSide={m.lastFillSide}
                  />
                  <RecentFills fills={m.fills} yesPrice={m.yesPrice} />
                </div>
              </div>

              {/* right: sticky trade panel */}
              <div className="lg:sticky lg:top-[76px] lg:self-start">
                <TradePanel
                  yesPrice={m.yesPrice}
                  balanceMicro={m.balanceMicro}
                  marketStatus={m.market.status}
                />
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function NotFound({ status }: { status: number }) {
  const is404 = status === 404;
  return (
    <div className="mx-auto max-w-md py-24 text-center">
      <div className="mb-4 font-mono text-[13px] text-dim">ERROR {status}</div>
      <h1 className="mb-2 text-2xl font-extrabold tracking-tight">
        {is404 ? "Market not found" : "Couldn’t load this market"}
      </h1>
      <p className="mb-6 text-[14px] leading-relaxed text-muted">
        {is404
          ? "This market doesn’t exist or was never created on-chain."
          : "The exchange didn’t respond. It may be a transient network issue."}
      </p>
      <Link
        href="/"
        className="inline-flex rounded-lg border border-line2 bg-panel2 px-4 py-2.5 text-[13px] font-bold text-ink transition-colors hover:border-dim"
      >
        Back to markets
      </Link>
    </div>
  );
}
