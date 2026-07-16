"use client";

// Market-maker view: answer open combo RFQs by hand (a human MM, not just the
// bot). Lists open RFQs; for each you set a payout, sign a ComboQuote with your
// wallet (ed25519 over borshComboQuote — byte-identical to the backend), and
// submit it. The taker sees it counting down on /combos and can accept. Reach
// this by URL — it's a market-maker tool, not part of the taker nav.

import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import Link from "next/link";
import bs58 from "bs58";
import { usePitchWallet } from "@/lib/wallet";
import { borshComboQuote, fromHex, randomSalt, toHex } from "@/lib/borsh";
import { api, configured, type OpenRFQ } from "@/lib/api";
import { usd } from "@/lib/format";
import { TopBar } from "@/components/TopBar";

const primaryBtn =
  "bg-accent px-5 py-3 text-[13px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 enabled:active:scale-[0.98] disabled:bg-line2 disabled:text-dim";

export default function MarketMakerPage() {
  const wallet = usePitchWallet();
  const [rfqs, setRfqs] = useState<OpenRFQ[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      setRfqs(await api.listOpenRFQs());
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

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
      <TopBar balanceMicro={0} />
      <main className="mx-auto max-w-[900px] px-5 sm:px-8">
        <div className="flex items-baseline justify-between py-8">
          <h1 className="text-[15px] font-semibold">Market maker</h1>
          <span className="eyebrow">answer open combo RFQs · signed quotes</span>
        </div>

        {!wallet.address ? (
          <div className="rule-t py-10">
            <p className="mb-5 max-w-[540px] text-[13px] leading-relaxed text-muted">
              Connect a wallet to quote open RFQs. Your quote is ed25519-signed;
              nothing is escrowed until a taker accepts it.
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
              <p className="py-16 font-mono text-[13px] text-dim">Loading…</p>
            ) : rfqs.length === 0 ? (
              <div className="rule-t py-16 text-[13px] leading-relaxed text-muted">
                No open RFQs right now.
                <br />
                <span className="text-dim">
                  When someone requests a combo quote on{" "}
                  <Link href="/combos" className="text-accent hover:brightness-110">
                    /combos
                  </Link>
                  , it appears here for you to quote.
                </span>
              </div>
            ) : (
              <div className="rule-t">
                {rfqs.map((r) => (
                  <RFQCard key={r.id} rfq={r} onQuoted={load} />
                ))}
              </div>
            )}
          </>
        )}
      </main>
    </div>
  );
}

function RFQCard({ rfq, onQuoted }: { rfq: OpenRFQ; onQuoted: () => void }) {
  const wallet = usePitchWallet();
  const [payout, setPayout] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const stakeUsd = rfq.stake / 1_000_000;
  const multiple = payout && stakeUsd > 0 ? (Number(payout) / stakeUsd).toFixed(2) : null;

  async function submit() {
    setErr(null);
    if (!wallet.address) {
      wallet.connect();
      return;
    }
    const payoutUsd = Math.floor(Number(payout) || 0);
    if (payoutUsd <= 0) {
      setErr("Enter a payout.");
      return;
    }
    setBusy(true);
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
      setDone(true);
      onQuoted();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rule-b py-5">
      <div className="mb-3 flex items-baseline justify-between gap-4">
        <span className="truncate font-mono text-[11px] text-dim">
          RFQ {rfq.id.slice(0, 8)} · taker {rfq.taker.slice(0, 4)}…{rfq.taker.slice(-4)}
        </span>
        <span className="shrink-0 font-mono text-[12px] text-muted tnum">
          stake {usd(rfq.stake)}
        </span>
      </div>
      <ul className="mb-4 space-y-1.5">
        {rfq.legs.map((l, i) => (
          <li key={i} className="flex items-baseline justify-between gap-3 text-[13px]">
            <span className="min-w-0 truncate text-ink">{l.title}</span>
            <span
              className={`shrink-0 font-mono text-[11px] ${l.outcome === 1 ? "text-accent" : "text-down"}`}
            >
              {l.outcome === 1 ? "YES" : "NO"}
            </span>
          </li>
        ))}
      </ul>

      {done ? (
        <p className="font-mono text-[12px] text-accent">
          ✓ Quote submitted — the taker sees it counting down on /combos.
        </p>
      ) : (
        <div className="flex items-end gap-4">
          <label className="flex-1">
            <span className="mb-1 block eyebrow">Payout (USDC) if all legs hit</span>
            <div className="flex items-baseline border-b border-line2 pb-1.5 focus-within:border-accent">
              <input
                inputMode="numeric"
                value={payout}
                onChange={(e) => setPayout(e.target.value.replace(/[^0-9]/g, ""))}
                placeholder={`> ${stakeUsd}`}
                className="w-full bg-transparent font-mono text-[20px] font-light text-ink outline-none tnum"
              />
              {multiple && <span className="ml-2 font-mono text-[12px] text-dim">{multiple}×</span>}
            </div>
          </label>
          <button onClick={submit} disabled={busy || !payout} className={primaryBtn}>
            {busy ? (
              <span className="inline-flex items-center gap-2">
                <Loader2 size={14} className="animate-spin" />
                Signing…
              </span>
            ) : (
              "Sign & Quote"
            )}
          </button>
        </div>
      )}
      {err && (
        <p role="alert" className="mt-2 font-mono text-[12px] text-down">
          {err}
        </p>
      )}
    </div>
  );
}
