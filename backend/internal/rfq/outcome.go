package rfq

import "encoding/json"

// parseResult extracts "result" from a market's outcome JSONB.
func parseResult(outcomeJSON []byte) string {
	var v struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(outcomeJSON, &v); err != nil {
		return ""
	}
	return v.Result
}
