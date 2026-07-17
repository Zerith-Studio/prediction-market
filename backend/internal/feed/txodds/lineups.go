package txodds

import (
	"sort"
	"strconv"
	"strings"
)

// --- wire shapes (scores feed `lineups`) ------------------------------------
//
// Field tags use the spec's camelCase; Go's json matches struct fields
// case-insensitively, so PascalCase frames decode too. One wireLineupTeam per
// participant; each carries the team's squad for the fixture.

type wireLineupTeam struct {
	PreferredName string             `json:"preferredName"` // team name
	NormativeID   int64              `json:"normativeId"`
	Lineups       []wirePlayerLineup `json:"lineups"`
}

type wirePlayerLineup struct {
	RosterNumber string `json:"rosterNumber"`
	Starter      bool   `json:"starter"`
	Starred      bool   `json:"starred"` // captain / featured
	PositionID   int    `json:"positionId"`
	UnitID       int    `json:"unitId"`
	StatusID     int    `json:"statusId"`
	Player       struct {
		PreferredName string `json:"preferredName"`
		NormativeID   int64  `json:"normativeId"`
	} `json:"player"`
}

// --- normalized payload (emitted as EventLineup) ----------------------------

// LineupPlayer is one squad member. number + name + starter are always real;
// position is "" when the feed's positionId isn't in our (PDF-gated) label
// table — we never guess a label. unit is the raw line grouping used by the
// pitch view.
type LineupPlayer struct {
	Number   string `json:"number"`
	Name     string `json:"name"`
	Position string `json:"position,omitempty"`
	Unit     int    `json:"unit"`
	Captain  bool   `json:"captain,omitempty"`
}

type TeamLineup struct {
	Team      string         `json:"team"`
	Formation string         `json:"formation,omitempty"`
	Starters  []LineupPlayer `json:"starters"`
	Subs      []LineupPlayer `json:"subs"`
}

// Lineups is the per-fixture team-sheet payload.
type Lineups struct {
	Home *TeamLineup `json:"home,omitempty"`
	Away *TeamLineup `json:"away,omitempty"`
}

// normalizeLineups maps the wire team sheets onto home/away for this fixture by
// matching each team's normativeId to the score frame's home/away participant
// ids, falling back to array order. Returns nil when no players are present, so
// callers can skip empty emits.
func normalizeLineups(teams []wireLineupTeam, homeID, awayID int64) *Lineups {
	if len(teams) == 0 {
		return nil
	}
	out := &Lineups{}
	for i, t := range teams {
		tl := normalizeTeam(t)
		if tl == nil {
			continue
		}
		switch {
		case homeID != 0 && t.NormativeID == homeID:
			out.Home = tl
		case awayID != 0 && t.NormativeID == awayID:
			out.Away = tl
		case out.Home == nil && i == 0:
			out.Home = tl
		case out.Away == nil:
			out.Away = tl
		}
	}
	if out.Home == nil && out.Away == nil {
		return nil
	}
	return out
}

func normalizeTeam(t wireLineupTeam) *TeamLineup {
	if len(t.Lineups) == 0 {
		return nil
	}
	tl := &TeamLineup{Team: t.PreferredName}
	for _, p := range t.Lineups {
		lp := LineupPlayer{
			Number:   p.RosterNumber,
			Name:     p.Player.PreferredName,
			Position: positionLabel(p.PositionID),
			Unit:     p.UnitID,
			Captain:  p.Starred,
		}
		if p.Starter {
			tl.Starters = append(tl.Starters, lp)
		} else {
			tl.Subs = append(tl.Subs, lp)
		}
	}
	sort.SliceStable(tl.Starters, func(i, j int) bool { return byUnitThenNumber(tl.Starters[i], tl.Starters[j]) })
	sort.SliceStable(tl.Subs, func(i, j int) bool { return numLess(tl.Subs[i].Number, tl.Subs[j].Number) })
	tl.Formation = deriveFormation(tl.Starters)
	return tl
}

// deriveFormation counts starters per outfield line (unit), skipping the
// goalkeeper line (the single-player line at the lowest unit). Returns "" unless
// we have a clean 11 across ≥4 lines — the feed carries no formation string, so
// this is a best-effort derivation, not an authoritative value.
func deriveFormation(starters []LineupPlayer) string {
	if len(starters) != 11 {
		return ""
	}
	counts := map[int]int{}
	units := []int{}
	for _, p := range starters {
		if _, seen := counts[p.Unit]; !seen {
			units = append(units, p.Unit)
		}
		counts[p.Unit]++
	}
	sort.Ints(units)
	if len(units) < 4 {
		return "" // not enough line separation to trust a formation
	}
	// Drop the GK line (lowest unit, exactly one keeper); the rest is DEF-…-FWD.
	if counts[units[0]] != 1 {
		return ""
	}
	parts := make([]string, 0, len(units)-1)
	for _, u := range units[1:] {
		parts = append(parts, strconv.Itoa(counts[u]))
	}
	return strings.Join(parts, "-")
}

func byUnitThenNumber(a, b LineupPlayer) bool {
	if a.Unit != b.Unit {
		return a.Unit < b.Unit
	}
	return numLess(a.Number, b.Number)
}

func numLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

// positionLabel maps a feed positionId to a display label. The code table lives
// only in TxODDS's downloadable soccer-feed PDF (not the JSON/HTML spec), so
// this is intentionally empty until the codes are confirmed: an unknown code
// returns "" and the UI shows number + name with no fabricated position. Fill
// this map — in one place — once the PDF codes are in hand.
func positionLabel(id int) string {
	return positionLabels[id]
}

var positionLabels = map[int]string{
	// e.g. 1: "Goalkeeper", 2: "Right Back", … — pending confirmed TxODDS codes.
}
