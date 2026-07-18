"use client";

import { useEffect, useState } from "react";
import { api, configured } from "./api";
import type { Market, Match } from "./types";

export interface RelatedMarkets {
  markets: Market[]; // ranked: same-match first, then same-team
  matchById: Map<string, Match>; // resolves flags for fixture cards
}

// Best-effort team-name match: case-insensitive, ignoring surrounding whitespace.
function sameTeam(a: string, b: string): boolean {
  return a.trim().toLowerCase() === b.trim().toLowerCase();
}

// Related markets for the market page, computed client-side from the existing
// /markets + /matches lists (no dedicated backend route). Ranked same-match
// first, then markets involving either team of the current match. Excludes the
// current market; open markets only. Fetched once — the related set is stable
// within a session, so it stays out of the live WS loop.
export function useRelatedMarkets(
  current: Market | null,
  match: Match | null,
  limit = 6,
): RelatedMarkets {
  const [state, setState] = useState<RelatedMarkets>({
    markets: [],
    matchById: new Map(),
  });

  const currentId = current?.market_id ?? null;
  const currentMatchId = current?.match_id || null;
  const home = match?.home ?? null;
  const away = match?.away ?? null;

  useEffect(() => {
    if (!configured() || !current) return;
    let alive = true;

    Promise.all([api.listMarkets("open"), api.listMatches()])
      .then(([markets, matches]) => {
        if (!alive) return;
        const matchById = new Map(matches.map((mt) => [mt.id, mt]));

        const teams = [home, away].filter(Boolean) as string[];
        const isSameTeam = (m: Market): boolean => {
          if (teams.length === 0) return false;
          // Fixture market on another match: does that match involve a team?
          const mt = m.match_id ? matchById.get(m.match_id) : undefined;
          if (mt && teams.some((t) => sameTeam(t, mt.home) || sameTeam(t, mt.away))) {
            return true;
          }
          // Team/player market: subject names a team.
          if (
            (m.scope === "team" || m.scope === "player") &&
            m.subject_id &&
            teams.some((t) => sameTeam(t, m.subject_id!))
          ) {
            return true;
          }
          return false;
        };

        const candidates = markets.filter((m) => m.market_id !== currentId);
        const sameMatch = currentMatchId
          ? candidates.filter((m) => m.match_id === currentMatchId)
          : [];
        const sameMatchIds = new Set(sameMatch.map((m) => m.market_id));
        const sameTeamOnly = candidates.filter(
          (m) => !sameMatchIds.has(m.market_id) && isSameTeam(m),
        );

        const ranked = [...sameMatch, ...sameTeamOnly].slice(0, limit);
        setState({ markets: ranked, matchById });
      })
      .catch(() => {
        // Related markets are best-effort — never surface an error on the page.
      });

    return () => {
      alive = false;
    };
  }, [currentId, currentMatchId, home, away, limit, current]);

  return state;
}
