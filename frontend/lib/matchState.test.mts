// Pure-logic regression test for the match_state reducer that fixes the false
// "LIVE" status bug. No test runner is wired up in this app, so run it directly:
//
//   cd frontend && node --experimental-strip-types lib/matchState.test.mts
//
// It guards the two invariants that keep the live badge honest: the fixture
// guard, and "status changes only on real lifecycle transitions" (a stray
// odds/score tick must never fabricate LIVE, and a finished match must never
// regress to live).

import { applyMatchState } from "./matchState.ts";
import type { Match } from "./types.ts";

const base: Match = {
  id: "m1",
  fixture_id: "SPAIN",
  home: "Spain",
  away: "Argentina",
  kickoff_at: "2026-07-19T19:00:00Z",
  status: "scheduled",
  live_state: {},
  lineups: null,
};

let fail = 0;
const ok = (name: string, cond: boolean) => {
  console.log((cond ? "PASS" : "FAIL") + " — " + name);
  if (!cond) fail++;
};
const ev = (fixture_id: string | undefined, event: string, payload: unknown) =>
  ({ fixture_id, data: { event, payload } }) as Parameters<typeof applyMatchState>[1];

// Events that PREVIOUSLY forced LIVE must now leave a scheduled match alone.
ok(
  "scheduled + odds(own fixture) stays scheduled",
  applyMatchState(base, ev("SPAIN", "odds", { prices: { over_2_5: 40 } })).status === "scheduled"
);
ok(
  "scheduled + score(OTHER fixture) ignored — same ref (fixture guard)",
  applyMatchState(base, ev("FRANCE", "score", { minute: 70, home_goals: 3 })) === base
);
ok(
  "scheduled + score(own fixture, pre-kickoff) stays scheduled",
  applyMatchState(base, ev("SPAIN", "score", { minute: 10 })).status === "scheduled"
);

// Real lifecycle transitions must still work.
ok("scheduled + kickoff -> live", applyMatchState(base, ev("SPAIN", "kickoff", { minute: 1 })).status === "live");
ok(
  "live + full_time -> ft",
  applyMatchState({ ...base, status: "live" }, ev("SPAIN", "full_time", { minute: 90 })).status === "ft"
);

// A finished match must never regress to live on a stray tick.
ok(
  "ft + odds(own) stays ft (no regression)",
  applyMatchState({ ...base, status: "ft" }, ev("SPAIN", "odds", { prices: { x: 1 } })).status === "ft"
);

// A sparse tick must not wipe the last-known score.
{
  const w = applyMatchState(
    { ...base, status: "live", live_state: { home_score: 2, away_score: 1, minute: 50 } },
    ev("SPAIN", "score", { minute: 55 })
  );
  ok(
    "sparse tick keeps prior score",
    w.live_state.home_score === 2 && w.live_state.away_score === 1 && w.live_state.minute === 55
  );
}

// Events with no fixture_id still apply (backward-compatible).
ok(
  "event without fixture_id still applies kickoff",
  applyMatchState(base, { data: { event: "kickoff" } }).status === "live"
);

console.log(fail === 0 ? "\nALL PASS" : `\n${fail} FAILURES`);
process.exit(fail === 0 ? 0 : 1);
