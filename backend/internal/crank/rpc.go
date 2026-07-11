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
type RPCSubmitter struct {
	Client   *rpc.Client
	Builder  *TxBuilder
	Operator solana.PrivateKey
	// ConfirmTimeout bounds the poll for finalization (default 30s).
	ConfirmTimeout time.Duration
}

func NewRPCSubmitter(rpcURL string, builder *TxBuilder, operator solana.PrivateKey) *RPCSubmitter {
	return &RPCSubmitter{
		Client:         rpc.New(rpcURL),
		Builder:        builder,
		Operator:       operator,
		ConfirmTimeout: 30 * time.Second,
	}
}

func (s *RPCSubmitter) SettleMatch(ctx context.Context, f matching.Fill) (string, error) {
	recent, err := s.Client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("crank: blockhash: %w", err)
	}
	tx, err := s.Builder.BuildSettleMatchTx(f, s.Operator.PublicKey(), recent.Value.Blockhash)
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
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("crank: tx %s not confirmed within %s", sig, timeout)
}
