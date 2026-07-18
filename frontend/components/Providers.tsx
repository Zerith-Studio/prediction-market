"use client";

import { MotionConfig } from "framer-motion";
import { Toaster } from "sonner";
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
      {/* Global action toasts (order / fill / self-trade / cancel / deposit).
          Text-only — no icons/emoji; meaning comes from the semantic color
          (green fill, red error, amber self-trade prevention). */}
      <Toaster
        position="bottom-right"
        theme="dark"
        richColors
        closeButton
        icons={{ success: null, error: null, warning: null, info: null, loading: null }}
        toastOptions={{ classNames: { title: "font-semibold", icon: "hidden" } }}
      />
    </MotionConfig>
  );
}
