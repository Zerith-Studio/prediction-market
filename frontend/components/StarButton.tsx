"use client";

import { usePitchWallet } from "@/lib/wallet";
import { useWatchlist } from "@/lib/watchlist";

// Star toggle for a market's watchlist membership. Hidden when no wallet is
// connected (there is nowhere to persist the preference). Stops click
// propagation so it can sit inside link/card surfaces.
export function StarButton({
  marketId,
  className = "",
}: {
  marketId: string;
  className?: string;
}) {
  const wallet = usePitchWallet();
  const { isWatched, toggle } = useWatchlist();
  if (!wallet.address) return null;

  const watched = isWatched(marketId);
  return (
    <button
      type="button"
      aria-pressed={watched}
      aria-label={watched ? "Remove from watchlist" : "Add to watchlist"}
      title={watched ? "In watchlist" : "Add to watchlist"}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        toggle(marketId);
      }}
      className={`shrink-0 transition-colors duration-150 ${
        watched ? "text-accent" : "text-dim hover:text-muted"
      } ${className}`}
    >
      <svg
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill={watched ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M12 17.3 6.16 20.5l1.12-6.53L2.5 9.36l6.56-.95L12 2.5l2.94 5.91 6.56.95-4.78 4.61 1.12 6.53z" />
      </svg>
    </button>
  );
}
