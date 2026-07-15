"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, configured } from "@/lib/api";
import type { Market, Match } from "@/lib/types";
import { TopBar } from "@/components/TopBar";
import { usePitchWallet } from "@/lib/wallet";

export default function MarketsIndex() {
  const wallet = usePitchWallet();
  const [matches, setMatches] = useState<Match[]>([]);
  const [markets, setMarkets] = useState<Market[]>([]);
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

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={balance} />
      <main className="mx-auto max-w-[1200px] px-5 sm:px-8">
        <div className="flex items-baseline justify-between py-8">
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
      <div className="mb-4 flex items-baseline justify-between gap-4">
        <h2 className="min-w-0 truncate text-[17px] font-bold tracking-tight">
          {match.home} <span className="font-normal text-dim">vs</span> {match.away}
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

      <div className="grid gap-x-8 sm:grid-cols-2 lg:grid-cols-3">
        {binaries.map((m) => (
          <Link
            key={m.market_id}
            href={`/market/${m.market_id}`}
            className="group flex items-baseline justify-between gap-3 border-b border-line py-2.5 transition-colors hover:border-line2"
          >
            <span className="min-w-0 truncate text-[13.5px] text-ink group-hover:text-accent">
              {m.title}
            </span>
            <MarketState market={m} />
          </Link>
        ))}
        {pools.map((m) => (
          <Link
            key={m.market_id}
            href={`/precision/${m.market_id}`}
            className="group flex items-baseline justify-between gap-3 border-b border-line py-2.5 transition-colors hover:border-line2"
          >
            <span className="min-w-0 truncate text-[13.5px] text-ink group-hover:text-accent">
              {m.title}
            </span>
            <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-dim">
              pool
            </span>
          </Link>
        ))}
      </div>
    </section>
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

function IndexSkeleton() {
  return (
    <div className="space-y-6" aria-busy="true">
      {[0, 1].map((i) => (
        <div key={i} className="rule-t space-y-3 py-6">
          <div className="h-5 w-64 animate-pulse bg-line2/50" />
          <div className="grid gap-x-8 gap-y-2 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, j) => (
              <div key={j} className="h-8 animate-pulse bg-line2/40" />
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
