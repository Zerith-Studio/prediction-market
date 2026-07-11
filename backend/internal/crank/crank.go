// Package crank builds and submits on-chain transactions on behalf of the operator
// (the fee payer — never the user). One settle_match tx per Fill, per
// interface-contract.md §4 "Crank protocol". The operator can only move funds
// according to user-signed Order messages; it cannot forge or over-fill (docs/adr/0003).
package crank

import (
	"context"
	"encoding/hex"
	"log/slog"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
)

// Submitter sends one fill's settle_match transaction to the chain and returns
// the confirmed signature. Implementations: RPCSubmitter (devnet, rpc.go) and
// test fakes. The required tx layout is pinned by TxBuilder (builder.go).
type Submitter interface {
	SettleMatch(ctx context.Context, f matching.Fill) (txSig string, err error)
}

// Hooks receive settlement outcomes. OnConfirmed records the tx signature
// (store.SetFillSettleTx + WS fill event). OnReverted is the
// interface-contract §6.2 revert→reconcile path: unwind the Postgres mirror
// (store.RevertFill), restore the in-memory book (Book.Restore), and re-emit
// order_update so a losing race is a no-op, not a stuck order.
type Hooks struct {
	OnConfirmed func(ctx context.Context, fillID, txSig string)
	OnReverted  func(ctx context.Context, fillID string, f matching.Fill)
}

// Crank drains fills produced by the matching engine and settles them on-chain,
// one tx per fill, sequentially (interface-contract.md §4).
type Crank struct {
	submitter Submitter
	hooks     Hooks
	log       *slog.Logger
}

func New(s Submitter, hooks Hooks, log *slog.Logger) *Crank {
	return &Crank{submitter: s, hooks: hooks, log: log}
}

// SettleOne submits a single fill and dispatches the outcome hooks. A revert
// never propagates as an error — reconciliation IS the handling.
func (c *Crank) SettleOne(ctx context.Context, fillID string, f matching.Fill) {
	txSig, err := c.submitter.SettleMatch(ctx, f)
	if err != nil {
		c.log.Warn("crank: settle_match reverted — reconciling",
			"fill", fillID, "market", hex.EncodeToString(f.MarketID[:4]), "err", err)
		if c.hooks.OnReverted != nil {
			c.hooks.OnReverted(ctx, fillID, f)
		}
		return
	}
	c.log.Info("crank: settle_match confirmed", "fill", fillID, "tx", txSig)
	if c.hooks.OnConfirmed != nil {
		c.hooks.OnConfirmed(ctx, fillID, txSig)
	}
}

// Settle processes fills in order (one tx per fill).
func (c *Crank) Settle(ctx context.Context, fillIDs []string, fills []matching.Fill) {
	for i, f := range fills {
		c.SettleOne(ctx, fillIDs[i], f)
	}
}

// OffchainSubmitter is the not-yet-deployed mode: fills settle in the Postgres
// mirror only, with an empty tx signature — the UI shows no "Verified on
// Solana" link, which is the honest representation. Swap for RPCSubmitter the
// moment the program is live on devnet.
type OffchainSubmitter struct{}

func (OffchainSubmitter) SettleMatch(context.Context, matching.Fill) (string, error) {
	return "", nil
}
