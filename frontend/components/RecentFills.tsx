"use client";

import { AnimatePresence, motion } from "framer-motion";
import type { Fill } from "@/lib/types";
import { shares, shortHash } from "@/lib/format";

function ago(ts: number): string {
  const s = Math.max(0, Math.round((Date.now() - ts) / 1000));
  if (s < 60) return `${s}s`;
  return `${Math.round(s / 60)}m`;
}

export function RecentFills({
  fills,
  yesPrice,
}: {
  fills: Fill[];
  yesPrice: number;
}) {
  return (
    <div className="panel p-[18px] sm:p-5">
      <div className="mb-3.5 flex items-center justify-between">
        <h3 className="text-[13px] font-bold">Recent fills</h3>
        <span className="font-mono text-[11px] text-dim">settled by crank</span>
      </div>
      <div className="font-mono text-[12px]">
        <div className="grid grid-cols-[1fr_1fr_1fr_auto] px-1 pb-2 text-[10.5px] uppercase tracking-[0.08em] text-dim">
          <span>Price</span>
          <span className="text-right">Size</span>
          <span className="text-center">Type</span>
          <span className="text-right">Age</span>
        </div>
        <AnimatePresence initial={false}>
          {fills.map((f) => {
            const up = f.price >= yesPrice;
            return (
              <motion.div
                key={`${f.taker_hash}-${f.ts}`}
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: "auto" }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
                className="grid grid-cols-[1fr_1fr_1fr_auto] items-center px-1 py-[5px]"
              >
                <span className={`font-bold ${up ? "text-yes" : "text-no"} tnum`}>
                  {f.price}¢
                </span>
                <span className="text-right text-muted tnum">{shares(f.size)}</span>
                <span className="text-center">
                  <span
                    className={`rounded px-1.5 py-0.5 text-[10px] ${
                      f.match_type === "MINT"
                        ? "bg-verify/[0.15] text-verify"
                        : f.match_type === "MERGE"
                          ? "bg-accent/[0.15] text-accent"
                          : "bg-line2/60 text-muted"
                    }`}
                    title={shortHash(f.taker_hash)}
                  >
                    {f.match_type}
                  </span>
                </span>
                <span className="text-right text-dim tnum">{ago(f.ts)}</span>
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>
    </div>
  );
}
