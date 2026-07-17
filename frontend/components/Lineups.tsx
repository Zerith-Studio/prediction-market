"use client";

import type { LineupPlayer, Lineups as LineupsData, TeamLineup } from "@/lib/types";

/**
 * Team sheets from the TxLINE scores feed. number + name are always real; the
 * position sub-label and the derived formation are shown only when present
 * (the feed's position codes aren't fully mapped yet — we never invent a label).
 * Starters are grouped into lines by the feed's unit id (own goal → attack).
 */
export function Lineups({
  lineups,
  home,
  away,
}: {
  lineups: LineupsData;
  home: string;
  away: string;
}) {
  return (
    <div className="grid gap-10 sm:grid-cols-2 sm:gap-8">
      <TeamColumn team={lineups.home} fallbackName={home} align="left" />
      <TeamColumn team={lineups.away} fallbackName={away} align="right" />
    </div>
  );
}

function TeamColumn({
  team,
  fallbackName,
  align,
}: {
  team?: TeamLineup;
  fallbackName: string;
  align: "left" | "right";
}) {
  if (!team || team.starters.length === 0) {
    return (
      <div className={align === "right" ? "text-right" : ""}>
        <TeamHeader name={fallbackName} align={align} />
        <p className="mt-4 font-mono text-[12px] text-dim">No lineup published.</p>
      </div>
    );
  }

  const lines = groupByUnit(team.starters);

  return (
    <div>
      <TeamHeader name={team.team || fallbackName} formation={team.formation} align={align} />

      <div className="mt-6 space-y-6">
        {lines.map((line, i) => (
          <div key={i} className="flex flex-wrap justify-center gap-x-5 gap-y-4">
            {line.map((p) => (
              <PlayerChip key={p.number + p.name} p={p} />
            ))}
          </div>
        ))}
      </div>

      {team.subs.length > 0 && (
        <div className="mt-8 rule-t pt-4">
          <div className="mb-2 eyebrow">Substitutes</div>
          <ul className="space-y-1.5">
            {team.subs.map((p) => (
              <li key={p.number + p.name} className="flex items-baseline gap-2.5 text-[12.5px]">
                <span className="w-6 shrink-0 font-mono tnum text-dim">{p.number}</span>
                <span className="text-muted">{p.name}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function TeamHeader({
  name,
  formation,
  align,
}: {
  name: string;
  formation?: string;
  align: "left" | "right";
}) {
  return (
    <div className={`flex items-baseline gap-3 ${align === "right" ? "justify-end" : ""}`}>
      <span className="text-[15px] font-semibold text-ink">{name}</span>
      {formation && <span className="font-mono text-[12px] tnum text-accent">{formation}</span>}
    </div>
  );
}

function PlayerChip({ p }: { p: LineupPlayer }) {
  return (
    <div className="flex w-[76px] flex-col items-center gap-1.5">
      <div className="flex h-9 w-9 items-center justify-center rounded-full border border-line2 font-mono text-[13px] tnum text-ink">
        {p.number}
      </div>
      <span className="text-center text-[10.5px] leading-tight text-muted">
        {p.name}
        {p.captain && <span className="text-dim"> (C)</span>}
      </span>
      {p.position && (
        <span className="text-center text-[8.5px] uppercase tracking-[0.08em] text-dim">
          {p.position}
        </span>
      )}
    </div>
  );
}

// groupByUnit splits starters into lines ordered by the feed's unit id (lower =
// own goal, higher = attack), preserving each line's within-order.
function groupByUnit(starters: LineupPlayer[]): LineupPlayer[][] {
  const byUnit = new Map<number, LineupPlayer[]>();
  for (const p of starters) {
    const arr = byUnit.get(p.unit) ?? [];
    arr.push(p);
    byUnit.set(p.unit, arr);
  }
  return [...byUnit.keys()].sort((a, b) => a - b).map((u) => byUnit.get(u)!);
}
