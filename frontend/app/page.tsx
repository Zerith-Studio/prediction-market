"use client";

import { useEffect, useState } from "react";
import { api, configured } from "@/lib/api";
import type { Market, Match, NewsItem } from "@/lib/types";
import { TopBar } from "@/components/TopBar";
import { FlagPair } from "@/components/TeamFlag";
import { BinaryCard, GlobalBinaryCard, PrecisionCard } from "@/components/MarketCard";
import { FeaturedHero } from "@/components/FeaturedHero";
import { BreakingNews } from "@/components/BreakingNews";
import { usePitchWallet } from "@/lib/wallet";

export default function MarketsIndex() {
  const wallet = usePitchWallet();
  const [matches, setMatches] = useState<Match[]>([]);
  const [markets, setMarkets] = useState<Market[]>([]);
  const [news, setNews] = useState<NewsItem[]>([]);
  const [balance, setBalance] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!configured()) {
      setLoading(false);
      setError("not-configured");
      return;
    }
    let alive = true;
    // Breaking news is best-effort — never block the index if it fails/empty.
    api.getBreakingNews().then((n) => alive && setNews(n)).catch(() => {});
    Promise.all([api.listMatches(), api.listMarkets(), api.getBalance(wallet.address)])
      .then(([ms, mks, bal]) => {
        if (!alive) return;
        setMatches(ms);
        setMarkets(mks);
        setBalance(bal);
        setLoading(false);
      })
      .catch((e) => {
        if (!alive) return;
        setError(e instanceof Error ? e.message : "failed to load");
        setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [wallet.address]);

  // Pinned markets (admin featured_rank), lowest rank first, drive the hero.
  const featured = markets
    .filter((m) => m.featured_rank != null && m.status !== "void")
    .sort((a, b) => (a.featured_rank ?? 0) - (b.featured_rank ?? 0));
  const fixtureMarketIds = new Set(matches.map((m) => m.id));
  const globalMarkets = markets.filter((m) => !m.match_id || !fixtureMarketIds.has(m.match_id));

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={balance} />
      <main className="mx-auto max-w-[1200px] px-5 sm:px-8">
        <div className="flex flex-col gap-1 py-8 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
          <h1 className="text-[15px] font-semibold">Matches</h1>
          <span className="eyebrow">TxLINE live data · settled on Solana devnet</span>
        </div>

        {loading && <IndexSkeleton />}
        {error === "not-configured" && <NotConfigured />}
        {error && error !== "not-configured" && (
          <p className="py-16 text-center font-mono text-[13px] text-down">{error}</p>
        )}

        {!loading && !error && matches.length === 0 && (
          <p className="py-16 text-center text-[14px] leading-relaxed text-muted">
            No fixtures in coverage right now.
            <br />
            <span className="text-dim">
              Markets appear automatically when TxLINE lists a World Cup fixture.
            </span>
          </p>
        )}

        {/* Featured hero + Breaking News, side by side (reference layout). */}
        {!loading && !error && featured.length > 0 && (
          <div className={news.length ? "grid gap-8 lg:grid-cols-[1fr_320px]" : ""}>
            <FeaturedHero featured={featured} news={news} />
            {news.length > 0 && (
              <div className="lg:rule-l lg:self-start lg:pl-8 lg:sticky lg:top-[76px]">
                <BreakingNews items={news} />
              </div>
            )}
          </div>
        )}
        {/* No pinned market yet, but there is fresh news — show it full width. */}
        {!loading && !error && featured.length === 0 && news.length > 0 && (
          <div className="rule-t py-6">
            <BreakingNews items={news} />
          </div>
        )}

        {matches.map((match) => (
          <MatchSection
            key={match.id}
            match={match}
            markets={markets.filter((m) => m.match_id === match.id)}
          />
        ))}
        {!loading && !error && globalMarkets.length > 0 && (
          <GlobalMarketsSection markets={globalMarkets} />
        )}
      </main>
    </div>
  );
}

function GlobalMarketsSection({ markets }: { markets: Market[] }) {
  const binaries = markets.filter((m) => m.type === "binary");
  const pools = markets.filter((m) => m.type === "precision");

  return (
    <section className="rule-t py-6">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h2 className="text-[17px] font-bold tracking-tight">World Cup markets</h2>
        <span className="shrink-0 font-mono text-[11px] uppercase tracking-[0.1em] text-dim">
          competition · player · custom
        </span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {binaries.map((m) => (
          <GlobalBinaryCard key={m.market_id} m={m} />
        ))}
        {pools.map((m) => (
          <PrecisionCard key={m.market_id} m={m} />
        ))}
      </div>
    </section>
  );
}

function MatchSection({ match, markets }: { match: Match; markets: Market[] }) {
  const live = match.status === "live";
  const kickoff = new Date(match.kickoff_at);
  const binaries = markets.filter((m) => m.type === "binary");
  const pools = markets.filter((m) => m.type === "precision");

  return (
    <section className="rule-t py-6">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h2 className="flex min-w-0 items-center gap-2.5 text-[17px] font-bold tracking-tight">
          <FlagPair home={match.home} away={match.away} size={22} />
          <span className="min-w-0 truncate">
            {match.home} <span className="font-normal text-dim">vs</span> {match.away}
          </span>
        </h2>
        <span className="shrink-0 font-mono text-[11px] tracking-[0.1em]">
          {live ? (
            <span className="text-down">
              <span className="mr-1.5 inline-block h-[6px] w-[6px] rounded-full bg-down align-middle animate-live-pulse-down" />
              LIVE · {match.live_state.home_score}–{match.live_state.away_score}
            </span>
          ) : match.status === "ft" ? (
            <span className="text-dim">
              FT · {match.live_state.home_score}–{match.live_state.away_score}
            </span>
          ) : (
            <span className="text-dim">
              {kickoff.toLocaleString(undefined, {
                weekday: "short",
                hour: "2-digit",
                minute: "2-digit",
              })}
            </span>
          )}
        </span>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {binaries.map((m) => (
          <BinaryCard key={m.market_id} m={m} match={match} />
        ))}
        {pools.map((m) => (
          <PrecisionCard key={m.market_id} m={m} match={match} />
        ))}
      </div>
    </section>
  );
}

function IndexSkeleton() {
  return (
    <div className="space-y-6" aria-busy="true">
      {[0, 1].map((i) => (
        <div key={i} className="rule-t space-y-4 py-6">
          <div className="h-5 w-64 animate-pulse bg-line2/50" />
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, j) => (
              <div key={j} className="h-[68px] animate-pulse rounded-[3px] bg-line2/40" />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function NotConfigured() {
  return (
    <div className="mx-auto max-w-md py-24 text-center">
      <div className="mb-4 eyebrow">Setup</div>
      <h2 className="mb-3 text-xl font-semibold tracking-tight">Exchange not connected</h2>
      <p className="text-[14px] leading-relaxed text-muted">
        Set <code className="font-mono text-accent">NEXT_PUBLIC_API_URL</code> in{" "}
        <code className="font-mono">.env.local</code> to the Go backend origin and restart.
      </p>
    </div>
  );
}
