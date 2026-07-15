package crank

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	lookup "github.com/gagliardetto/solana-go/programs/address-lookup-table"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
)

// LUTManager maintains one address lookup table per market so settle_match
// fits on-chain (the 3-ix tx is 1453 B > the 1232 B legacy limit; v0 + table
// brings it well under — progress.md §2). The table holds the market's static
// accounts plus each trading wallet's vault/ATAs; the per-order OrderStatus
// PDAs stay inline (see TxBuilder.LookupAddresses), so once a wallet pair has
// traded a market once, later settles need no table changes at all.
type LUTManager struct {
	client   *rpc.Client
	builder  *TxBuilder
	operator solana.PrivateKey

	mu     sync.Mutex
	tables map[[32]byte]solana.PublicKey // marketID → table address
}

func NewLUTManager(client *rpc.Client, builder *TxBuilder, operator solana.PrivateKey) *LUTManager {
	return &LUTManager{
		client:   client,
		builder:  builder,
		operator: operator,
		tables:   make(map[[32]byte]solana.PublicKey),
	}
}

// EnsureTable returns an ACTIVE lookup table covering the fill's lookup-eligible
// accounts, creating or extending it as needed. Newly written table entries are
// unusable until the slot after the write lands, so any create/extend waits for
// slot advancement before returning.
func (m *LUTManager) EnsureTable(ctx context.Context, f matching.Fill) (solana.PublicKey, solana.PublicKeySlice, error) {
	want, err := m.builder.LookupAddresses(f)
	if err != nil {
		return solana.PublicKey{}, nil, err
	}

	m.mu.Lock()
	tableAddr, exists := m.tables[f.MarketID]
	m.mu.Unlock()

	if !exists {
		addr, err := m.createTable(ctx, want)
		if err != nil {
			return solana.PublicKey{}, nil, err
		}
		m.mu.Lock()
		m.tables[f.MarketID] = addr
		m.mu.Unlock()
		return addr, want, nil
	}

	// Table exists: fetch its current contents and extend with anything missing
	// (e.g. a wallet trading this market for the first time).
	state, err := lookup.GetAddressLookupTable(ctx, m.client, tableAddr)
	if err != nil {
		return solana.PublicKey{}, nil, fmt.Errorf("crank: fetch lookup table %s: %w", tableAddr, err)
	}
	have := make(map[solana.PublicKey]bool, len(state.Addresses))
	for _, a := range state.Addresses {
		have[a] = true
	}
	var missing solana.PublicKeySlice
	for _, a := range want {
		if !have[a] {
			missing = append(missing, a)
		}
	}
	entries := append(solana.PublicKeySlice{}, state.Addresses...)
	if len(missing) > 0 {
		ext := lookup.NewExtendLookupTableInstruction(
			tableAddr, m.operator.PublicKey(), m.operator.PublicKey(), missing)
		if err := m.sendAndWaitNextSlot(ctx, []solana.Instruction{ext.Build()}); err != nil {
			return solana.PublicKey{}, nil, fmt.Errorf("crank: extend lookup table: %w", err)
		}
		entries = append(entries, missing...)
	}
	return tableAddr, entries, nil
}

func (m *LUTManager) createTable(ctx context.Context, addresses solana.PublicKeySlice) (solana.PublicKey, error) {
	slot, err := m.client.GetSlot(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("crank: get slot: %w", err)
	}
	create, tableAddr, err := lookup.NewCreateLookupTableInstruction(
		m.operator.PublicKey(), m.operator.PublicKey(), slot)
	if err != nil {
		return solana.PublicKey{}, err
	}
	extend := lookup.NewExtendLookupTableInstruction(
		tableAddr, m.operator.PublicKey(), m.operator.PublicKey(), addresses)

	if err := m.sendAndWaitNextSlot(ctx, []solana.Instruction{create.Build(), extend.Build()}); err != nil {
		return solana.PublicKey{}, fmt.Errorf("crank: create lookup table: %w", err)
	}
	return tableAddr, nil
}

// sendAndWaitNextSlot submits a small operator-signed legacy tx, confirms it,
// and then waits until the cluster has moved past the confirmation slot —
// the point at which fresh table entries become loadable.
func (m *LUTManager) sendAndWaitNextSlot(ctx context.Context, ixs []solana.Instruction) error {
	recent, err := m.client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	tx, err := solana.NewTransaction(ixs, recent.Value.Blockhash,
		solana.TransactionPayer(m.operator.PublicKey()))
	if err != nil {
		return err
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(m.operator.PublicKey()) {
			return &m.operator
		}
		return nil
	}); err != nil {
		return err
	}
	sig, err := m.client.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return err
	}

	var landedSlot uint64
	deadline := time.Now().Add(120 * time.Second)
	for {
		st, err := m.client.GetSignatureStatuses(ctx, false, sig)
		if err == nil && len(st.Value) > 0 && st.Value[0] != nil {
			v := st.Value[0]
			if v.Err != nil {
				return fmt.Errorf("crank: lookup-table tx %s reverted: %v", sig, v.Err)
			}
			if v.ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
				v.ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				landedSlot = v.Slot
				break
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("crank: lookup-table tx %s not confirmed in time", sig)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}

	for {
		cur, err := m.client.GetSlot(ctx, rpc.CommitmentConfirmed)
		if err == nil && cur > landedSlot {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("crank: slot did not advance past %d", landedSlot)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}
