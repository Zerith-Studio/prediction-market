"use client";

// Combo builder (RFQ, ADR 0004): pick 2–6 legs from open binary markets —
// mutually exclusive legs grey out — set a stake, request a quote. The MM bot
// answers over WS with a signed quote that counts down to expiry; accepting it
// escrows the pot. Off-chain negotiation, on-chain-grade single-use accept.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { api, configured, wsUrl, type RFQQuote } from "@/lib/api";
import type { Market, Match } from "@/lib/types";
import { usd } from "@/lib/format";
import { TopBar } from "@/components/TopBar";
import { usePitchWallet } from "@/lib/wallet";

// Mutex groups mirror backend/internal/templates (the API enforces; the UI teaches).
const MUTEX: Record<string, string> = {
  home_win: "result",
  draw: "result",
  away_win: "result",
  dnb_home: "result_dnb",
  over_2_5: "total_goals",
  ou_1h_075: "h1_goals",
};

type Leg = { market: Market; outcome: 1 };

export default function CombosPage() {
  const wallet = usePitchWallet();
  const [markets, setMarkets] = useState<Market[]>([]);
  const [matches, setMatches] = useState<Match[]>([]);
  const [balance, setBalance] = useState(0);
  const [legs, setLegs] = useState<Leg[]>([]);
  const [stake, setStake] = useState("5");
  const [rfqId, setRfqId] = useState<string | null>(null);
  const [quote, setQuote] = useState<RFQQuote | null>(null);
  const [state, setState] = useState<"build" | "waiting" | "quoted" | "accepted">("build");
  const [error, setError] = useState<string | null>(null);
  const [acceptTx, setAcceptTx] = useState("");
  const pollRef = useRef<number | null>(null);

  useEffect(() => {
    if (!configured()) return;
    Promise.all([api.listMarkets("open"), api.listMatches(), api.getBalance(wallet.address)])
      .then(([mk, ms, b]) => {
        setMarkets(mk.filter((m) => m.type === "binary"));
        setMatches(ms);
        setBalance(b);
      })
      .catch((e) => setError(e.message));
  }, [wallet.address]);

  // A candidate is blocked when a picked leg shares its mutex group + match.
  const blocked = useMemo(() => {
    const taken = new Set(
      legs
        .map((l) => MUTEX[l.market.template_key] && `${l.market.match_id}:${MUTEX[l.market.template_key]}`)
        .filter(Boolean)
    );
    return (m: Market) => {
      if (legs.some((l) => l.market.market_id === m.market_id)) return false; // toggles off
      const g = MUTEX[m.template_key];
      return g ? taken.has(`${m.match_id}:${g}`) : false;
    };
  }, [legs]);

  const toggle = (m: Market) => {
    setError(null);
    setLegs((ls) =>
      ls.some((l) => l.market.market_id === m.market_id)
        ? ls.filter((l) => l.market.market_id !== m.market_id)
        : ls.length < 6
          ? [...ls, { market: m, outcome: 1 }]
          : ls
    );
  };

  const stakeMicro = Math.max(0, Math.floor(Number(stake) || 0)) * 1_000_000;

  async function requestQuote() {
    if (!wallet.address) {
      wallet.connect();
      return;
    }
    setError(null);
    setState("waiting");
    try {
      const { rfq_id } = await api.createRFQ(
        wallet.address,
        legs.map((l) => ({ market_id: l.market.market_id, outcome: l.outcome })),
        stakeMicro
      );
      setRfqId(rfq_id);
      // Poll for the bot's quote (it also lands via WS combo_quote; polling
      // keeps this page self-sufficient).
      pollRef.current = window.setInterval(async () => {
        const { quotes } = await api.getRFQ(rfq_id);
        const open = quotes.find((q) => q.status === "open");
        if (open) {
          setQuote(open);
          setState("quoted");
          if (pollRef.current) clearInterval(pollRef.current);
        }
      }, 1000);
    } catch (e) {
      setState("build");
      setError(e instanceof Error ? e.message : "quote request failed");
    }
  }

  async function accept() {
    if (!rfqId || !quote || !wallet.address) return;
    setError(null);
    try {
      const res = await api.acceptQuote(rfqId, quote.quote_hash, wallet.address);
      setAcceptTx(res.accept_tx);
      setState("accepted");
    } catch (e) {
      setError(e instanceof Error ? e.message : "accept failed");
    }
  }

  const reset = useCallback(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    setLegs([]);
    setRfqId(null);
    setQuote(null);
    setAcceptTx("");
    setState("build");
  }, []);

  useEffect(() => () => reset(), [reset]);

  const matchName = (id: string) => {
    const m = matches.find((x) => x.id === id);
    return m ? `${m.home} vs ${m.away}` : "";
  };

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={balance} />
      <main className="mx-auto max-w-[1200px] px-5 sm:px-8">
        <div className="flex items-baseline justify-between py-8">
          <h1 className="text-[15px] font-semibold">Combo builder</h1>
          <span className="eyebrow">RFQ · quoted by the market maker · escrowed pot</span>
        </div>

        <div className="grid gap-10 lg:grid-cols-[1fr_320px]">
          {/* leg picker */}
          <section>
            {markets.length === 0 && (
              <p className="py-12 text-[13px] text-muted">No open binary markets right now.</p>
            )}
            {matches.map((match) => {
              const ms = markets.filter((m) => m.match_id === match.id);
              if (!ms.length) return null;
              return (
                <div key={match.id} className="rule-t py-4">
                  <div className="mb-2 eyebrow">
                    {match.home} vs {match.away}
                  </div>
                  <div className="grid gap-x-8 sm:grid-cols-2">
                    {ms.map((m) => {
                      const picked = legs.some((l) => l.market.market_id === m.market_id);
                      const isBlocked = blocked(m);
                      return (
                        <button
                          key={m.market_id}
                          onClick={() => !isBlocked && toggle(m)}
                          disabled={isBlocked}
                          aria-pressed={picked}
                          className={`flex items-baseline justify-between gap-3 border-b py-2.5 text-left transition-colors ${
                            picked
                              ? "border-accent"
                              : isBlocked
                                ? "cursor-not-allowed border-line opacity-30"
                                : "border-line hover:border-line2"
                          }`}
                        >
                          <span className={`min-w-0 truncate text-[13.5px] ${picked ? "text-accent" : "text-ink"}`}>
                            {m.title} <span className="text-dim">· YES</span>
                          </span>
                          {picked && <span className="font-mono text-[11px] text-accent">✓</span>}
                          {isBlocked && (
                            <span className="font-mono text-[10px] uppercase tracking-[0.1em] text-dim">
                              exclusive
                            </span>
                          )}
                        </button>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </section>

          {/* slip */}
          <aside className="lg:rule-l lg:pl-10">
            <div className="lg:sticky lg:top-[76px]">
              <h2 className="mb-5 text-[13px] font-semibold">Your combo</h2>

              {legs.length === 0 && (
                <p className="text-[13px] leading-relaxed text-muted">
                  Pick 2–6 legs. All must hit for the combo to pay.
                  <br />
                  <span className="text-dim">Mutually exclusive legs grey out.</span>
                </p>
              )}

              <AnimatePresence initial={false}>
                {legs.map((l) => (
                  <motion.div
                    key={l.market.market_id}
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={{ opacity: 0, height: 0, transition: { duration: 0.12 } }}
                    transition={{ duration: 0.18, ease: [0.23, 1, 0.32, 1] }}
                    className="overflow-hidden"
                  >
                    <div className="flex items-baseline justify-between gap-2 border-b border-line py-2">
                      <div className="min-w-0">
                        <div className="truncate text-[13px] text-ink">{l.market.title}</div>
                        <div className="truncate font-mono text-[10.5px] text-dim">
                          {matchName(l.market.match_id)}
                        </div>
                      </div>
                      <button
                        onClick={() => toggle(l.market)}
                        className="font-mono text-[11px] text-dim transition-colors hover:text-down"
                        disabled={state !== "build"}
                      >
                        remove
                      </button>
                    </div>
                  </motion.div>
                ))}
              </AnimatePresence>

              {legs.length > 0 && state === "build" && (
                <>
                  <label className="mb-5 mt-6 block">
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
                    onClick={requestQuote}
                    disabled={legs.length < 2 || stakeMicro === 0}
                    className="w-full bg-accent px-5 py-3.5 text-[14px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 enabled:active:scale-[0.98] disabled:bg-line2 disabled:text-dim"
                  >
                    {wallet.address ? `Request quote · ${legs.length} legs` : "Connect wallet"}
                  </button>
                </>
              )}

              {state === "waiting" && (
                <p className="mt-6 font-mono text-[12.5px] text-muted">
                  Waiting for the market maker…
                </p>
              )}

              {state === "quoted" && quote && (
                <QuoteCard
                  quote={quote}
                  stakeMicro={stakeMicro}
                  onAccept={accept}
                  onExpired={() => {
                    setQuote(null);
                    setState("build");
                    setError("Quote expired — request a fresh one.");
                  }}
                />
              )}

              {state === "accepted" && (
                <div className="mt-6">
                  <p className="mb-2 font-mono text-[13px] text-accent">
                    ✓ Combo escrowed{quote ? ` — pays ${usd(quote.payout)}` : ""}
                  </p>
                  <p className="font-mono text-[11.5px] leading-relaxed text-dim">
                    {acceptTx
                      ? "Escrowed on-chain."
                      : "Escrowed by the exchange; resolves from the same on-chain outcomes as every market."}{" "}
                    Track it in your portfolio.
                  </p>
                  <button
                    onClick={reset}
                    className="mt-4 font-mono text-[12px] text-accent hover:brightness-110"
                  >
                    Build another →
                  </button>
                </div>
              )}

              {error && (
                <p className="mt-4 font-mono text-[12px] text-down" role="alert">
                  {error}
                </p>
              )}
            </div>
          </aside>
        </div>
      </main>
    </div>
  );
}

function QuoteCard({
  quote,
  stakeMicro,
  onAccept,
  onExpired,
}: {
  quote: RFQQuote;
  stakeMicro: number;
  onAccept: () => void;
  onExpired: () => void;
}) {
  const [left, setLeft] = useState(() =>
    Math.max(0, Math.floor((Date.parse(quote.expiry) - Date.now()) / 1000))
  );
  useEffect(() => {
    const t = setInterval(() => {
      const s = Math.max(0, Math.floor((Date.parse(quote.expiry) - Date.now()) / 1000));
      setLeft(s);
      if (s === 0) onExpired();
    }, 250);
    return () => clearInterval(t);
  }, [quote.expiry, onExpired]);

  const multiple = stakeMicro > 0 ? quote.payout / stakeMicro : 0;

  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2, ease: [0.23, 1, 0.32, 1] }}
      className="mt-6"
    >
      <dl className="space-y-2.5 font-mono text-[12.5px]">
        <div className="flex justify-between">
          <dt className="text-dim">Stake</dt>
          <dd className="text-ink tnum">{usd(quote.stake)}</dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-dim">Pays</dt>
          <dd className="text-accent tnum">
            {usd(quote.payout)} <span className="text-dim">({multiple.toFixed(2)}×)</span>
          </dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-dim">Expires</dt>
          <dd className={`tnum ${left <= 5 ? "text-down" : "text-muted"}`}>{left}s</dd>
        </div>
      </dl>
      <button
        onClick={onAccept}
        className="mt-4 w-full bg-accent px-5 py-3.5 text-[14px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 active:scale-[0.98]"
      >
        Accept quote
      </button>
    </motion.div>
  );
}
