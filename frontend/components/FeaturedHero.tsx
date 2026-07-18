"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowDownRight, ArrowUpRight, ChevronLeft, ChevronRight } from "lucide-react";
import type { Market, NewsItem } from "@/lib/types";
import { useLiveMarket } from "@/lib/useLiveMarket";
import { usePitchWallet } from "@/lib/wallet";
import { FlagPair } from "./TeamFlag";

/**
 * The featured-market hero on the markets index — the pinned markets (admin
 * `featured_rank`), one at a time with ◄ ► pagination. The shown market is kept
 * live (price, delta, sparkline) via useLiveMarket, and carries its match's
 * latest real "Latest:" news blurb from the hourly Exa job.
 */
export function FeaturedHero({ featured, news }: { featured: Market[]; news: NewsItem[] }) {
  const wallet = usePitchWallet();
  const [idx, setIdx] = useState(0);
  const n = featured.length;
  const cur = Math.min(idx, n - 1);
  const market = featured[cur];

  const m = useLiveMarket(market.market_id, wallet.address);
  const up = m.priceDelta >= 0;
  const latest = news.find((x) => x.market_id === market.market_id);
  const go = (delta: number) => setIdx((i) => (((Math.min(i, n - 1) + delta) % n) + n) % n);

  return (
    <section className="rule-t rule-b py-6">
      <div className="mb-4 flex items-center justify-between">
        <span className="eyebrow">Featured</span>
        {n > 1 && (
          <div className="flex items-center gap-3 font-mono text-[11px] text-dim tnum">
            <span>
              {cur + 1} of {n}
            </span>
            <button
              onClick={() => go(-1)}
              className="rounded-full border border-line2 p-1 text-dim transition-colors hover:border-dim hover:text-ink"
              aria-label="Previous featured market"
            >
              <ChevronLeft size={14} />
            </button>
            <button
              onClick={() => go(1)}
              className="rounded-full border border-line2 p-1 text-dim transition-colors hover:border-dim hover:text-ink"
              aria-label="Next featured market"
            >
              <ChevronRight size={14} />
            </button>
          </div>
        )}
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_280px] lg:items-center">
        <div className="min-w-0">
          <Link
            href={`/market/${market.market_id}`}
            className="group flex items-start gap-3"
          >
            {m.match && <FlagPair home={m.match.home} away={m.match.away} size={26} />}
            <h2 className="text-[20px] font-bold leading-tight tracking-tight text-ink transition-colors group-hover:text-accent sm:text-[24px]">
              {market.title}
            </h2>
          </Link>

          <div className="mt-4 flex items-center gap-2">
            <Link
              href={`/market/${market.market_id}?o=yes`}
              className="flex-1 rounded-[2px] border border-accent/30 bg-accent/10 px-4 py-2.5 text-center font-mono text-[13px] font-semibold text-accent transition-colors hover:bg-accent/20"
            >
              Yes {m.yesPrice}%
            </Link>
            <Link
              href={`/market/${market.market_id}?o=no`}
              className="flex-1 rounded-[2px] border border-down/30 bg-down/10 px-4 py-2.5 text-center font-mono text-[13px] font-semibold text-down transition-colors hover:bg-down/20"
            >
              No {100 - m.yesPrice}%
            </Link>
          </div>

          {latest && (
            <a
              href={latest.url}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-4 block text-[13px] leading-relaxed text-muted transition-colors hover:text-ink"
            >
              <span className="text-dim">Latest: </span>
              <span className="line-clamp-2">{latest.summary || latest.headline}</span>
              {latest.source && (
                <span className="ml-1 font-mono text-[11px] text-dim">— {latest.source}</span>
              )}
            </a>
          )}
        </div>

        <div className="min-w-0">
          <div className="mb-1 flex items-baseline justify-between">
            <span className="font-mono text-[28px] font-light tnum text-ink">
              {m.yesPrice}
              <span className="text-[16px] text-dim">%</span>
            </span>
            <span
              className={`flex items-center gap-0.5 font-mono text-[12px] tnum ${
                up ? "text-accent" : "text-down"
              }`}
            >
              {up ? <ArrowUpRight size={13} /> : <ArrowDownRight size={13} />}
              {Math.abs(m.priceDelta)}%
            </span>
          </div>
          <Sparkline data={m.history.map((p) => p.price)} up={up} />
        </div>
      </div>
    </section>
  );
}

function Sparkline({ data, up }: { data: number[]; up: boolean }) {
  if (data.length < 2) return <div className="h-[90px]" />;
  const W = 300;
  const H = 90;
  let lo = Math.min(...data);
  let hi = Math.max(...data);
  if (hi - lo < 6) {
    const mid = (hi + lo) / 2;
    lo = mid - 3;
    hi = mid + 3;
  }
  const pts = data
    .map((p, i) => {
      const x = (i / (data.length - 1)) * W;
      const y = H - ((p - lo) / (hi - lo)) * H;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="h-[90px] w-full" aria-hidden>
      <polyline
        points={pts}
        fill="none"
        stroke={up ? "#34d399" : "#f2637e"}
        strokeWidth="2"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
