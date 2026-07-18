// Package marketdefs describes predefined market templates for the admin market
// builder. Definitions are product metadata: they declare required inputs,
// resolution rules, and TxLINE dependencies. Created markets still become rows
// in store.markets and resolve through lifecycle/admin.
package marketdefs

import "encoding/json"

type Definition struct {
	Key                  string          `json:"key"`
	Scope                string          `json:"scope"` // fixture|competition|team|player|custom
	Type                 string          `json:"type"`  // binary|precision
	TitleTemplate        string          `json:"title_template"`
	RuleTemplate         string          `json:"rule_template"`
	ResolutionSource     string          `json:"resolution_source"`
	RuleJSON             json.RawMessage `json:"rule_json"`
	RequiredInputsSchema json.RawMessage `json:"required_inputs_schema"`
	TxLineRequirements   []string        `json:"txline_requirements"`
}

func Registry() []Definition {
	return []Definition{
		{
			Key:              "fixture_team_stat_threshold",
			Scope:            "fixture",
			Type:             "binary",
			TitleTemplate:    "{{team}} {{stat}} over {{threshold}}",
			RuleTemplate:     "Settles YES if {{team}} record more than {{threshold}} {{stat}}.",
			ResolutionSource: "manual_required",
			RuleJSON:         raw(`{"kind":"fixture_team_stat","team":"{{home|away}}","stat":"{{stat}}","operator":">","threshold":"{{number}}"}`),
			RequiredInputsSchema: raw(`{
				"required":["fixture_id","home","away","team","stat","threshold"],
				"properties":{"stat":{"enum":["corners","yellow_cards","red_cards","shots","shots_on_target"]}}
			}`),
			TxLineRequirements: []string{"fixture scores snapshot stats map for selected stat"},
		},
		{
			Key:              "world_cup_winner",
			Scope:            "competition",
			Type:             "binary",
			TitleTemplate:    "{{team}} to win the World Cup",
			RuleTemplate:     "Settles YES if {{team}} are the official World Cup winner.",
			ResolutionSource: "manual_required",
			RuleJSON:         raw(`{"kind":"team_tournament_result","target":"winner","tie_policy":"none"}`),
			RequiredInputsSchema: raw(`{
				"required":["competition_id","subject_type","subject_id","title","rule"],
				"properties":{"subject_type":{"const":"team"}}
			}`),
			TxLineRequirements: []string{"competition winner or completed final fixture"},
		},
		{
			Key:                  "team_reaches_final",
			Scope:                "team",
			Type:                 "binary",
			TitleTemplate:        "{{team}} to reach the World Cup final",
			RuleTemplate:         "Settles YES if {{team}} appear in the official final fixture.",
			ResolutionSource:     "manual_required",
			RuleJSON:             raw(`{"kind":"team_tournament_result","target":"reaches_final","tie_policy":"none"}`),
			RequiredInputsSchema: raw(`{"required":["competition_id","subject_type","subject_id"],"properties":{"subject_type":{"const":"team"}}}`),
			TxLineRequirements:   []string{"competition bracket fixtures"},
		},
		{
			Key:                  "group_winner",
			Scope:                "team",
			Type:                 "binary",
			TitleTemplate:        "{{team}} to win group {{group}}",
			RuleTemplate:         "Settles YES if {{team}} finish first in group {{group}}.",
			ResolutionSource:     "manual_required",
			RuleJSON:             raw(`{"kind":"group_standings","target":"winner","tie_policy":"official_table"}`),
			RequiredInputsSchema: raw(`{"required":["competition_id","subject_type","subject_id","group"],"properties":{"subject_type":{"const":"team"}}}`),
			TxLineRequirements:   []string{"group standings table"},
		},
		{
			Key:                  "golden_boot",
			Scope:                "player",
			Type:                 "binary",
			TitleTemplate:        "{{player}} Golden Boot",
			RuleTemplate:         "Settles YES if {{player}} are official top scorer. Ties follow the creation-time tie policy.",
			ResolutionSource:     "manual_required",
			RuleJSON:             raw(`{"kind":"player_leaderboard","stat":"goals","winner_policy":"most","tie_policy":"dead_heat"}`),
			RequiredInputsSchema: raw(`{"required":["competition_id","subject_type","subject_id","tie_policy"],"properties":{"subject_type":{"const":"player"}}}`),
			TxLineRequirements:   []string{"competition player goals leaderboard", "official tie/dead-heat policy"},
		},
		{
			Key:                  "golden_glove",
			Scope:                "player",
			Type:                 "binary",
			TitleTemplate:        "{{player}} Golden Glove",
			RuleTemplate:         "Settles YES if {{player}} receive the official Golden Glove award.",
			ResolutionSource:     "manual_required",
			RuleJSON:             raw(`{"kind":"official_award","award":"golden_glove","tie_policy":"official_award"}`),
			RequiredInputsSchema: raw(`{"required":["competition_id","subject_type","subject_id"],"properties":{"subject_type":{"const":"player"}}}`),
			TxLineRequirements:   []string{"official awards feed or goalkeeper clean-sheet/stat leaderboard"},
		},
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }
