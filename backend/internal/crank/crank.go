// Package crank builds and submits on-chain transactions on behalf of the operator
// (the fee payer — never the user). One settle_match tx per Fill batch, per
// interface-contract.md §4 "Crank protocol". The operator can only move funds
// according to user-signed Order messages; it cannot forge or over-fill (docs/adr/0003).
package crank

import (
	"context"
	"fmt"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
)

// Submitter sends a built settle_match instruction to the chain and returns the tx
// signature once confirmed. TODO: implement against github.com/gagliardetto/solana-go
// once the Anchor program is deployed to devnet and its IDL is available — this
// interface is the seam so `matching` and `api` can be built/tested without it.
//
// Required transaction layout (programs/pitchmarket/src/lib.rs settle_match doc
// comment is the canonical source): the built tx MUST contain, in order,
//   ix[0] = Ed25519Program.createInstructionWithPublicKey(taker.maker, borsh(taker), taker_sig)
//   ix[1] = Ed25519Program.createInstructionWithPublicKey(maker.maker, borsh(maker), maker_sig)
//   ix[2] = the settle_match instruction itself
// settle_match reads ix[0]/ix[1] via the instructions sysvar to verify both
// signatures on-chain — omitting or reordering these makes settlement always
// fail with BadSignature. borsh(order) must match models.OrderHash's encoding
// (internal/models/hash.go) byte-for-byte.
type Submitter interface {
	SettleMatch(ctx context.Context, fills []matching.Fill) (txSig string, err error)
}

// Crank drains fills produced by the matching engine and settles them on-chain,
// one tx per match, sequentially (interface-contract.md §4).
type Crank struct {
	submitter Submitter
}

func New(s Submitter) *Crank {
	return &Crank{submitter: s}
}

// Settle submits one settle_match per Fill. TODO(revert→reconcile, interface-contract
// §6.2): on a tx revert, the caller must unwind the matching engine's soft-lock and
// re-emit order_update over WS so a losing race is a no-op, not a stuck order.
func (c *Crank) Settle(ctx context.Context, fills []matching.Fill) error {
	for _, f := range fills {
		if _, err := c.submitter.SettleMatch(ctx, []matching.Fill{f}); err != nil {
			return fmt.Errorf("crank: settle_match failed for market %x: %w", f.MarketID, err)
		}
	}
	return nil
}
