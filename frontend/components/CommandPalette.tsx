"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, configured } from "@/lib/api";
import { kindOf } from "@/lib/kinds";
import type { Market, Match } from "@/lib/types";

interface Result {
  market: Market;
  match?: Match;
  href: string;
}

// Global market search. Opened by TopBar (⌘K / Ctrl+K or the Search button);
// fetches a fresh markets+matches snapshot on each open and filters client-side.
export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const [markets, setMarkets] = useState<Market[]>([]);
  const [matches, setMatches] = useState<Match[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setSelected(0);
    if (!configured()) {
      setError("not-configured");
      return;
    }
    let alive = true;
    setLoading(true);
    setError(null);
    Promise.all([api.listMarkets(), api.listMatches()])
      .then(([mks, ms]) => {
        if (!alive) return;
        setMarkets(mks);
        setMatches(ms);
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
  }, [open]);

  useEffect(() => {
    if (open && !loading) inputRef.current?.focus();
  }, [open, loading]);

  // Global Escape: the dialog's onKeyDown only fires when focus is inside it,
  // which isn't guaranteed (e.g. while loading the input isn't focused yet).
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  const matchById = useMemo(() => new Map(matches.map((m) => [m.id, m])), [matches]);

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    const hit = (m: Market): boolean => {
      if (!q) return true;
      const match = matchById.get(m.match_id);
      const hay = [m.title, m.rule, kindOf(m), m.type, match?.home ?? "", match?.away ?? ""]
        .join(" ")
        .toLowerCase();
      return q.split(/\s+/).every((word) => hay.includes(word));
    };
    const toResult = (m: Market): Result => ({
      market: m,
      match: matchById.get(m.match_id),
      // Precision pools have their own page; everything else is /market/[id].
      href: m.type === "precision" ? `/precision/${m.market_id}` : `/market/${m.market_id}`,
    });
    return {
      binary: markets.filter((m) => m.type === "binary" && hit(m)).map(toResult),
      precision: markets.filter((m) => m.type === "precision" && hit(m)).map(toResult),
    };
  }, [markets, matchById, query]);

  const flat = useMemo(() => [...results.binary, ...results.precision], [results]);
  const sel = Math.min(selected, Math.max(flat.length - 1, 0));

  useEffect(() => setSelected(0), [query]);

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-idx="${sel}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  if (!open) return null;

  const go = (r: Result) => {
    onClose();
    router.push(r.href);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected(Math.min(sel + 1, flat.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected(Math.max(sel - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const r = flat[sel];
      if (r) go(r);
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  };

  return (
    <div
      className="fixed inset-0 z-50"
      role="dialog"
      aria-modal="true"
      aria-label="Search markets"
      onKeyDown={onKeyDown}
    >
      <div className="absolute inset-0 bg-black/60 backdrop-blur-[2px]" onClick={onClose} />
      <div className="absolute left-1/2 top-[16vh] w-[min(600px,calc(100vw-2rem))] -translate-x-1/2 rounded-[3px] border border-line2 bg-bg shadow-2xl">
        <div className="flex items-center gap-3 rule-b px-4">
          <SearchIcon className="shrink-0 text-dim" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search markets, teams…"
            aria-label="Search markets"
            className="h-12 w-full bg-transparent text-[14px] text-ink placeholder:text-dim focus:outline-none"
          />
          <kbd className="shrink-0 rounded-[2px] border border-line2 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
            esc
          </kbd>
        </div>

        <div ref={listRef} className="max-h-[52vh] overflow-y-auto py-2">
          {error === "not-configured" && (
            <p className="px-4 py-8 text-center text-[13px] leading-relaxed text-muted">
              Exchange not connected — set{" "}
              <code className="font-mono text-accent">NEXT_PUBLIC_API_URL</code> first.
            </p>
          )}
          {error && error !== "not-configured" && (
            <p className="px-4 py-8 text-center font-mono text-[12.5px] text-down">{error}</p>
          )}
          {loading && (
            <div className="space-y-2 px-4 py-3" aria-busy="true">
              {[0, 1, 2].map((i) => (
                <div key={i} className="h-9 animate-pulse rounded-[2px] bg-line2/40" />
              ))}
            </div>
          )}

          {!loading && !error && flat.length === 0 && (
            <p className="px-4 py-8 text-center text-[13px] text-muted">
              No markets match{" "}
              <span className="font-mono text-ink">&ldquo;{query.trim()}&rdquo;</span>
            </p>
          )}

          {!loading && !error && (
            <>
              <Group
                label="Markets"
                items={results.binary}
                startIdx={0}
                sel={sel}
                setSelected={setSelected}
                go={go}
              />
              <Group
                label="Precision"
                items={results.precision}
                startIdx={results.binary.length}
                sel={sel}
                setSelected={setSelected}
                go={go}
              />
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function Group({
  label,
  items,
  startIdx,
  sel,
  setSelected,
  go,
}: {
  label: string;
  items: Result[];
  startIdx: number;
  sel: number;
  setSelected: (i: number) => void;
  go: (r: Result) => void;
}) {
  if (items.length === 0) return null;
  return (
    <div className="py-1">
      <div className="eyebrow px-4 py-1.5">{label}</div>
      {items.map((r, i) => {
        const idx = startIdx + i;
        const active = idx === sel;
        return (
          <button
            key={r.market.market_id}
            type="button"
            data-idx={idx}
            role="option"
            aria-selected={active}
            onClick={() => go(r)}
            onMouseMove={() => setSelected(idx)}
            className={`flex w-full items-center justify-between gap-4 px-4 py-2.5 text-left transition-colors duration-75 ${
              active ? "bg-line/70" : ""
            }`}
          >
            <div className="min-w-0">
              <div className={`truncate text-[13.5px] ${active ? "text-ink" : "text-muted"}`}>
                {r.market.title}
              </div>
              {r.match && (
                <div className="mt-0.5 truncate font-mono text-[10.5px] uppercase tracking-[0.1em] text-dim">
                  {r.match.home} vs {r.match.away}
                  {r.match.status === "live" && (
                    <span className="ml-2 text-down">
                      live · {r.match.live_state.home_score}–{r.match.live_state.away_score}
                    </span>
                  )}
                </div>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-2.5 font-mono text-[10px] uppercase tracking-[0.12em]">
              {r.market.status !== "open" && (
                <span className={r.market.status === "settled" ? "text-accent/70" : "text-dim"}>
                  {r.market.status}
                </span>
              )}
              <span className="text-dim">{kindOf(r.market)}</span>
            </div>
          </button>
        );
      })}
    </div>
  );
}

export function SearchIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      aria-hidden="true"
    >
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
  );
}
