"use client";

import { motion } from "framer-motion";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";

export function OddsMeter({
  yesPrice,
  priceDelta,
  volumeMicro,
}: {
  yesPrice: number;
  priceDelta: number;
  volumeMicro: number;
}) {
  const noPrice = 100 - yesPrice;
  const up = priceDelta >= 0;
  const vol = `$${(volumeMicro / 1_000_000 / 1000).toFixed(1)}k`;

  return (
    <div>
      <div className="flex h-11 overflow-hidden rounded-xl font-mono text-[15px] font-extrabold">
        <motion.div
          className="flex items-center bg-gradient-to-b from-yes to-[#12a866] px-4 text-yes-ink"
          animate={{ width: `${yesPrice}%` }}
          transition={{ type: "spring", stiffness: 120, damping: 20 }}
        >
          YES {yesPrice}¢
        </motion.div>
        <div className="flex flex-1 items-center justify-end bg-gradient-to-b from-no to-[#c9243f] px-4 text-no-ink">
          {noPrice}¢ NO
        </div>
      </div>
      <div className="mt-2 flex items-center justify-between font-mono text-[11px] text-muted tnum">
        <span className="flex items-center gap-1.5">
          Implied {yesPrice}%
          {priceDelta !== 0 && (
            <span className={`flex items-center gap-0.5 ${up ? "text-yes" : "text-no"}`}>
              {up ? <ArrowUpRight size={12} /> : <ArrowDownRight size={12} />}
              {Math.abs(priceDelta)}¢
            </span>
          )}
        </span>
        <span>24h vol {vol}</span>
      </div>
    </div>
  );
}
