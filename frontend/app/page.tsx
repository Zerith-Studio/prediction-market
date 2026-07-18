"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, configured } from "@/lib/api";
import { kindOf } from "@/lib/kinds";
import type { Market, Match, NewsItem } from "@/lib/types";
import { TopBar } from "@/components/TopBar";
import { FlagPair } from "@/components/TeamFlag";
import { StarButton } from "@/components/StarButton";
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
      </main>
    </div>
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
          <Link key={m.market_id} href={`/precision/${m.market_id}`} className={cardLink}>
            <span className="flex items-start gap-2.5">
              <FlagPair home={match.home} away={match.away} size={18} />
              <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors group-hover:text-accent">
                {m.title}
              </span>
            </span>
            <div className="mt-3 flex items-center justify-between gap-2">
              <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
                {kindOf(m)}
              </span>
              <span className="flex items-center gap-2">
                <StarButton marketId={m.market_id} />
                <PrecisionState market={m} />
              </span>
            </div>
          </Link>
        ))}
      </div>
    </section>
  );
}

// Market-card shells: bordered tile, subtle elevation. cardLink is the fully
// clickable variant (pools, settled markets) with a hover-lift + press feedback;
// cardBox is the static container used when the card holds its own Yes/No buttons.
const cardBase =
  "flex min-h-[68px] flex-col justify-between rounded-[3px] border border-line bg-line/40 p-4";
const cardLink = `group ${cardBase} transition-[transform,border-color,background-color] duration-150 ease-out-strong hover:-translate-y-px hover:border-line2 hover:bg-line/70 active:translate-y-0`;
const cardBox = `${cardBase} transition-colors duration-150 hover:border-line2`;

// Yes/No quick-trade buttons — accent for YES, down for NO, with press feedback.
const yesBtn =
  "rounded-[2px] border border-accent/30 bg-accent/10 px-2.5 py-1 font-mono text-[11px] font-semibold uppercase tracking-wide text-accent transition-[transform,filter,background-color] duration-150 ease-out-strong hover:bg-accent/20 hover:brightness-110 active:scale-[0.96]";
const noBtn =
  "rounded-[2px] border border-down/30 bg-down/10 px-2.5 py-1 font-mono text-[11px] font-semibold uppercase tracking-wide text-down transition-[transform,filter,background-color] duration-150 ease-out-strong hover:bg-down/20 hover:brightness-110 active:scale-[0.96]";

// BinaryCard: open markets carry Yes/No buttons that deep-link into the trade
// panel with the outcome preselected; resolved markets show the result instead.
function BinaryCard({ m, match }: { m: Market; match: Match }) {
  if (m.status === "open") {
    return (
      <div className={cardBox}>
        <Link
          href={`/market/${m.market_id}`}
          className="flex items-start gap-2.5 text-ink transition-colors hover:text-accent"
        >
          <FlagPair home={match.home} away={match.away} size={18} />
          <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug">{m.title}</span>
        </Link>
        <div className="mt-3 flex items-center justify-between gap-2">
          <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
            {kindOf(m)}
          </span>
          <div className="flex shrink-0 items-center gap-1.5">
            <StarButton marketId={m.market_id} />
            <Link href={`/market/${m.market_id}?o=yes`} className={yesBtn}>
              Yes
            </Link>
            <Link href={`/market/${m.market_id}?o=no`} className={noBtn}>
              No
            </Link>
          </div>
        </div>
      </div>
    );
  }
  return (
    <Link href={`/market/${m.market_id}`} className={cardLink}>
      <span className="flex items-start gap-2.5">
        <FlagPair home={match.home} away={match.away} size={18} />
        <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors group-hover:text-accent">
          {m.title}
        </span>
      </span>
      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
          {kindOf(m)}
        </span>
        <span className="flex items-center gap-2">
          <StarButton marketId={m.market_id} />
          <MarketState market={m} />
        </span>
      </div>
    </Link>
  );
}

function MarketState({ market }: { market: Market }) {
  if (market.status === "settled") {
    const w = market.outcome?.winner;
    return (
      <span className={`shrink-0 font-mono text-[11px] ${w === "YES" ? "text-accent" : "text-down"}`}>
        {w}
      </span>
    );
  }
  if (market.status === "void") {
    return <span className="shrink-0 font-mono text-[11px] text-dim">VOID</span>;
  }
  return (
    <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-dim">
      {market.status}
    </span>
  );
}

// PrecisionState mirrors MarketState for pools: a settled pool shows its winning
// value, a kickoff-locked pool shows "locked", an open pool shows "pool".
function PrecisionState({ market }: { market: Market }) {
  if (market.status === "settled" && market.outcome?.value != null) {
    return (
      <span className="shrink-0 font-mono text-[11px] text-accent tnum">
        {market.outcome.value}
      </span>
    );
  }
  if (market.status === "void") {
    return <span className="shrink-0 font-mono text-[11px] text-dim">VOID</span>;
  }
  if (market.status === "closed") {
    return (
      <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-dim">
        locked
      </span>
    );
  }
  return (
    <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-accent/70">
      pool
    </span>
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
