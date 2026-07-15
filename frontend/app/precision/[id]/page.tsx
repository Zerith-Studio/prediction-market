"use client";

// Precision pool (ADR 0006): guess the number, one entry per wallet, entries
// close at kickoff (non-negotiable), σ-normalized closeness scoring pays from
// the pool. The leaderboard doubles as a live entry distribution.

import { useCallback, useEffect, useMemo, useState } from "react";
import { api, configured, type PrecisionEntry } from "@/lib/api";
import type { Market, Match } from "@/lib/types";
import { usd } from "@/lib/format";
import { TopBar } from "@/components/TopBar";
import { usePitchWallet } from "@/lib/wallet";

export default function PrecisionPage({ params }: { params: { id: string } }) {
  const wallet = usePitchWallet();
  const [market, setMarket] = useState<Market | null>(null);
  const [match, setMatch] = useState<Match | null>(null);
  const [entries, setEntries] = useState<PrecisionEntry[]>([]);
  const [status, setStatus] = useState("open");
  const [balance, setBalance] = useState(0);
  const [guess, setGuess] = useState("");
  const [stake, setStake] = useState("2");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [entered, setEntered] = useState(false);

  const load = useCallback(async () => {
    const [m, lb, bal] = await Promise.all([
      api.getMarket(params.id),
      api.leaderboard(params.id),
      api.getBalance(wallet.address),
    ]);
    setMarket(m);
    setEntries(lb.entries);
    setStatus(lb.status);
    setBalance(bal);
    if (wallet.address && lb.entries.some((e) => e.user === wallet.address)) setEntered(true);
    try {
      setMatch(await api.getMatch(m.match_id));
    } catch {
      /* match optional for render */
    }
  }, [params.id, wallet.address]);

  useEffect(() => {
    if (!configured()) return;
    load().catch((e) => setError(e.message));
    const t = setInterval(() => load().catch(() => {}), 5000);
    return () => clearInterval(t);
  }, [load]);

  const pool = useMemo(() => entries.reduce((a, e) => a + e.stake, 0), [entries]);
  const open = status === "open";
  const settled = status === "settled" || status === "void";
  const stakeMicro = Math.max(0, Math.floor(Number(stake) || 0)) * 1_000_000;

  async function enter() {
    if (!wallet.address) {
      wallet.connect();
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await api.enterPrecision(params.id, wallet.address, Number(guess), stakeMicro);
      setEntered(true);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "entry failed");
    } finally {
      setBusy(false);
    }
  }

  // Histogram buckets for the distribution strip.
  const distribution = useMemo(() => {
    if (entries.length < 2) return [];
    const guesses = entries.map((e) => e.guess);
    const lo = Math.min(...guesses);
    const hi = Math.max(...guesses);
    const n = Math.min(24, Math.max(8, Math.ceil((hi - lo) / Math.max(1, (hi - lo) / 16))));
    const buckets = new Array(n).fill(0);
    for (const g of guesses) {
      const i = hi === lo ? 0 : Math.min(n - 1, Math.floor(((g - lo) / (hi - lo)) * n));
      buckets[i]++;
    }
    const max = Math.max(...buckets);
    return buckets.map((b) => b / max);
  }, [entries]);

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={balance} />
      <main className="mx-auto max-w-[900px] px-5 sm:px-8">
        <div className="py-8">
          <div className="mb-1 flex items-baseline justify-between gap-4">
            <h1 className="min-w-0 truncate text-[15px] font-semibold">
              {market?.title ?? "…"}
            </h1>
            <span className="eyebrow">
              {open ? "entries close at kickoff" : settled ? "settled" : "locked — in play"}
            </span>
          </div>
          {match && (
            <p className="font-mono text-[12px] text-dim">
              {match.home} vs {match.away}
            </p>
          )}
        </div>

        <section className="rule-t grid gap-10 py-8 sm:grid-cols-[1fr_280px]">
          <div>
            <div className="mb-8 flex items-baseline gap-8">
              <div>
                <div className="font-mono text-[38px] font-light leading-none text-ink tnum">
                  {usd(pool)}
                </div>
                <div className="mt-1.5 font-mono text-[11px] text-dim">
                  pool · {entries.length} entries
                </div>
              </div>
              {settled && market?.outcome?.value !== undefined && (
                <div>
                  <div className="font-mono text-[38px] font-light leading-none text-accent tnum">
                    {market.outcome.value}
                  </div>
                  <div className="mt-1.5 font-mono text-[11px] text-dim">actual</div>
                </div>
              )}
            </div>

            {distribution.length > 0 && (
              <div className="mb-8">
                <div className="mb-2 eyebrow">Entry distribution</div>
                <div className="flex h-16 items-end gap-[2px]">
                  {distribution.map((h, i) => (
                    <span
                      key={i}
                      className="flex-1 bg-accent/25"
                      style={{ height: `${Math.max(4, h * 100)}%` }}
                    />
                  ))}
                </div>
              </div>
            )}

            <div className="mb-2 eyebrow">Leaderboard</div>
            <div className="font-mono text-[12.5px]">
              <div className="grid grid-cols-[2rem_1fr_auto_auto_auto] gap-3 pb-2 eyebrow">
                <span>#</span>
                <span>Wallet</span>
                <span className="text-right">Guess</span>
                <span className="text-right">Stake</span>
                <span className="text-right">{settled ? "Payout" : ""}</span>
              </div>
              {entries.slice(0, 15).map((e, i) => {
                const mine = e.user === wallet.address;
                return (
                  <div
                    key={e.user}
                    className={`grid grid-cols-[2rem_1fr_auto_auto_auto] gap-3 border-b border-line py-[5px] ${
                      mine ? "text-accent" : ""
                    }`}
                  >
                    <span className="text-dim tnum">{i + 1}</span>
                    <span className={`truncate ${mine ? "" : "text-muted"}`}>
                      {mine ? "you" : `${e.user.slice(0, 4)}…${e.user.slice(-4)}`}
                    </span>
                    <span className="text-right text-ink tnum">{e.guess}</span>
                    <span className="text-right text-muted tnum">{usd(e.stake)}</span>
                    <span className="text-right text-accent tnum">
                      {e.payout !== undefined && e.payout > 0 ? usd(e.payout) : ""}
                    </span>
                  </div>
                );
              })}
              {entries.length === 0 && (
                <p className="py-6 text-[13px] text-muted">
                  No entries yet — be the first in the pool.
                </p>
              )}
            </div>
          </div>

          {/* entry panel */}
          <aside>
            <h2 className="mb-5 text-[13px] font-semibold">Your entry</h2>
            {entered ? (
              <p className="font-mono text-[13px] leading-relaxed text-accent">
                ✓ You're in.
                <br />
                <span className="text-dim">
                  One entry per wallet — closest to the actual number takes the biggest share.
                </span>
              </p>
            ) : !open ? (
              <p className="font-mono text-[13px] text-dim">
                {settled ? "Pool settled." : "Entries closed at kickoff."}
              </p>
            ) : (
              <>
                <label className="mb-5 block">
                  <span className="mb-1 flex justify-between font-mono text-[11px] text-muted">
                    <span>Your guess</span>
                  </span>
                  <div className="flex items-baseline border-b border-line2 pb-1.5 focus-within:border-accent">
                    <input
                      inputMode="numeric"
                      value={guess}
                      placeholder="0"
                      onChange={(e) => setGuess(e.target.value.replace(/[^0-9]/g, ""))}
                      className="w-full bg-transparent font-mono text-[22px] font-light text-ink outline-none tnum placeholder:text-dim"
                    />
                  </div>
                </label>
                <label className="mb-6 block">
                  <span className="mb-1 flex justify-between font-mono text-[11px] text-muted">
                    <span>Stake</span>
                    <span className="text-dim">USDC</span>
                  </span>
                  <div className="flex items-baseline border-b border-line2 pb-1.5 focus-within:border-accent">
                    <input
                      inputMode="numeric"
                      value={stake}
                      onChange={(e) => setStake(e.target.value.replace(/[^0-9]/g, ""))}
                      className="w-full bg-transparent font-mono text-[22px] font-light text-ink outline-none tnum"
                    />
                  </div>
                </label>
                <button
                  onClick={enter}
                  disabled={busy || guess === "" || stakeMicro === 0}
                  className="w-full bg-accent px-5 py-3.5 text-[14px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 enabled:active:scale-[0.98] disabled:bg-line2 disabled:text-dim"
                >
                  {!wallet.address ? "Connect wallet" : busy ? "Entering…" : "Enter pool"}
                </button>
                {error && (
                  <p className="mt-3 font-mono text-[12px] text-down" role="alert">
                    {error}
                  </p>
                )}
                <p className="mt-3 font-mono text-[11px] leading-relaxed text-dim">
                  Scoring: 1/(1+|guess−actual|/s)² — closeness pays, exact hits pay most.
                </p>
              </>
            )}
          </aside>
        </section>
      </main>
    </div>
  );
}
