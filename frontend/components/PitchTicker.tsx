"use client";

import { AnimatePresence, motion } from "framer-motion";

export function PitchTicker({ lines, index }: { lines: string[]; index: number }) {
  if (!lines.length) return null;
  return (
    <div className="flex items-center gap-4 py-3">
      <span className="shrink-0 eyebrow">Pitch AI</span>
      <div className="relative h-[19px] flex-1">
        <AnimatePresence mode="wait" initial={false}>
          <motion.p
            key={index}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
            className="absolute inset-0 truncate text-[13px] italic text-muted"
          >
            {lines[index]}
          </motion.p>
        </AnimatePresence>
      </div>
    </div>
  );
}
