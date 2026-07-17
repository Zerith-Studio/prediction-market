"use client";

import type { Match } from "@/lib/types";
import { TeamFlag } from "@/components/TeamFlag";

function periodLabel(p?: string): string | null {
  return p === "1H" ? "1ST HALF" : p === "HT" ? "HALF TIME" : p === "2H" ? "2ND HALF" : p === "FT" ? "FULL TIME" : null;
}

export function MatchHero({ match }: { match: Match }) {
  const live = match.status === "live" || match.status === "ht";
  const { minute, period, home_score = 0, away_score = 0 } = match.live_state;

  // Build "LIVE · <period> · <minute>'" from only the parts we actually have,
  // so a live match with no clock/period reads "LIVE" — never "LIVE · LIVE ·
  // undefined'".
  const liveLabel = [
    "LIVE",
    periodLabel(period),
    typeof minute === "number" ? `${minute}'` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <section className="py-7 sm:py-9">
      <div className="mb-5 flex items-center justify-between gap-3">
        <div className="flex shrink-0 items-center gap-2 whitespace-nowrap font-mono text-[11px] tracking-[0.14em]">
          {live && (
            <span className="h-[7px] w-[7px] rounded-full bg-down animate-live-pulse-down" aria-hidden />
          )}
          <span className={live ? "text-down" : "text-dim"}>
            {live ? liveLabel : "SCHEDULED"}
          </span>
        </div>
        <div className="min-w-0 truncate eyebrow">
          {live || match.status === "ft"
            ? "TxLINE live data · settled on Solana"
            : `Kickoff ${new Date(match.kickoff_at).toLocaleString(undefined, {
                weekday: "short",
                hour: "2-digit",
                minute: "2-digit",
              })}`}
        </div>
      </div>

      <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-4 sm:gap-8">
        <div className="flex min-w-0 items-center justify-end gap-2.5 text-right">
          <span className="truncate text-[22px] font-bold tracking-tight sm:text-[32px]">
            {match.home}
          </span>
          <TeamFlag team={match.home} size={26} />
        </div>
        <div className="shrink-0 font-mono text-[24px] font-light tracking-[0.12em] text-ink tnum sm:text-[34px]">
          {home_score}
          <span className="mx-1.5 text-dim sm:mx-2.5">–</span>
          {away_score}
        </div>
        <div className="flex min-w-0 items-center gap-2.5">
          <TeamFlag team={match.away} size={26} />
          <span className="truncate text-[22px] font-bold tracking-tight sm:text-[32px]">
            {match.away}
          </span>
        </div>
      </div>
    </section>
  );
}
