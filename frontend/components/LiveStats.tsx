"use client";

import type { Match } from "@/lib/types";

/**
 * Live match stats from the TxLINE scores feed — possession, corners, cards.
 * Every value shown is real feed data; a stat that isn't present is not
 * rendered. The scoreline itself lives in MatchHero above this.
 */
export function LiveStats({ match }: { match: Match }) {
  const ls = match.live_state;
  const home = ls.stats?.home;
  const away = ls.stats?.away;
  const poss = ls.possession;
  const live = match.status === "live" || match.status === "ht";

  const rows: { label: string; home?: number; away?: number }[] = [
    { label: "Corners", home: home?.corners, away: away?.corners },
    { label: "Yellow Cards", home: home?.yellow, away: away?.yellow },
    { label: "Red Cards", home: home?.red, away: away?.red },
  ];
  if (home?.shots != null || away?.shots != null) {
    rows.push({ label: "Shots", home: home?.shots, away: away?.shots });
  }
  const hasRows = rows.some((r) => r.home != null || r.away != null);

  if (!poss && !hasRows) {
    return (
      <p className="py-8 text-center font-mono text-[12px] text-dim">
        {live
          ? "Awaiting live match stats from TxLINE…"
          : "Live match stats appear once the match kicks off."}
      </p>
    );
  }

  return (
    <div className="mx-auto max-w-[620px]">
      {poss && (
        <Possession home={poss.home} away={poss.away} homeTeam={match.home} awayTeam={match.away} />
      )}
      {hasRows && (
        <div className={poss ? "mt-8" : ""}>
          {rows
            .filter((r) => r.home != null || r.away != null)
            .map((r) => (
              <StatRow key={r.label} label={r.label} home={r.home} away={r.away} />
            ))}
        </div>
      )}
    </div>
  );
}

function Possession({
  home,
  away,
  homeTeam,
  awayTeam,
}: {
  home: number;
  away: number;
  homeTeam: string;
  awayTeam: string;
}) {
  const total = home + away || 1;
  const homePct = Math.round((home / total) * 100);
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between font-mono text-[13px] tnum">
        <span className="text-ink">{home}%</span>
        <span className="eyebrow">Possession</span>
        <span className="text-ink">{away}%</span>
      </div>
      <div className="flex h-1.5 w-full overflow-hidden rounded-sm">
        <div className="bg-accent" style={{ width: `${homePct}%` }} aria-hidden />
        <div className="flex-1 bg-line2" aria-hidden />
      </div>
      <div className="mt-1.5 flex justify-between font-mono text-[10.5px] text-dim">
        <span className="truncate">{homeTeam}</span>
        <span className="truncate">{awayTeam}</span>
      </div>
    </div>
  );
}

function StatRow({ label, home, away }: { label: string; home?: number; away?: number }) {
  return (
    <div className="grid grid-cols-[1fr_auto_1fr] items-center rule-b py-3">
      <span className="font-mono text-[15px] tnum text-ink">{home ?? "–"}</span>
      <span className="eyebrow px-4 text-center">{label}</span>
      <span className="text-right font-mono text-[15px] tnum text-ink">{away ?? "–"}</span>
    </div>
  );
}
