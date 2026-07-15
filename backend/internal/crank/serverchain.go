package crank

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	ata "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// ChainOps gives the server its on-chain side: market creation at fixture
// registration, tier-a resolution, lazy vault-ATA assurance before settles,
// and the two-step user deposit (operator-cosigned tx where the user's wallet
// signs only the serialized message — no Solana libs needed client-side).
type ChainOps struct {
	Client   *rpc.Client
	Builder  *TxBuilder
	Operator solana.PrivateKey
	Log      *slog.Logger

	mu       sync.Mutex
	atas     map[string]bool            // "owner:mint" → known-exists
	deposits map[string]*pendingDeposit // deposit id → awaiting user signature
}

type pendingDeposit struct {
	tx      *solana.Transaction
	user    solana.PublicKey
	amount  uint64
	created time.Time
}

func NewChainOps(client *rpc.Client, builder *TxBuilder, operator solana.PrivateKey, log *slog.Logger) *ChainOps {
	return &ChainOps{
		Client: client, Builder: builder, Operator: operator, Log: log,
		atas:     make(map[string]bool),
		deposits: make(map[string]*pendingDeposit),
	}
}

// InitializeMarket creates the Market PDA + mints + pool on devnet (idempotent:
// an already-initialized market is detected and treated as success).
// Implements lifecycle.ChainCreator.
func (c *ChainOps) InitializeMarket(ctx context.Context, marketID [32]byte) (string, error) {
	accounts, err := c.Builder.MarketAccounts(marketID)
	if err != nil {
		return "", err
	}
	if info, err := c.Client.GetAccountInfo(ctx, accounts.Market); err == nil && info != nil && info.Value != nil {
		return "", nil // already on-chain
	}
	ix, err := c.Builder.InitializeMarketIx(marketID, 0, c.Operator.PublicKey(), c.Operator.PublicKey())
	if err != nil {
		return "", err
	}
	sig, err := c.sendOperator(ctx, []solana.Instruction{ix})
	if err != nil {
		return "", fmt.Errorf("crank: initialize_market: %w", err)
	}
	c.Log.Info("chain: market initialized", "market", hex.EncodeToString(marketID[:8]), "tx", sig)
	return sig, nil
}

// ResolveMarket submits tier-a resolve_market (0=NO, 1=YES, 2=VOID).
// Implements lifecycle.ChainResolver.
func (c *ChainOps) ResolveMarket(ctx context.Context, marketID [32]byte, outcome uint8) (string, error) {
	ix, err := c.Builder.ResolveMarketIx(marketID, outcome, c.Operator.PublicKey())
	if err != nil {
		return "", err
	}
	sig, err := c.sendOperator(ctx, []solana.Instruction{ix})
	if err != nil {
		return "", fmt.Errorf("crank: resolve_market: %w", err)
	}
	c.Log.Info("chain: market resolved", "market", hex.EncodeToString(marketID[:8]), "outcome", outcome, "tx", sig)
	return sig, nil
}

// EnsureSettleATAs creates (operator-paid) any missing vault token accounts a
// settle needs — settle_match's associated_token constraints require them to
// exist. Cached per (owner, mint) so repeat settles cost nothing.
func (c *ChainOps) EnsureSettleATAs(ctx context.Context, marketID [32]byte, takerMaker, makerMaker [32]byte, takerOutcome, makerOutcome uint8) error {
	m, err := c.Builder.MarketAccounts(marketID)
	if err != nil {
		return err
	}
	mintFor := func(outcome uint8) solana.PublicKey {
		if outcome == models.OutcomeYes {
			return m.YesMint
		}
		return m.NoMint
	}
	var ixs []solana.Instruction
	for _, side := range []struct {
		maker   [32]byte
		outcome uint8
	}{{takerMaker, takerOutcome}, {makerMaker, makerOutcome}} {
		vault, err := c.Builder.VaultPDA(side.maker)
		if err != nil {
			return err
		}
		for _, mint := range []solana.PublicKey{c.Builder.USDCMint, mintFor(side.outcome)} {
			ix, err := c.ensureATAIx(ctx, vault, mint)
			if err != nil {
				return err
			}
			if ix != nil {
				ixs = append(ixs, ix)
			}
		}
	}
	if len(ixs) == 0 {
		return nil
	}
	if _, err := c.sendOperator(ctx, ixs); err != nil {
		return fmt.Errorf("crank: create vault ATAs: %w", err)
	}
	return nil
}

func (c *ChainOps) ensureATAIx(ctx context.Context, owner, mint solana.PublicKey) (solana.Instruction, error) {
	key := owner.String() + ":" + mint.String()
	c.mu.Lock()
	known := c.atas[key]
	c.mu.Unlock()
	if known {
		return nil, nil
	}
	addr, _, err := solana.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return nil, err
	}
	if info, err := c.Client.GetAccountInfo(ctx, addr); err == nil && info != nil && info.Value != nil {
		c.mu.Lock()
		c.atas[key] = true
		c.mu.Unlock()
		return nil, nil
	}
	// CreateIdempotent: a no-op when the ATA already exists, so a transient
	// RPC error on the existence check can never turn into a failed settle.
	c.mu.Lock()
	c.atas[key] = true
	c.mu.Unlock()
	create := ata.NewCreateIdempotentInstructionBuilder().
		SetPayer(c.Operator.PublicKey()).
		SetWallet(owner).
		SetMint(mint)
	return create.Build(), nil
}

// DepositWithKey performs the full deposit for a wallet whose key the server
// holds (the MM bot): SOL top-up, USDC ATA + mint-to, init_vault, deposit —
// operator and user both signed server-side.
func (c *ChainOps) DepositWithKey(ctx context.Context, user solana.PrivateKey, amountMicro uint64) (string, error) {
	id, _, err := c.PrepareDeposit(ctx, user.PublicKey(), amountMicro)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	pd := c.deposits[id]
	c.mu.Unlock()
	if pd == nil {
		return "", fmt.Errorf("crank: pending deposit vanished")
	}
	msg, err := pd.tx.Message.MarshalBinary()
	if err != nil {
		return "", err
	}
	userSig, err := user.Sign(msg)
	if err != nil {
		return "", err
	}
	var sig64 [64]byte
	copy(sig64[:], userSig[:])
	return c.CompleteDeposit(ctx, id, sig64)
}

// --- two-step real deposit -----------------------------------------------------

// PrepareDeposit builds the user's one real signed transaction: fund the
// wallet with SOL for rent+fees, create their USDC ATA, mint demo USDC to it
// (operator is the mint authority), init their vault (if missing), and deposit
// into the vault ATA. The operator co-signs; the USER signs the returned
// serialized message client-side (raw ed25519 over the message bytes — exactly
// what wallet.signMessage does). Returns (depositID, base64 message).
func (c *ChainOps) PrepareDeposit(ctx context.Context, user solana.PublicKey, amountMicro uint64) (string, string, error) {
	var userPK [32]byte
	copy(userPK[:], user.Bytes())
	vault, err := c.Builder.VaultPDA(userPK)
	if err != nil {
		return "", "", err
	}
	userATA, _, err := solana.FindAssociatedTokenAddress(user, c.Builder.USDCMint)
	if err != nil {
		return "", "", err
	}

	var ixs []solana.Instruction
	// Rent + fees so the user can be the init_vault payer (program constraint).
	if bal, err := c.Client.GetBalance(ctx, user, rpc.CommitmentConfirmed); err == nil && bal.Value < 30_000_000 {
		ixs = append(ixs, system.NewTransferInstruction(30_000_000, c.Operator.PublicKey(), user).Build())
	}
	if ix, err := c.ensureATAIx(ctx, user, c.Builder.USDCMint); err == nil && ix != nil {
		ixs = append(ixs, ix)
	}
	ixs = append(ixs, token.NewMintToInstruction(amountMicro, c.Builder.USDCMint, userATA,
		c.Operator.PublicKey(), nil).Build())
	if info, err := c.Client.GetAccountInfo(ctx, vault); err != nil || info == nil || info.Value == nil {
		initIx, err := c.Builder.InitVaultIx(user)
		if err != nil {
			return "", "", err
		}
		ixs = append(ixs, initIx)
	}
	depIx, err := c.Builder.DepositIx(user, amountMicro)
	if err != nil {
		return "", "", err
	}
	ixs = append(ixs, depIx)

	recent, err := c.Client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", "", err
	}
	tx, err := solana.NewTransaction(ixs, recent.Value.Blockhash, solana.TransactionPayer(c.Operator.PublicKey()))
	if err != nil {
		return "", "", err
	}
	msg, err := tx.Message.MarshalBinary()
	if err != nil {
		return "", "", err
	}

	var idBytes [12]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(idBytes[:])
	c.mu.Lock()
	c.deposits[id] = &pendingDeposit{tx: tx, user: user, amount: amountMicro, created: time.Now()}
	for k, d := range c.deposits { // sweep stale
		if time.Since(d.created) > 2*time.Minute {
			delete(c.deposits, k)
		}
	}
	c.mu.Unlock()
	return id, base64.StdEncoding.EncodeToString(msg), nil
}

// CompleteDeposit attaches the user's signature, co-signs as operator, submits
// and confirms. Returns the tx signature.
func (c *ChainOps) CompleteDeposit(ctx context.Context, depositID string, userSig [64]byte) (string, error) {
	c.mu.Lock()
	pd := c.deposits[depositID]
	delete(c.deposits, depositID)
	c.mu.Unlock()
	if pd == nil {
		return "", fmt.Errorf("crank: unknown or expired deposit %s", depositID)
	}

	msg, err := pd.tx.Message.MarshalBinary()
	if err != nil {
		return "", err
	}
	opSig, err := c.Operator.Sign(msg)
	if err != nil {
		return "", err
	}
	// Signature order must match the message's signer list (fee payer first).
	pd.tx.Signatures = nil
	for _, key := range pd.tx.Message.AccountKeys[:pd.tx.Message.Header.NumRequiredSignatures] {
		switch {
		case key.Equals(c.Operator.PublicKey()):
			pd.tx.Signatures = append(pd.tx.Signatures, opSig)
		case key.Equals(pd.user):
			pd.tx.Signatures = append(pd.tx.Signatures, solana.SignatureFromBytes(userSig[:]))
		default:
			return "", fmt.Errorf("crank: unexpected required signer %s", key)
		}
	}
	if err := pd.tx.VerifySignatures(); err != nil {
		return "", fmt.Errorf("crank: deposit signature invalid: %w", err)
	}

	sig, err := c.Client.SendTransactionWithOpts(ctx, pd.tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return "", fmt.Errorf("crank: send deposit: %w", err)
	}
	if err := c.confirm(ctx, sig, 90*time.Second); err != nil {
		return "", err
	}
	c.Log.Info("chain: deposit confirmed", "user", pd.user, "amount", pd.amount, "tx", sig)
	return sig.String(), nil
}

func (c *ChainOps) sendOperator(ctx context.Context, ixs []solana.Instruction) (string, error) {
	recent, err := c.Client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", err
	}
	tx, err := solana.NewTransaction(ixs, recent.Value.Blockhash, solana.TransactionPayer(c.Operator.PublicKey()))
	if err != nil {
		return "", err
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(c.Operator.PublicKey()) {
			return &c.Operator
		}
		return nil
	}); err != nil {
		return "", err
	}
	sig, err := c.Client.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return "", err
	}
	if err := c.confirm(ctx, sig, 90*time.Second); err != nil {
		return "", err
	}
	return sig.String(), nil
}

func (c *ChainOps) confirm(ctx context.Context, sig solana.Signature, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := c.Client.GetSignatureStatuses(ctx, true, sig)
		if err == nil && len(st.Value) > 0 && st.Value[0] != nil {
			if st.Value[0].Err != nil {
				return fmt.Errorf("tx %s reverted: %v", sig, st.Value[0].Err)
			}
			if st.Value[0].ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
				st.Value[0].ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("tx %s not confirmed in time", sig)
}
