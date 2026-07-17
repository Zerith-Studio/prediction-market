"use client";

// Market-maker desk: answer open combo RFQs by hand (a human MM, not just the
// bot). Lists open RFQs; for each you set a payout, sign a ComboQuote with your
// wallet (ed25519 over borshComboQuote — byte-identical to the backend), and
// submit it. The taker sees it counting down on /combos and can accept. Reach
// this by URL — it's a market-maker tool, not part of the taker nav.

import { useCallback, useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Loader2 } from "lucide-react";
import Link from "next/link";
import bs58 from "bs58";
import { usePitchWallet } from "@/lib/wallet";
import { borshComboQuote, fromHex, randomSalt, toHex } from "@/lib/borsh";
import { api, configured, type OpenRFQ } from "@/lib/api";
import { usd } from "@/lib/format";
import { TopBar } from "@/components/TopBar";

const ease = [0.23, 1, 0.32, 1] as const;
const primaryBtn =
  "bg-accent px-5 py-3 text-[13px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 enabled:active:scale-[0.98] disabled:bg-line2 disabled:text-dim";

function ago(iso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - Date.parse(iso)) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  return `${Math.floor(s / 3600)}h ago`;
}

export default function MarketMakerPage() {
  const wallet = usePitchWallet();
  const [rfqs, setRfqs] = useState<OpenRFQ[]>([]);
  const [balanceMicro, setBalanceMicro] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const [list, bal] = await Promise.all([
        api.listOpenRFQs(),
        api.getBalance(wallet.address),
      ]);
      setRfqs(list);
      setBalanceMicro(bal);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [wallet.address]);

  useEffect(() => {
    if (!configured()) {
      setLoading(false);
      return;
    }
    load();
    const t = setInterval(load, 4000);
    return () => clearInterval(t);
  }, [load]);

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={balanceMicro} />
      <main className="mx-auto max-w-[820px] px-5 sm:px-8">
        <div className="flex items-baseline justify-between py-8">
          <h1 className="flex items-baseline gap-2.5 text-[15px] font-semibold">
            Market maker
            {wallet.address && rfqs.length > 0 && (
              <span className="font-mono text-[11px] font-normal text-dim tnum">
                {rfqs.length} open
              </span>
            )}
          </h1>
          <span className="eyebrow">answer combo RFQs · signed quotes</span>
        </div>

        {!wallet.address ? (
          <div className="rule-t py-10">
            <p className="mb-5 max-w-[540px] text-[13px] leading-relaxed text-muted">
              Connect a wallet to make markets. You set the payout on a combo; if the
              taker accepts, you post collateral and win their stake if the combo
              misses. The quote is ed25519-signed — nothing is escrowed until it&apos;s
              accepted.
            </p>
            <button onClick={wallet.connect} disabled={!wallet.ready} className={primaryBtn}>
              Connect wallet
            </button>
          </div>
        ) : (
          <>
            {error && (
              <p role="alert" className="mb-4 font-mono text-[12px] text-down">
                {error}
              </p>
            )}
            {loading && rfqs.length === 0 ? (
              <p className="rule-t py-16 font-mono text-[13px] text-dim">Loading requests…</p>
            ) : rfqs.length === 0 ? (
              <div className="rule-t py-16 text-[13px] leading-relaxed text-muted">
                No open requests right now.
                <br />
                <span className="text-dim">
                  When someone requests a combo quote on{" "}
                  <Link href="/combos" className="text-accent hover:brightness-110">
                    /combos
                  </Link>
                  , it lands here for you to price.
                </span>
              </div>
            ) : (
              <div className="rule-t">
                <AnimatePresence initial={false}>
                  {rfqs.map((r) => (
                    <motion.div
                      key={r.id}
                      layout
                      initial={{ opacity: 0, height: 0 }}
                      animate={{ opacity: 1, height: "auto" }}
                      exit={{ opacity: 0, height: 0, transition: { duration: 0.15, ease } }}
                      transition={{ duration: 0.22, ease }}
                      className="overflow-hidden"
                    >
                      <RFQCard rfq={r} balanceMicro={balanceMicro} onQuoted={load} />
                    </motion.div>
                  ))}
                </AnimatePresence>
              </div>
            )}
          </>
        )}
      </main>
    </div>
  );
}

type QuoteState = "idle" | "signing" | "done";

function RFQCard({
  rfq,
  balanceMicro,
  onQuoted,
}: {
  rfq: OpenRFQ;
  balanceMicro: number;
  onQuoted: () => void;
}) {
  const wallet = usePitchWallet();
  const [payout, setPayout] = useState("");
  const [state, setState] = useState<QuoteState>("idle");
  const [err, setErr] = useState<string | null>(null);

  const stakeUsd = rfq.stake / 1_000_000;
  const payoutUsd = Math.floor(Number(payout) || 0);
  const valid = payoutUsd > stakeUsd;
  const multiple = payout && stakeUsd > 0 ? (Number(payout) / stakeUsd).toFixed(2) : null;
  const collateralMicro = Math.max(0, payoutUsd * 1_000_000 - rfq.stake);
  // On accept the exchange debits your collateral (payout − stake); a quote you
  // can't cover would fail the taker's accept, so block it here too.
  const overBalance = valid && collateralMicro > balanceMicro;

  async function submit() {
    setErr(null);
    if (!wallet.address) {
      wallet.connect();
      return;
    }
    if (!valid) {
      setErr(`Payout must exceed the ${usd(rfq.stake)} stake.`);
      return;
    }
    if (overBalance) {
      setErr(`Needs ${usd(collateralMicro)} collateral — your vault has ${usd(balanceMicro)}.`);
      return;
    }
    setState("signing");
    try {
      const payoutMicro = payoutUsd * 1_000_000;
      const expiry = BigInt(Math.floor(Date.now() / 1000) + 120); // 2-minute quote
      const salt = randomSalt();
      const msg = borshComboQuote({
        maker: bs58.decode(wallet.address),
        legs: rfq.legs.map((l) => ({ marketId: fromHex(l.market_id), outcome: l.outcome })),
        stake: BigInt(rfq.stake),
        payout: BigInt(payoutMicro),
        expiry,
        salt,
      });
      const sig = await wallet.signMessage(msg);
      await api.submitQuote(rfq.id, {
        maker: wallet.address,
        legs: rfq.legs.map((l) => ({ market_id: l.market_id, outcome: l.outcome })),
        stake: rfq.stake,
        payout: payoutMicro,
        expiry: Number(expiry),
        salt: Number(salt),
        sig: toHex(sig),
      });
      setState("done");
      // Let the taker's side pick it up; the RFQ flips to 'quoted' and this card
      // animates out on the next poll.
      setTimeout(onQuoted, 900);
    } catch (e) {
      setState("idle");
      setErr((e as Error).message);
    }
  }

  return (
    <div className="rule-b py-5">
      {/* header: what's being quoted */}
      <div className="mb-3.5 flex items-baseline justify-between gap-4">
        <div className="min-w-0">
          <span className="text-[13.5px] font-medium text-ink">
            {rfq.legs.length}-leg combo
          </span>
          <span className="ml-2 font-mono text-[11px] text-dim">
            opened {ago(rfq.created_at)} · {rfq.taker.slice(0, 4)}…{rfq.taker.slice(-4)}
          </span>
        </div>
        <span className="shrink-0 font-mono text-[12px] text-muted tnum">
          stake {usd(rfq.stake)}
        </span>
      </div>

      {/* the legs — all must hit for the taker to win */}
      <ul className="mb-5 space-y-1.5">
        {rfq.legs.map((l, i) => (
          <li key={i} className="flex items-baseline gap-2.5 text-[13px]">
            <span
              className={`w-7 shrink-0 font-mono text-[11px] ${l.outcome === 1 ? "text-accent" : "text-down"}`}
            >
              {l.outcome === 1 ? "YES" : "NO"}
            </span>
            <span className="min-w-0 truncate text-ink">{l.title}</span>
          </li>
        ))}
      </ul>

      {state === "done" ? (
        <p className="flex items-center gap-2 font-mono text-[12.5px] text-accent">
          <Check size={14} /> Quote sent — {usd(payoutUsd * 1_000_000)} · the taker can accept it now.
        </p>
      ) : (
        <div>
          <div className="mb-2 flex items-center justify-between gap-3">
            <span className="eyebrow">Payout if all legs hit</span>
            <div className="flex gap-1.5">
              {[2, 3, 5, 10].map((n) => (
                <button
                  key={n}
                  onClick={() => setPayout(String(Math.round(stakeUsd * n)))}
                  className="rounded-[2px] border border-line2 px-2 py-0.5 font-mono text-[10.5px] text-dim transition-colors hover:border-accent hover:text-accent"
                >
                  {n}×
                </button>
              ))}
            </div>
          </div>

          <div className="flex items-baseline border-b border-line2 pb-1.5 transition-colors focus-within:border-accent">
            <span className="mr-1 font-mono text-[18px] font-light text-dim">$</span>
            <input
              inputMode="numeric"
              value={payout}
              onChange={(e) => {
                setErr(null);
                setPayout(e.target.value.replace(/[^0-9]/g, ""));
              }}
              placeholder={String(Math.round(stakeUsd * 3))}
              className="w-full bg-transparent font-mono text-[24px] font-light text-ink outline-none tnum"
            />
            {multiple && (
              <span
                className={`ml-2 shrink-0 font-mono text-[13px] tnum ${valid ? "text-accent" : "text-dim"}`}
              >
                {multiple}×
              </span>
            )}
          </div>

          <p className="mt-2.5 font-mono text-[11px] leading-relaxed">
            {!valid ? (
              <span className="text-dim">
                a payout above {usd(rfq.stake)} — the taker wins it only if every leg hits
              </span>
            ) : overBalance ? (
              <span className="text-down">
                needs {usd(collateralMicro)} collateral · your vault has only {usd(balanceMicro)}
              </span>
            ) : (
              <span className="text-dim">
                you post <span className="text-muted">{usd(collateralMicro)}</span> collateral ·
                win the <span className="text-muted">{usd(rfq.stake)}</span> stake if any leg misses
              </span>
            )}
          </p>

          <button
            onClick={submit}
            disabled={!valid || overBalance || state === "signing"}
            className={`mt-4 w-full ${primaryBtn}`}
          >
            <AnimatePresence mode="popLayout" initial={false}>
              <motion.span
                key={state}
                initial={{ opacity: 0, y: 5, filter: "blur(2px)" }}
                animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
                exit={{ opacity: 0, y: -5, filter: "blur(2px)", transition: { duration: 0.1 } }}
                transition={{ duration: 0.13, ease }}
                className="flex items-center justify-center gap-2"
              >
                {state === "signing" && <Loader2 size={14} className="animate-spin" />}
                {state === "signing" ? "Signing…" : "Sign & Quote"}
              </motion.span>
            </AnimatePresence>
          </button>
        </div>
      )}

      {err && (
        <p role="alert" className="mt-2.5 font-mono text-[12px] text-down">
          {err}
        </p>
      )}
    </div>
  );
}
