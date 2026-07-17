package txodds

import "strconv"

// Soccer stat-key catalog (txline scores/soccer-feed, confirmed against the
// published spec). Base keys 1–8 are the per-participant totals; a period is
// encoded as base + 1000*prefix.
//
//	prefix 0=Total 1=H1 2=HT 3=H2 4=ET1 5=ET2 6=PE 7=ETTotal
//	e.g. "1001" = Participant-1 H1 goals, "8" = Participant-2 total corners.
//
// Keys 1/2 (goals) are also derivable from the action stream; the stats map is
// authoritative and overrides the counter (see applyStats).
const (
	statP1Goals  = 1
	statP2Goals  = 2
	statP1Yellow = 3
	statP2Yellow = 4
	statP1Red    = 5
	statP2Red    = 6
	statP1Corner = 7
	statP2Corner = 8

	periodTotal = 0
	periodH1    = 1
)

// statValue reads a stats-map entry (keys are stringified ints, values may be
// float64 from JSON or string). base is 1..8, prefix a period prefix above.
func statValue(stats map[string]any, base, prefix int) (int, bool) {
	return intFromMap(stats, strconv.Itoa(base+1000*prefix))
}

// intFromMap coerces a JSON value (float64 or numeric string) at key to an int.
func intFromMap(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}
