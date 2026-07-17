import type { Market } from "./types";

// Short human label per template — the meta line on market cards and in the
// command palette. Shared so both surfaces name a template the same way.
export const KIND: Record<string, string> = {
  home_win: "Match result",
  draw: "Match result",
  away_win: "Match result",
  dnb_home: "Draw no bet",
  over_2_5: "Total goals",
  btts: "Both to score",
  ou_1h_075: "First half",
  precision_total_goals: "Precision",
  precision_total_passes: "Precision",
};

export function kindOf(m: Market): string {
  return KIND[m.template_key] ?? m.type;
}
