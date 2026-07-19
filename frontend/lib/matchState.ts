import type { Lineups, Match, TeamMatchStats } from "./types";

// The score-tick shape carried by a match_state WS event's payload (mirrors the
// backend scorePayload); a "lineup" sub-event instead carries a Lineups object.
export type ScoreTick = {
  minute?: number;
  period?: string;
  home_goals?: number;
  away_goals?: number;
  possession?: { home: number; away: number };
  stats?: { home: TeamMatchStats; away: TeamMatchStats };
};

export interface MatchStateEvent {
  // fixture_id is set for match-scoped events (backend ws.Event.FixtureID).
  fixture_id?: string;
  data?: { event?: string; payload?: ScoreTick | Lineups };
}

/**
 * Applies one match_state WS event to the current match, returning the next
 * match (or the SAME reference when nothing should change, so callers can skip
 * a re-render). This is the single source of truth for how a live tick maps to
 * the match a market page is showing.
 *
 * Two rules that keep the status honest:
 *
 *  1. **Fixture guard.** Only events for THIS match's fixture may touch it —
 *     mirroring the market_id guard every other WS event uses. A score/odds
 *     tick for a *different* fixture must not mutate the match shown here.
 *
 *  2. **Status changes only on real lifecycle transitions.** `kickoff` → live,
 *     `full_time` → finished; every other event (score, half_time, and stray
 *     `odds` re-broadcasts) updates the live_state but NEVER fabricates "live".
 *     Otherwise a scheduled match that receives any tick would be shown as LIVE
 *     forever, and a finished match would flip back to live — the false-LIVE bug.
 */
export function applyMatchState(match: Match, ev: MatchStateEvent): Match {
  const d = ev.data ?? {};

  if (ev.fixture_id && match.fixture_id && ev.fixture_id !== match.fixture_id) {
    return match;
  }

  // Team sheets arrive on their own match_state sub-event.
  if (d.event === "lineup") {
    return { ...match, lineups: (d.payload as Lineups) ?? match.lineups };
  }

  const nextStatus: Match["status"] =
    d.event === "full_time" ? "ft" : d.event === "kickoff" ? "live" : match.status;

  // Merge onto the last-known live_state so a sparse or partial tick can never
  // wipe the score or stats: keep the last good value for any omitted field.
  const prevLs = match.live_state;
  const incoming = (d.payload ?? {}) as ScoreTick;
  return {
    ...match,
    status: nextStatus,
    live_state: {
      minute: incoming.minute ?? prevLs.minute,
      period:
        incoming.period ?? prevLs.period ?? (nextStatus === "ft" ? "FT" : undefined),
      home_score: incoming.home_goals ?? prevLs.home_score ?? 0,
      away_score: incoming.away_goals ?? prevLs.away_score ?? 0,
      possession: incoming.possession ?? prevLs.possession,
      stats: incoming.stats ?? prevLs.stats,
    },
    // team sheets carry across score ticks
    lineups: match.lineups,
  };
}
