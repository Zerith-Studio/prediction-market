// Package templates is the market-template registry: which markets get created
// per fixture (feed-driven auto creation, PROJECT_PLAN §3), their RFQ mutex
// groups (ADR 0004 — mutually exclusive legs grey out in the combo builder),
// and per-template precision scales (ADR 0006 C4 σ-normalization).
package templates

import (
	"crypto/sha256"
	"fmt"
)

type Template struct {
	Key   string
	Type  string // binary | precision
	Title string // %s placeholders: home, away where applicable
	Rule  string
	// MutexGroup: two legs from the same group on the same match cannot be
	// combined in one RFQ combo ("" = compatible with everything).
	MutexGroup string
	// Scale is the σ-normalization s for precision templates (ADR 0006):
	// score = 1/(1+|guess−actual|/s)^k.
	Scale float64
	// ResolvesAt marks when the market's outcome is final and may settle:
	// "" (default) = full time; "half_time" = as soon as the half-time score is
	// final, via lifecycle.ResolveHalfTime. The full-time cascade re-settles any
	// half-time market it missed (idempotent), so this is a fast path, not a gate.
	ResolvesAt string
}

// Registry — the 1X2 mutex trio, totals, BTTS, two precision pools, and the
// two markets TxLINE's free World Cup tier prices live (dnb_home, ou_1h_075 —
// the MM bot only quotes markets with a real feed price).
var Registry = []Template{
	// --- full-time result (1X2 mutex trio) ---
	{Key: "home_win", Type: "binary", Title: "%s to win", Rule: "Settles YES if %s win in regulation (90' + stoppage).", MutexGroup: "result"},
	{Key: "draw", Type: "binary", Title: "%s vs %s: draw", Rule: "Settles YES if the match ends level in regulation.", MutexGroup: "result"},
	{Key: "away_win", Type: "binary", Title: "%s to win", Rule: "Settles YES if %s win in regulation (90' + stoppage).", MutexGroup: "result"},
	// --- draw no bet (TxLINE prices dnb_home live) ---
	{Key: "dnb_home", Type: "binary", Title: "%s to win · draw no bet", Rule: "Settles YES if %s win in regulation; VOID (refund) on a draw.", MutexGroup: "result_dnb"},
	{Key: "dnb_away", Type: "binary", Title: "%s to win · draw no bet", Rule: "Settles YES if %s win in regulation; VOID (refund) on a draw.", MutexGroup: "result_dnb"},
	// --- double chance (correlated → mutex so a combo can't stack them) ---
	{Key: "dc_1x", Type: "binary", Title: "%s or draw", Rule: "Settles YES if %s win or draw in regulation (double chance).", MutexGroup: "double_chance"},
	{Key: "dc_12", Type: "binary", Title: "%s or %s (no draw)", Rule: "Settles YES unless the match ends in a draw.", MutexGroup: "double_chance"},
	{Key: "dc_x2", Type: "binary", Title: "Draw or %s", Rule: "Settles YES if %s win or draw in regulation (double chance).", MutexGroup: "double_chance"},
	// --- totals (correlated goal lines → mutex) ---
	{Key: "over_1_5", Type: "binary", Title: "Over 1.5 goals", Rule: "Settles YES with 2+ total goals in regulation.", MutexGroup: "total_goals"},
	{Key: "over_2_5", Type: "binary", Title: "Over 2.5 goals", Rule: "Settles YES with 3+ total goals in regulation.", MutexGroup: "total_goals"},
	{Key: "over_3_5", Type: "binary", Title: "Over 3.5 goals", Rule: "Settles YES with 4+ total goals in regulation.", MutexGroup: "total_goals"},
	// --- both teams to score / clean sheets ---
	{Key: "btts", Type: "binary", Title: "Both teams to score", Rule: "Settles YES if both teams score in regulation."},
	{Key: "cs_home", Type: "binary", Title: "%s clean sheet", Rule: "Settles YES if %s concede no goals in regulation."},
	{Key: "cs_away", Type: "binary", Title: "%s clean sheet", Rule: "Settles YES if %s concede no goals in regulation."},
	// --- half-time markets: settle at the break (lifecycle.ResolveHalfTime) ---
	{Key: "ht_home", Type: "binary", Title: "%s to lead at half-time", Rule: "Settles YES if %s lead at half-time.", MutexGroup: "ht_result", ResolvesAt: "half_time"},
	{Key: "ht_draw", Type: "binary", Title: "Level at half-time", Rule: "Settles YES if the match is level at half-time.", MutexGroup: "ht_result", ResolvesAt: "half_time"},
	{Key: "ht_away", Type: "binary", Title: "%s to lead at half-time", Rule: "Settles YES if %s lead at half-time.", MutexGroup: "ht_result", ResolvesAt: "half_time"},
	{Key: "ou_1h_075", Type: "binary", Title: "First-half goal", Rule: "Settles YES with 1+ goal before half time (O/U 0.75, 1H).", MutexGroup: "h1_goals", ResolvesAt: "half_time"},
	{Key: "ou_1h_15", Type: "binary", Title: "First half over 1.5", Rule: "Settles YES with 2+ goals before half-time (O/U 1.5, 1H).", MutexGroup: "h1_goals", ResolvesAt: "half_time"},
	{Key: "btts_1h", Type: "binary", Title: "Both teams to score in 1H", Rule: "Settles YES if both teams score before half-time.", ResolvesAt: "half_time"},
	// --- precision pools ---
	{Key: "precision_total_goals", Type: "precision", Title: "Total goals — precision", Rule: "Closest to total goals wins the pool (σ-scored, k=2).", Scale: 2},
	{Key: "precision_total_passes", Type: "precision", Title: "Total passes — precision", Rule: "Closest to total completed passes wins the pool (σ-scored, k=2).", Scale: 100},
}

// ByKey looks a template up; ok=false for unknown keys.
func ByKey(key string) (Template, bool) {
	for _, t := range Registry {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}

// MarketID derives the deterministic [u8;32] identifier from
// (fixture, template) per interface-contract.md §0.
func MarketID(fixtureID, templateKey string) [32]byte {
	return sha256.Sum256([]byte("pitchmarket:" + fixtureID + ":" + templateKey))
}

// Title renders the display title for a fixture's sides.
func (t Template) RenderTitle(home, away string) string {
	switch t.Key {
	case "home_win", "dnb_home", "cs_home", "ht_home", "dc_1x":
		return fmt.Sprintf(t.Title, home)
	case "away_win", "dnb_away", "cs_away", "ht_away", "dc_x2":
		return fmt.Sprintf(t.Title, away)
	case "draw", "dc_12":
		return fmt.Sprintf(t.Title, home, away)
	default:
		return t.Title
	}
}

// RenderRule renders the settlement rule text.
func (t Template) RenderRule(home, away string) string {
	switch t.Key {
	case "home_win", "dnb_home", "cs_home", "ht_home", "dc_1x":
		return fmt.Sprintf(t.Rule, home)
	case "away_win", "dnb_away", "cs_away", "ht_away", "dc_x2":
		return fmt.Sprintf(t.Rule, away)
	default:
		return t.Rule
	}
}
