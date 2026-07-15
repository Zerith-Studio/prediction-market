"use client";

import { AnimatePresence, motion } from "framer-motion";

export function PitchTicker({
  lines,
  index,
}: {
  lines: string[];
  index: number;
}) {
  if (!lines.length) return null;
  return (
    <div className="flex items-center gap-3.5 overflow-hidden rounded-xl border border-line bg-panel2/70 px-4 py-2.5">
      <span className="shrink-0 font-mono text-[10px] font-bold tracking-[0.14em] text-accent">
        ◆ PITCH AI
      </span>
      <div className="relative h-[18px] flex-1">
        {/* initial={false} => first line paints instantly (never gated on animation);
            subsequent line changes crossfade. */}
        <AnimatePresence mode="wait" initial={false}>
          <motion.p
            key={index}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.32, ease: [0.22, 1, 0.36, 1] }}
            className="absolute inset-0 truncate text-[13px] italic text-ink"
          >
            {lines[index]}
          </motion.p>
        </AnimatePresence>
      </div>
    </div>
  );
}
