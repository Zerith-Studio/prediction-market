package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
)

func TestCustomCompetitionMarketCanBeCreatedWithoutFixture(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)

	req := store.CustomMarketRequest{
		Scope:            "competition",
		TemplateKey:      "world_cup_winner",
		Type:             "binary",
		Title:            "Argentina to win the World Cup",
		Rule:             "Settles YES if Argentina are the official competition winner.",
		CompetitionID:    "72",
		SubjectType:      "team",
		SubjectID:        "argentina",
		ResolutionSource: "manual_required",
		RuleJSON:         json.RawMessage(`{"kind":"team_tournament_result","target":"winner"}`),
	}

	marketID, err := st.CreateCustomMarket(ctx, req)
	if err != nil {
		t.Fatalf("CreateCustomMarket: %v", err)
	}
	if marketID != store.CustomMarketID(req) {
		t.Fatalf("market id is not deterministic")
	}

	row, err := st.GetMarket(ctx, marketID)
	if err != nil {
		t.Fatalf("GetMarket: %v", err)
	}
	if row.MatchID != "" {
		t.Fatalf("custom competition market should not require a fixture, got match_id %q", row.MatchID)
	}
	if row.Scope != "competition" || row.CompetitionID != "72" || row.SubjectType != "team" || row.SubjectID != "argentina" {
		t.Fatalf("custom metadata not persisted: %+v", row)
	}
	if row.ResolutionSource != "manual_required" {
		t.Fatalf("resolution source = %q, want manual_required", row.ResolutionSource)
	}
	if !json.Valid(row.RuleJSON) {
		t.Fatalf("rule_json is not valid json: %s", row.RuleJSON)
	}
	if got, want := models.HashString(marketID), models.HashString(store.CustomMarketID(req)); got != want {
		t.Fatalf("hash string = %s, want %s", got, want)
	}
}

func TestCustomFixtureMarketCanBeMappedToFixture(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)

	matchID, err := st.UpsertMatch(ctx, "fixture-777", "Spain", "Japan", time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	req := store.CustomMarketRequest{
		Scope:            "fixture",
		FixtureID:        "fixture-777",
		MatchID:          matchID,
		TemplateKey:      "home_corners_over_5_5",
		Type:             "binary",
		Title:            "Spain over 5.5 corners",
		Rule:             "Settles YES if Spain record 6+ corners.",
		ResolutionSource: "manual_required",
		RuleJSON:         json.RawMessage(`{"kind":"fixture_team_stat","stat":"corners","team":"home","operator":">","threshold":5.5}`),
	}

	marketID, err := st.CreateCustomMarket(ctx, req)
	if err != nil {
		t.Fatalf("CreateCustomMarket: %v", err)
	}
	row, err := st.GetMarket(ctx, marketID)
	if err != nil {
		t.Fatal(err)
	}
	if row.MatchID != matchID || row.Scope != "fixture" {
		t.Fatalf("fixture mapping not persisted: %+v", row)
	}
}

func TestResolutionAttemptIsRecordedWithEvidence(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)

	req := store.CustomMarketRequest{
		Scope:            "competition",
		TemplateKey:      "golden_boot",
		Type:             "binary",
		Title:            "Kylian Mbappe Golden Boot",
		Rule:             "Settles YES if Mbappe is official top scorer.",
		CompetitionID:    "72",
		SubjectType:      "player",
		SubjectID:        "mbappe",
		ResolutionSource: "manual_required",
		RuleJSON:         json.RawMessage(`{"kind":"player_leaderboard","stat":"goals","tie_policy":"dead_heat"}`),
	}
	marketID, err := st.CreateCustomMarket(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	evidence := json.RawMessage(`{"manual":true,"reason":"official leaderboard checked","source":"admin"}`)
	if err := st.RecordResolutionAttempt(ctx, store.ResolutionAttempt{
		MarketID:  marketID,
		Actor:     "admin-wallet",
		Outcome:   "yes",
		Evidence:  evidence,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordResolutionAttempt: %v", err)
	}

	attempts, err := st.ResolutionAttempts(ctx, marketID)
	if err != nil {
		t.Fatalf("ResolutionAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts len = %d, want 1", len(attempts))
	}
	if attempts[0].Actor != "admin-wallet" || attempts[0].Outcome != "yes" {
		t.Fatalf("attempt not persisted: %+v", attempts[0])
	}
	var gotEvidence, wantEvidence map[string]any
	if err := json.Unmarshal(attempts[0].Evidence, &gotEvidence); err != nil {
		t.Fatalf("stored evidence is not json: %v", err)
	}
	if err := json.Unmarshal(evidence, &wantEvidence); err != nil {
		t.Fatalf("test evidence is not json: %v", err)
	}
	if gotEvidence["reason"] != wantEvidence["reason"] || gotEvidence["manual"] != wantEvidence["manual"] {
		t.Fatalf("evidence = %+v, want %+v", gotEvidence, wantEvidence)
	}
}
