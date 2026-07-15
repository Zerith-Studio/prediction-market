package crank

import (
	"context"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
)

// RPCSubmitter builds, signs (operator key), sends, and confirms settle_match
// transactions against a Solana RPC endpoint (devnet for the hackathon).
// Transactions are v0 with a per-market address lookup table — the 3-ix settle
// tx exceeds the legacy size limit (progress.md §2 finding).
type RPCSubmitter struct {
	Client   *rpc.Client
	Builder  *TxBuilder
	Operator solana.PrivateKey
	Tables   *LUTManager
	// ConfirmTimeout bounds the poll for finalization (default 30s).
	ConfirmTimeout time.Duration
}

func NewRPCSubmitter(rpcURL string, builder *TxBuilder, operator solana.PrivateKey) *RPCSubmitter {
	client := rpc.New(rpcURL)
	return &RPCSubmitter{
		Client:         client,
		Builder:        builder,
		Operator:       operator,
		Tables:         NewLUTManager(client, builder, operator),
		ConfirmTimeout: 30 * time.Second,
	}
}

func (s *RPCSubmitter) SettleMatch(ctx context.Context, f matching.Fill) (string, error) {
	tableAddr, entries, err := s.Tables.EnsureTable(ctx, f)
	if err != nil {
		return "", fmt.Errorf("crank: lookup table: %w", err)
	}
	recent, err := s.Client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("crank: blockhash: %w", err)
	}
	tx, err := s.Builder.BuildSettleMatchTxV0(f, s.Operator.PublicKey(),
		recent.Value.Blockhash, tableAddr, entries)
	if err != nil {
		return "", err
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(s.Operator.PublicKey()) {
			return &s.Operator
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("crank: sign: %w", err)
	}

	sig, err := s.Client.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return "", fmt.Errorf("crank: send: %w", err)
	}

	timeout := s.ConfirmTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := s.Client.GetSignatureStatuses(ctx, false, sig)
		if err == nil && len(st.Value) > 0 && st.Value[0] != nil {
			v := st.Value[0]
			if v.Err != nil {
				return "", fmt.Errorf("crank: tx %s reverted: %v", sig, v.Err)
			}
			if v.ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
				v.ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				return sig.String(), nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("crank: tx %s not confirmed within %s", sig, timeout)
}
