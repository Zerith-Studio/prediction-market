"use client";

// Market-level portfolio: the connected wallet's position and open orders for
// THIS market, with inline exit (sell at best bid) and cancel — so you never
// have to leave the market page to manage a trade. Renders nothing when the
// wallet is disconnected or has nothing here.

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { AnimatePresence, motion } from "framer-motion";
import { api } from "@/lib/api";
import type { Portfolio } from "@/lib/types";
import { usd, shares as fmtShares, shortHash } from "@/lib/format";
import { usePitchWallet } from "@/lib/wallet";
import { calcPosition, usePositionActions } from "@/lib/usePositionActions";

export function MarketPositions({
  marketId,
  refreshKey = 0,
}: {
  marketId: string;
  refreshKey?: number;
}) {
  const wallet = usePitchWallet();
  const [pf, setPf] = useState<Portfolio | null>(null);

  const load = useCallback(() => {
    if (!wallet.address) {
      setPf(null);
      return;
    }
    api.getPortfolio(wallet.address).then(setPf).catch(() => {});
  }, [wallet.address]);

  useEffect(() => {
    load();
    const t = setInterval(load, 6000);
    return () => clearInterval(t);
  }, [load, refreshKey]);

  const { exit, cancel, busy, error } = usePositionActions(load);

  if (!wallet.address) return null;

  const positions = (pf?.positions ?? [])
    .filter((p) => p.market_id === marketId && (p.yes > 0 || p.no > 0))
    .map(calcPosition);
  const orders = (pf?.orders ?? []).filter(
    (o) => o.market_id === marketId && o.status === "live"
  );

  if (positions.length === 0 && orders.length === 0) return null;

  return (
    <div className="border border-line p-5 sm:p-6">
      <div className="mb-4 flex items-baseline justify-between">
        <div className="eyebrow">Your position</div>
        <Link
          href="/portfolio"
          className="font-mono text-[11px] text-dim transition-colors hover:text-accent"
        >
          full portfolio →
        </Link>
      </div>

      {positions.length > 0 && (
        <div className="mb-1">
          {positions.map((x) => {
            const up = x.unrealizedMicro >= 0;
            return (
              <div
                key={`${x.p.market_id}-${x.side}`}
                className="flex items-center justify-between gap-3 border-b border-line py-3 last:border-b-0"
              >
                <div className="min-w-0">
                  <div className="font-mono text-[13px] text-ink tnum">
                    {fmtShares(x.qty)} {x.side}
                  </div>
                  <div className="mt-0.5 font-mono text-[11px] text-dim tnum">
                    avg {x.entry}¢ · now {x.cur}¢ ·{" "}
                    <span className={up ? "text-accent" : "text-down"}>
                      {up ? "+" : ""}
                      {usd(x.unrealizedMicro)}
                    </span>
                  </div>
                </div>
                <button
                  onClick={() => exit(x)}
                  disabled={busy === x.p.market_id || x.cur <= 0}
                  title={x.cur <= 0 ? "No bid to exit into" : `Sell ${fmtShares(x.qty)} @ ${x.cur}¢`}
                  className="shrink-0 border border-line2 px-3 py-1.5 font-mono text-[12px] text-muted transition-colors hover:border-dim hover:text-ink disabled:cursor-not-allowed disabled:opacity-40 enabled:active:scale-[0.97]"
                >
                  {busy === x.p.market_id ? "exiting…" : "exit"}
                </button>
              </div>
            );
          })}
        </div>
      )}

      {orders.length > 0 && (
        <div className={positions.length > 0 ? "mt-4" : ""}>
          {positions.length > 0 && <div className="mb-2 eyebrow">Open orders</div>}
          <AnimatePresence initial={false}>
            {orders.map((o) => (
              <motion.div
                key={o.order_hash}
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: "auto" }}
                exit={{ opacity: 0, height: 0, transition: { duration: 0.12 } }}
                transition={{ duration: 0.18, ease: [0.23, 1, 0.32, 1] }}
                className="overflow-hidden"
              >
                <div className="flex items-center justify-between gap-3 border-b border-line py-3 last:border-b-0">
                  <div className="min-w-0">
                    <div className="font-mono text-[13px] text-ink tnum">
                      {o.side === "buy" ? "Buy" : "Sell"} {o.outcome} · {fmtShares(o.remaining)} @ {o.price}¢
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-dim">
                      resting · {shortHash(o.order_hash, 6, 4)}
                    </div>
                  </div>
                  <button
                    onClick={() => cancel(o.order_hash)}
                    disabled={busy === o.order_hash}
                    className="shrink-0 border border-line2 px-3 py-1.5 font-mono text-[12px] text-muted transition-colors hover:border-dim hover:text-down disabled:cursor-not-allowed disabled:opacity-40 enabled:active:scale-[0.97]"
                  >
                    {busy === o.order_hash ? "cancelling…" : "cancel"}
                  </button>
                </div>
              </motion.div>
            ))}
          </AnimatePresence>
        </div>
      )}

      {error && (
        <p className="mt-3 font-mono text-[12px] text-down" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
