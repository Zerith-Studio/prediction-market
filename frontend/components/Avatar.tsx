"use client";

import dynamic from "next/dynamic";

// @outpacelabs/avatars renders to <canvas>, so load it client-only (no SSR) to
// avoid touching canvas on the server. A hairline placeholder holds the space
// until it hydrates.
const GradientAvatar = dynamic(
  () => import("@outpacelabs/avatars").then((m) => ({ default: m.GradientAvatar })),
  { ssr: false, loading: () => null }
);

/** Deterministic gradient avatar for a wallet/seed. seed → identical avatar. */
export function Avatar({ seed, size = 28 }: { seed?: string | null; size?: number }) {
  const box = { width: size, height: size };
  if (!seed) {
    return (
      <span
        className="inline-block shrink-0 rounded-full border border-line2 bg-line"
        style={box}
        aria-hidden
      />
    );
  }
  return (
    <span
      className="inline-flex shrink-0 overflow-hidden rounded-full border border-line2"
      style={box}
      aria-hidden
    >
      <GradientAvatar seed={seed} size={size} />
    </span>
  );
}
