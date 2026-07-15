"use client";

import { AnimatePresence, motion } from "framer-motion";
import type { Fill } from "@/lib/types";
import { shares } from "@/lib/format";

function ago(ts: number): string {
  const s = Math.max(0, Math.round((Date.now() - ts) / 1000));
  return s < 60 ? `${s}s` : `${Math.round(s / 60)}m`;
}

export function RecentFills({ fills, yesPrice }: { fills: Fill[]; yesPrice: number }) {
  return (
    <div>
      <div className="mb-4 flex items-baseline justify-between">
        <h2 className="text-[13px] font-semibold text-ink">Trades</h2>
        <span className="font-mono text-[11px] text-dim">settled on-chain</span>
      </div>
      <div className="font-mono text-[12px]">
        <div className="grid grid-cols-[1fr_1fr_auto_auto] gap-3 pb-2.5 eyebrow">
          <span>Price</span>
          <span className="text-right">Size</span>
          <span className="text-right">Type</span>
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
                className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-3 py-[5px]"
              >
                <span className={`${up ? "text-accent" : "text-down"} tnum`}>{f.price}¢</span>
                <span className="text-right text-muted tnum">{shares(f.size)}</span>
                <span className="text-right text-dim">{f.match_type}</span>
                <span className="text-right text-dim tnum">{ago(f.ts)}</span>
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>
    </div>
  );
}
