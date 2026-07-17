"use client";

import { useState } from "react";
import type { ReactNode } from "react";
import { motion } from "framer-motion";
import type { Match } from "@/lib/types";
import { LiveStats } from "./LiveStats";
import { Lineups } from "./Lineups";

/**
 * The match centre below the price header. Only tabs backed by real TxLINE data
 * render — "Match" (live score/possession/cards/corners) always, "Lineups" only
 * once the feed delivers team sheets. Panels the feed can't source (venue,
 * standings, head-to-head history, managers, referee) are intentionally absent.
 */
export function MatchCentre({ match }: { match: Match }) {
  const hasLineups = !!(
    match.lineups?.home?.starters?.length || match.lineups?.away?.starters?.length
  );

  const tabs: { key: "match" | "lineups"; label: string }[] = [{ key: "match", label: "Match" }];
  if (hasLineups) tabs.push({ key: "lineups", label: "Lineups" });

  const [tab, setTab] = useState<"match" | "lineups">("match");
  const active = tabs.some((t) => t.key === tab) ? tab : "match";

  return (
    <section className="rule-t">
      <div className="flex items-center gap-7 pt-5">
        {tabs.map((t) => (
          <Tab key={t.key} active={active === t.key} onClick={() => setTab(t.key)}>
            {t.label}
          </Tab>
        ))}
        <span className="ml-auto eyebrow">TxLINE live data</span>
      </div>

      <div className="py-7">
        {active === "match" && <LiveStats match={match} />}
        {active === "lineups" && match.lineups && (
          <Lineups lineups={match.lineups} home={match.home} away={match.away} />
        )}
      </div>
    </section>
  );
}

function Tab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={`relative pb-2.5 text-[13px] font-semibold transition-colors duration-150 ${
        active ? "text-ink" : "text-dim hover:text-muted"
      }`}
    >
      {children}
      {active && (
        <motion.span
          layoutId="matchcentre-underline"
          transition={{ duration: 0.2, ease: [0.23, 1, 0.32, 1] }}
          className="absolute inset-x-0 -bottom-0.5 h-0.5 bg-accent"
          aria-hidden
        />
      )}
    </button>
  );
}
