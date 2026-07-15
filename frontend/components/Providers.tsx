"use client";

import { MotionConfig } from "framer-motion";
import { PitchWalletProvider } from "@/lib/wallet";

/**
 * MotionConfig reducedMotion="user": the globals.css kill-switch only zeroes
 * CSS transitions/animations — Framer Motion drives values from JS and ignores
 * it. This makes every motion.* component honor prefers-reduced-motion
 * (transforms disabled, opacity crossfades kept — which is the right fallback:
 * fewer and gentler, not zero).
 */
export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <MotionConfig reducedMotion="user">
      <PitchWalletProvider>{children}</PitchWalletProvider>
    </MotionConfig>
  );
}
