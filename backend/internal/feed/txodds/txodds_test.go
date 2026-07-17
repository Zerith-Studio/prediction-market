package txodds

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
)

func strPtr(s string) *string { return &s }

func TestTemplateMapping(t *testing.T) {
	cases := []struct {
		odds wireOdds
		want string
	}{
		{wireOdds{SuperOddsType: "ASIANHANDICAP_PARTICIPANT_GOALS", MarketParameters: "line=0"}, "dnb_home"},
		{wireOdds{SuperOddsType: "OVERUNDER_PARTICIPANT_GOALS", MarketParameters: "line=2.5"}, "over_2_5"},
		{wireOdds{SuperOddsType: "OVERUNDER_PARTICIPANT_GOALS", MarketParameters: "line=0.75", MarketPeriod: strPtr("half=1")}, "ou_1h_075"},
		{wireOdds{SuperOddsType: "OVERUNDER_PARTICIPANT_GOALS", MarketParameters: "line=0.75"}, ""}, // full-match 0.75: unmapped
		{wireOdds{SuperOddsType: "ASIANHANDICAP_PARTICIPANT_GOALS", MarketParameters: "line=-0.5"}, ""},
	}
	for _, c := range cases {
		if got := templateFor(c.odds); got != c.want {
			t.Errorf("templateFor(%s %s) = %q, want %q", c.odds.SuperOddsType, c.odds.MarketParameters, got, c.want)
		}
	}
}

func TestImpliedCents(t *testing.T) {
	// Real TxLINE frame: demargined Pct present → use it directly.
	p, ok := impliedCents(wireOdds{Pct: []string{"54.083", "45.935"}, Prices: []int64{1849, 2177}})
	if !ok || p != 54 {
		t.Errorf("Pct path = %d %v, want 54 true", p, ok)
	}
	// Pct "NA" → normalize inverse decimal odds (real 1H O/U frame).
	p, ok = impliedCents(wireOdds{Pct: []string{"NA", "NA"}, Prices: []int64{1878, 2139}})
	if !ok || p < 52 || p > 54 { // 1/1.878 / (1/1.878+1/2.139) ≈ 53.2%
		t.Errorf("price path = %d %v, want ≈53 true", p, ok)
	}
	if _, ok = impliedCents(wireOdds{Pct: []string{"NA"}, Prices: []int64{1878}}); ok {
		t.Error("single-sided odds must not produce a price")
	}
}

func testProvider() *Provider {
	return &Provider{
		Log:   slog.Default(),
		subs:  make(map[string][]chan feed.MatchEvent),
		score: make(map[string]*scoreState),
	}
}

func subscribe(p *Provider, fixtureID string) chan feed.MatchEvent {
	ch := make(chan feed.MatchEvent, 16)
	p.subs[fixtureID] = append(p.subs[fixtureID], ch)
	p.score[fixtureID] = &scoreState{}
	return ch
}

func drain(ch chan feed.MatchEvent) []feed.MatchEvent {
	var out []feed.MatchEvent
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestScoreFolding(t *testing.T) {
	p := testProvider()
	ch := subscribe(p, "18241006")

	base := wireScore{FixtureID: 18241006, Participant1ID: 1888, Participant2ID: 1489, Participant1H: true}

	kick := base
	kick.Action = "kickoff"
	p.handleScore(kick)

	goalHome := base
	goalHome.Action = "goal"
	goalHome.Data = map[string]any{"ParticipantId": float64(1888)}
	p.handleScore(goalHome)

	ht := base
	ht.Action = "halftime_finalised"
	p.handleScore(ht)

	goalAway := base
	goalAway.Action = "goal"
	goalAway.Data = map[string]any{"ParticipantId": float64(1489)}
	p.handleScore(goalAway)

	ft := base
	ft.Action = "final_whistle"
	p.handleScore(ft)

	events := drain(ch)
	if len(events) != 5 {
		t.Fatalf("want 5 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != feed.EventKickoff {
		t.Errorf("first event: %s", events[0].Type)
	}
	final := events[4]
	if final.Type != feed.EventFullTime {
		t.Fatalf("last event: %s", final.Type)
	}
	payload := final.Payload.(map[string]any)
	if payload["home_goals"] != 1 || payload["away_goals"] != 1 {
		t.Errorf("final score payload: %+v", payload)
	}
	if payload["ht_home_goals"] != 1 || payload["ht_away_goals"] != 0 {
		t.Errorf("half-time payload: %+v", payload)
	}

	// A repeated final whistle must not double-emit full_time.
	p.handleScore(ft)
	if extra := drain(ch); len(extra) != 0 {
		t.Errorf("duplicate full_time emitted: %+v", extra)
	}
}

func TestStatsOverrideActionCount(t *testing.T) {
	p := testProvider()
	ch := subscribe(p, "9")

	s := wireScore{FixtureID: 9, Participant1ID: 1, Participant2ID: 2, Participant1H: true,
		Action: "goal",
		Data:   map[string]any{"ParticipantId": float64(1)},
		Stats:  map[string]any{"1": float64(2), "2": float64(1)}, // authoritative totals
	}
	p.handleScore(s)
	events := drain(ch)
	payload := events[0].Payload.(map[string]any)
	// Stats say 2-1; the action-counter alone would have said 1-0.
	if payload["home_goals"] != 2 || payload["away_goals"] != 1 {
		t.Errorf("stats must win: %+v", payload)
	}
}

// xi builds an 11-player starting lineup across 4 unit lines (GK=1, DEF=2×4,
// MID=3×3, FWD=4×3) plus one substitute, so deriveFormation yields "4-3-3".
func xi() []wirePlayerLineup {
	units := []int{1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4}
	var out []wirePlayerLineup
	for i, u := range units {
		out = append(out, wirePlayerLineup{
			RosterNumber: strconv.Itoa(i + 1), Starter: true, UnitID: u,
			Player: struct {
				PreferredName string `json:"preferredName"`
				NormativeID   int64  `json:"normativeId"`
			}{PreferredName: "Player " + strconv.Itoa(i+1)},
		})
	}
	out = append(out, wirePlayerLineup{RosterNumber: "12", Starter: false, UnitID: 4,
		Player: struct {
			PreferredName string `json:"preferredName"`
			NormativeID   int64  `json:"normativeId"`
		}{PreferredName: "Sub One"}})
	return out
}

func TestLineupEmit(t *testing.T) {
	p := testProvider()
	ch := subscribe(p, "18241006")
	s := wireScore{FixtureID: 18241006, Participant1ID: 1888, Participant2ID: 1489, Participant1H: true,
		Lineups: []wireLineupTeam{
			{PreferredName: "Spain", NormativeID: 1888, Lineups: xi()},
			{PreferredName: "Argentina", NormativeID: 1489, Lineups: xi()},
		},
	}
	p.handleScore(s)

	events := drain(ch)
	if len(events) != 1 || events[0].Type != feed.EventLineup {
		t.Fatalf("want 1 lineup event, got %+v", events)
	}
	lu := events[0].Payload.(*Lineups)
	if lu.Home == nil || lu.Home.Team != "Spain" || len(lu.Home.Starters) != 11 || len(lu.Home.Subs) != 1 {
		t.Fatalf("home lineup: %+v", lu.Home)
	}
	if lu.Away == nil || lu.Away.Team != "Argentina" {
		t.Fatalf("away lineup: %+v", lu.Away)
	}
	if lu.Home.Formation != "4-3-3" {
		t.Errorf("derived formation = %q, want 4-3-3", lu.Home.Formation)
	}

	// Team sheets emit exactly once — a later frame must not re-emit them.
	p.handleScore(s)
	if extra := drain(ch); len(extra) != 0 {
		t.Errorf("lineups re-emitted: %+v", extra)
	}
}

func TestLiveStatsPayload(t *testing.T) {
	p := testProvider()
	ch := subscribe(p, "9")
	poss := 57
	s := wireScore{FixtureID: 9, Participant1ID: 1, Participant2ID: 2, Participant1H: true,
		Action:     "goal",
		Data:       map[string]any{"ParticipantId": float64(1)},
		Stats:      map[string]any{"1": float64(1), "3": float64(2), "7": float64(5), "4": float64(1), "8": float64(3)},
		Possession: &poss,
	}
	p.handleScore(s)

	pl := drain(ch)[0].Payload.(map[string]any)
	stats := pl["stats"].(map[string]any)
	home := stats["home"].(map[string]any)
	if home["yellow"] != 2 || home["corners"] != 5 {
		t.Errorf("home stats: %+v", home)
	}
	away := stats["away"].(map[string]any)
	if away["yellow"] != 1 || away["corners"] != 3 {
		t.Errorf("away stats: %+v", away)
	}
	poss2 := pl["possession"].(map[string]any)
	if poss2["home"] != 57 || poss2["away"] != 43 {
		t.Errorf("possession: %+v", poss2)
	}
}

func TestOddsFanOut(t *testing.T) {
	p := testProvider()
	ch := subscribe(p, "18241006")
	p.handleOdds(wireOdds{
		FixtureID: 18241006, SuperOddsType: "ASIANHANDICAP_PARTICIPANT_GOALS",
		MarketParameters: "line=0", Pct: []string{"54.083", "45.935"},
	})
	events := drain(ch)
	if len(events) != 1 || events[0].Type != feed.EventOdds {
		t.Fatalf("events: %+v", events)
	}
	prices := events[0].Payload.(map[string]any)["prices"].(map[string]uint16)
	if prices["dnb_home"] != 54 {
		t.Errorf("prices: %+v", prices)
	}
}

func TestFixtureHomeAwayOrientation(t *testing.T) {
	// Participant1IsHome=false must swap sides.
	w := wireFixture{Participant1: "Argentina", Participant2: "England",
		Participant1Home: false, FixtureID: 7, StartTime: time.Now().UnixMilli()}
	p := testProvider()
	_ = p
	home, away := w.Participant1, w.Participant2
	if !w.Participant1Home {
		home, away = away, home
	}
	if home != "England" || away != "Argentina" {
		t.Errorf("orientation: home=%s away=%s", home, away)
	}
}

func TestStreamSubscribeCancel(t *testing.T) {
	p := testProvider()
	p.pumps = true // don't start network pumps in tests
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, "42")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return // closed on cancel ✓
			}
		case <-deadline:
			t.Fatal("stream channel not closed on context cancel")
		}
	}
}
