"use client";

import type { Match } from "@/lib/types";

// National-color crest treatments; falls back to a neutral gradient.
const CRESTS: Record<string, { code: string; bg: string; fg?: string }> = {
  Brazil: { code: "BRA", bg: "linear-gradient(150deg,#ffd21e,#009c3b)" },
  Argentina: { code: "ARG", bg: "linear-gradient(150deg,#75aadb,#ffffff)" },
  France: { code: "FRA", bg: "linear-gradient(150deg,#0055a4,#ef4135)", fg: "#fff" },
  England: { code: "ENG", bg: "linear-gradient(150deg,#f4f4f4,#cf142b)" },
  Spain: { code: "ESP", bg: "linear-gradient(150deg,#c60b1e,#ffc400)" },
  Germany: { code: "GER", bg: "linear-gradient(150deg,#111,#dd0000,#ffce00)", fg: "#fff" },
};

function crest(team: string) {
  return (
    CRESTS[team] ?? {
      code: team.slice(0, 3).toUpperCase(),
      bg: "linear-gradient(150deg,#2a3547,#141b26)",
      fg: "#eef2f8",
    }
  );
}

function periodLabel(p?: string) {
  switch (p) {
    case "1H":
      return "1ST HALF";
    case "HT":
      return "HALF TIME";
    case "2H":
      return "2ND HALF";
    case "FT":
      return "FULL TIME";
    default:
      return "LIVE";
  }
}

function Crest({ team, align }: { team: string; align: "start" | "end" }) {
  const c = crest(team);
  return (
    <div
      className={`flex min-w-0 items-center gap-2.5 sm:gap-3.5 ${
        align === "end" ? "flex-row-reverse text-right" : ""
      }`}
    >
      <span
        className="grid h-11 w-11 shrink-0 place-items-center rounded-xl text-[15px] font-extrabold sm:h-[52px] sm:w-[52px] sm:rounded-[13px] sm:text-[19px]"
        style={{ background: c.bg, color: c.fg ?? "#07090d" }}
        aria-hidden
      >
        {c.code}
      </span>
      <span className="truncate text-[15px] font-extrabold leading-tight tracking-tight sm:text-[20px]">
        {team}
      </span>
    </div>
  );
}

export function MatchHero({ match }: { match: Match }) {
  const live = match.status === "live" || match.status === "ht";
  const { minute, period, home_score = 0, away_score = 0 } = match.live_state;

  return (
    <section className="panel pitch-stripes overflow-hidden">
      <div className="flex items-center justify-between gap-3 border-b border-line px-4 py-3 sm:px-5 sm:py-3.5">
        <div className="flex shrink-0 items-center gap-2 whitespace-nowrap font-mono text-[11px] font-bold tracking-[0.1em]">
          {live && (
            <span className="h-2 w-2 rounded-full bg-no animate-live-pulse" aria-hidden />
          )}
          <span className={live ? "text-no" : "text-dim"}>
            {live ? `LIVE · ${periodLabel(period)}` : "SCHEDULED"}
          </span>
        </div>
        <div className="min-w-0 truncate font-mono text-[11px] tracking-[0.06em] text-dim">
          WORLD CUP 2026 · GROUP C · METLIFE STADIUM
        </div>
      </div>

      <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-3 px-4 py-5 sm:gap-8 sm:px-8 sm:py-6">
        <Crest team={match.home} align="start" />
        <div className="shrink-0 text-center">
          <div className="flex items-center justify-center gap-2.5 font-mono text-[30px] font-extrabold leading-none tracking-[2px] tnum sm:gap-3.5 sm:text-[40px]">
            <span>{home_score}</span>
            <span className="text-dim">:</span>
            <span>{away_score}</span>
          </div>
          {live && (
            <div className="mt-1.5 font-mono text-[11px] tracking-[0.1em] text-accent tnum sm:text-[12px]">
              {minute}&apos; <span className="animate-live-pulse">●</span>
            </div>
          )}
        </div>
        <Crest team={match.away} align="end" />
      </div>
    </section>
  );
}
