"use client";

import Link from "next/link";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import type { NewsItem } from "@/lib/types";
import { prob, relTime } from "@/lib/format";
import { FlagPair } from "./TeamFlag";

/**
 * The markets-index Breaking News panel. Each row pairs a market (question +
 * live Yes% + momentum delta from real odds) with its latest REAL article from
 * Exa — the headline links out to the actual source. Nothing here is fabricated;
 * the panel simply hides when the hourly job has produced nothing fresh.
 */
export function BreakingNews({ items }: { items: NewsItem[] }) {
  if (!items.length) return null;
  return (
    <aside aria-label="Breaking news">
      <div className="mb-3 flex items-center gap-2">
        <span
          className="h-[7px] w-[7px] rounded-full bg-down animate-live-pulse-down"
          aria-hidden
        />
        <h2 className="text-[13px] font-semibold tracking-tight text-ink">Breaking News</h2>
      </div>
      <ul>
        {items.map((n, i) => (
          <NewsRow key={n.market_id + i} n={n} />
        ))}
      </ul>
    </aside>
  );
}

function NewsRow({ n }: { n: NewsItem }) {
  const d = n.delta ?? null;
  const up = (d ?? 0) >= 0;
  return (
    <li className="rule-b py-3.5 first:border-t first:border-line">
      <div className="flex items-start gap-3">
        <span className="mt-0.5">
          <FlagPair home={n.home} away={n.away} size={17} />
        </span>
        <div className="min-w-0 flex-1">
          <Link
            href={`/market/${n.market_id}`}
            className="block truncate text-[12.5px] font-medium text-ink transition-colors hover:text-accent"
          >
            {n.question}
          </Link>
          <a
            href={n.url}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-0.5 block text-[11.5px] leading-snug text-muted transition-colors hover:text-ink"
          >
            <span className="line-clamp-2">{n.headline}</span>
          </a>
          {n.source && (
            <div className="mt-1 font-mono text-[10px] uppercase tracking-[0.1em] text-dim">
              {n.source}
              {n.published_at ? ` · ${relTime(n.published_at)}` : ""}
            </div>
          )}
        </div>
        {n.yes_pct != null && (
          <div className="shrink-0 text-right">
            <div className="font-mono text-[15px] font-semibold tnum text-ink">
              {prob(n.yes_pct)}
            </div>
            {d != null && d !== 0 && (
              <div
                className={`flex items-center justify-end gap-0.5 font-mono text-[11px] tnum ${
                  up ? "text-accent" : "text-down"
                }`}
              >
                {up ? <ArrowUpRight size={12} /> : <ArrowDownRight size={12} />}
                {Math.abs(d)}%
              </div>
            )}
          </div>
        )}
      </div>
    </li>
  );
}
