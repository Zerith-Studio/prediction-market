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
}

// Registry — the demo set: the 1X2 mutex trio, a totals line, BTTS, and two
// precision pools.
var Registry = []Template{
	{Key: "home_win", Type: "binary", Title: "%s to win", Rule: "Settles YES if %s win in regulation (90' + stoppage).", MutexGroup: "result"},
	{Key: "draw", Type: "binary", Title: "%s vs %s: draw", Rule: "Settles YES if the match ends level in regulation.", MutexGroup: "result"},
	{Key: "away_win", Type: "binary", Title: "%s to win", Rule: "Settles YES if %s win in regulation (90' + stoppage).", MutexGroup: "result"},
	{Key: "over_2_5", Type: "binary", Title: "Over 2.5 goals", Rule: "Settles YES with 3+ total goals in regulation.", MutexGroup: "total_goals"},
	{Key: "btts", Type: "binary", Title: "Both teams to score", Rule: "Settles YES if both teams score in regulation."},
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
	case "home_win":
		return fmt.Sprintf(t.Title, home)
	case "away_win":
		return fmt.Sprintf(t.Title, away)
	case "draw":
		return fmt.Sprintf(t.Title, home, away)
	default:
		return t.Title
	}
}

// RenderRule renders the settlement rule text.
func (t Template) RenderRule(home, away string) string {
	switch t.Key {
	case "home_win":
		return fmt.Sprintf(t.Rule, home)
	case "away_win":
		return fmt.Sprintf(t.Rule, away)
	default:
		return t.Rule
	}
}
