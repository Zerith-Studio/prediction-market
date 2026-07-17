"use client";

import { dotGradient, flagSrc } from "@/lib/teams";

// Circular team flag (local SVG asset) with the old gradient-dot fallback for
// teams we don't have a flag for. Decorative — the team name is always rendered
// as text alongside, so the image stays aria-hidden.
export function TeamFlag({ team, size = 18 }: { team: string; size?: number }) {
  const src = flagSrc(team);
  const px = { width: size, height: size };
  if (!src) {
    return (
      <span
        className="inline-block shrink-0 rounded-full"
        style={{ ...px, background: dotGradient(team) }}
        aria-hidden
      />
    );
  }
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={src}
      alt=""
      aria-hidden
      className="inline-block shrink-0 rounded-full border border-line2 object-cover"
      style={px}
    />
  );
}

// Polymarket-style market image: the two team flags slightly overlapped.
export function FlagPair({
  home,
  away,
  size = 20,
}: {
  home: string;
  away: string;
  size?: number;
}) {
  return (
    <span className="inline-flex shrink-0 items-center" aria-hidden>
      <span className="relative z-[1] inline-flex rounded-full ring-2 ring-bg">
        <TeamFlag team={home} size={size} />
      </span>
      <span className="inline-flex rounded-full" style={{ marginLeft: -size * 0.3 }}>
        <TeamFlag team={away} size={size} />
      </span>
    </span>
  );
}
