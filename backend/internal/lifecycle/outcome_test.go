package lifecycle

import (
	"testing"

	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
)

// TestBinaryOutcome pins the settlement logic of every binary template — the
// full-time ones off the FT score, the half-time ones off the HT fields.
func TestBinaryOutcome(t *testing.T) {
	ft := FinalScore{HomeGoals: 2, AwayGoals: 1, HTHomeGoals: 1, HTAwayGoals: 0} // 2-1, HT 1-0
	draw := FinalScore{HomeGoals: 1, AwayGoals: 1, HTHomeGoals: 1, HTAwayGoals: 1}
	nilNil := FinalScore{}

	cases := []struct {
		key  string
		f    FinalScore
		want string
	}{
		// full-time result
		{"home_win", ft, "yes"}, {"away_win", ft, "no"}, {"draw", ft, "no"}, {"draw", draw, "yes"},
		// draw no bet
		{"dnb_home", ft, "yes"}, {"dnb_away", ft, "no"},
		{"dnb_home", draw, "void"}, {"dnb_away", draw, "void"},
		// double chance (2-1 home win)
		{"dc_1x", ft, "yes"}, {"dc_12", ft, "yes"}, {"dc_x2", ft, "no"},
		{"dc_1x", draw, "yes"}, {"dc_x2", draw, "yes"}, {"dc_12", draw, "no"},
		// totals (3 goals)
		{"over_1_5", ft, "yes"}, {"over_2_5", ft, "yes"}, {"over_3_5", ft, "no"},
		{"over_3_5", FinalScore{HomeGoals: 2, AwayGoals: 2}, "yes"},
		// btts / clean sheets
		{"btts", ft, "yes"}, {"cs_home", ft, "no"}, {"cs_away", ft, "no"},
		{"cs_home", FinalScore{HomeGoals: 1, AwayGoals: 0}, "yes"},
		{"cs_away", FinalScore{HomeGoals: 0, AwayGoals: 1}, "yes"},
		// half-time (HT 1-0)
		{"ht_home", ft, "yes"}, {"ht_draw", ft, "no"}, {"ht_away", ft, "no"}, {"ht_draw", draw, "yes"},
		{"ou_1h_075", ft, "yes"}, {"ou_1h_075", nilNil, "no"},
		{"ou_1h_15", ft, "no"}, {"ou_1h_15", draw, "yes"},
		{"btts_1h", ft, "no"}, {"btts_1h", draw, "yes"},
	}
	for _, c := range cases {
		got, ok := binaryOutcome(c.key, c.f)
		if !ok {
			t.Errorf("%s: not resolvable", c.key)
			continue
		}
		if got != c.want {
			t.Errorf("%s @ %+v: got %s, want %s", c.key, c.f, got, c.want)
		}
	}

	// Every binary template MUST have a binaryOutcome mapping — guards against
	// adding a template without wiring its resolution.
	for _, tpl := range templates.Registry {
		if tpl.Type != "binary" {
			continue
		}
		if _, ok := binaryOutcome(tpl.Key, ft); !ok {
			t.Errorf("binary template %q has no binaryOutcome mapping", tpl.Key)
		}
	}
}
