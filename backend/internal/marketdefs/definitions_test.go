package marketdefs

import "testing"

func TestWorldCupDefinitionsIncludeCapabilityGatedPlayerMarkets(t *testing.T) {
	defs := Registry()
	var foundWinner, foundGoldenBoot, foundGoldenGlove bool
	for _, d := range defs {
		switch d.Key {
		case "world_cup_winner":
			foundWinner = d.Scope == "competition" && d.Type == "binary"
		case "golden_boot":
			foundGoldenBoot = d.Scope == "player" && d.ResolutionSource == "manual_required"
			if len(d.TxLineRequirements) == 0 {
				t.Fatalf("golden_boot must declare TxLINE data requirements")
			}
		case "golden_glove":
			foundGoldenGlove = d.Scope == "player" && d.ResolutionSource == "manual_required"
		}
	}
	if !foundWinner || !foundGoldenBoot || !foundGoldenGlove {
		t.Fatalf("missing definitions: winner=%v golden_boot=%v golden_glove=%v", foundWinner, foundGoldenBoot, foundGoldenGlove)
	}
}
