// Package feed abstracts the source of live match data behind FeedProvider so the
// rest of the backend (auto market creation, pricing, resolution) is agnostic to
// whether data comes from TxLINE live or a recorded replay (PROJECT_PLAN.md §9 safety net).
package feed

import "context"

type EventType string

const (
	EventScore    EventType = "score"
	EventKickoff  EventType = "kickoff"
	EventFullTime EventType = "full_time"
	EventOdds     EventType = "odds"
)

// MatchEvent is the normalized shape both txodds and replay providers emit.
type MatchEvent struct {
	FixtureID string    `json:"fixture_id"`
	Type      EventType `json:"type"`
	Payload   any       `json:"payload"`
	// SignedProof carries the TxLINE cryptographic signature / Merkle proof for this
	// event when available (docs/adr/0005 oracle tier d). Empty for replay/simulated data.
	SignedProof []byte `json:"signed_proof,omitempty"`
}

// FeedProvider streams normalized match events for a fixture. Implementations:
// txodds (live SSE stream, TODO pending docs/txodds-day1-email.md access) and
// replay (recorded fixtures, always available, demo safety net).
type FeedProvider interface {
	Stream(ctx context.Context, fixtureID string) (<-chan MatchEvent, error)
}
