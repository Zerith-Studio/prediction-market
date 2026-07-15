package crank

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// Builders for the non-settle program instructions the operator (or a test
// harness) drives: market creation, vault setup, resolution, redemption.
// Account order mirrors each #[derive(Accounts)] context in
// programs/pitchmarket/src/lib.rs — Anchor resolves by position.

// MarketAccounts are the derived PDAs for one market.
type MarketAccounts struct {
	Market   solana.PublicKey
	YesMint  solana.PublicKey
	NoMint   solana.PublicKey
	PoolUSDC solana.PublicKey
}

func (b *TxBuilder) MarketAccounts(marketID [32]byte) (MarketAccounts, error) {
	var m MarketAccounts
	var err error
	if m.Market, err = pda(b.ProgramID, []byte("market"), marketID[:]); err != nil {
		return m, err
	}
	if m.YesMint, err = pda(b.ProgramID, []byte("yes"), marketID[:]); err != nil {
		return m, err
	}
	if m.NoMint, err = pda(b.ProgramID, []byte("no"), marketID[:]); err != nil {
		return m, err
	}
	m.PoolUSDC, err = pda(b.ProgramID, []byte("pool"), marketID[:])
	return m, err
}

func (b *TxBuilder) VaultPDA(owner [32]byte) (solana.PublicKey, error) {
	return pda(b.ProgramID, []byte("vault"), owner[:])
}

// InitializeMarketIx creates the Market PDA + outcome mints + collateral pool.
func (b *TxBuilder) InitializeMarketIx(marketID [32]byte, oracleTier uint8,
	resolver, operator solana.PublicKey) (solana.Instruction, error) {
	m, err := b.MarketAccounts(marketID)
	if err != nil {
		return nil, err
	}
	data := anchorDiscriminator("initialize_market")
	data = append(data, marketID[:]...)
	data = append(data, oracleTier)
	data = append(data, resolver[:]...)

	return solana.NewInstruction(b.ProgramID, solana.AccountMetaSlice{
		solana.Meta(m.Market).WRITE(),
		solana.Meta(m.YesMint).WRITE(),
		solana.Meta(m.NoMint).WRITE(),
		solana.Meta(m.PoolUSDC).WRITE(),
		solana.Meta(b.USDCMint),
		solana.Meta(operator).WRITE().SIGNER(),
		solana.Meta(solana.TokenProgramID),
		solana.Meta(solana.SystemProgramID),
		solana.Meta(solana.SysVarRentPubkey),
	}, data), nil
}

// InitVaultIx opens a user's custody PDA (user signs and pays).
func (b *TxBuilder) InitVaultIx(user solana.PublicKey) (solana.Instruction, error) {
	vault, err := b.VaultPDA(user)
	if err != nil {
		return nil, err
	}
	return solana.NewInstruction(b.ProgramID, solana.AccountMetaSlice{
		solana.Meta(vault).WRITE(),
		solana.Meta(user).WRITE().SIGNER(),
		solana.Meta(solana.SystemProgramID),
	}, anchorDiscriminator("init_vault")), nil
}

// DepositIx moves USDC from the user's wallet ATA into the vault-owned ATA
// (the one live-signed tx a user ever makes — the Privy popup).
func (b *TxBuilder) DepositIx(user solana.PublicKey, amount uint64) (solana.Instruction, error) {
	vault, err := b.VaultPDA(user)
	if err != nil {
		return nil, err
	}
	userATA, _, err := solana.FindAssociatedTokenAddress(user, b.USDCMint)
	if err != nil {
		return nil, err
	}
	vaultATA, _, err := solana.FindAssociatedTokenAddress(vault, b.USDCMint)
	if err != nil {
		return nil, err
	}
	data := anchorDiscriminator("deposit")
	data = binary.LittleEndian.AppendUint64(data, amount)

	return solana.NewInstruction(b.ProgramID, solana.AccountMetaSlice{
		solana.Meta(vault),
		solana.Meta(user), // `owner` (has_one check)
		solana.Meta(userATA).WRITE(),
		solana.Meta(vaultATA).WRITE(),
		solana.Meta(b.USDCMint),
		solana.Meta(user).WRITE().SIGNER(),
		solana.Meta(solana.TokenProgramID),
		solana.Meta(solana.SPLAssociatedTokenAccountProgramID),
		solana.Meta(solana.SystemProgramID),
	}, data), nil
}

// ResolveMarketIx sets the outcome (tier-a: resolver key signs).
func (b *TxBuilder) ResolveMarketIx(marketID [32]byte, outcome uint8, resolver solana.PublicKey) (solana.Instruction, error) {
	m, err := b.MarketAccounts(marketID)
	if err != nil {
		return nil, err
	}
	data := anchorDiscriminator("resolve_market")
	data = append(data, outcome)
	return solana.NewInstruction(b.ProgramID, solana.AccountMetaSlice{
		solana.Meta(m.Market).WRITE(),
		solana.Meta(resolver).SIGNER(),
	}, data), nil
}

// RedeemIx burns `amount` winning shares from the user's vault-owned outcome
// ATA and pays USDC 1:1 from the pool to the user's own wallet ATA.
func (b *TxBuilder) RedeemIx(marketID [32]byte, user solana.PublicKey, outcome uint8, amount uint64) (solana.Instruction, error) {
	m, err := b.MarketAccounts(marketID)
	if err != nil {
		return nil, err
	}
	vault, err := b.VaultPDA(user)
	if err != nil {
		return nil, err
	}
	outcomeMint := m.NoMint
	if outcome == models.OutcomeYes {
		outcomeMint = m.YesMint
	}
	vaultOutcomeATA, _, err := solana.FindAssociatedTokenAddress(vault, outcomeMint)
	if err != nil {
		return nil, err
	}
	userUSDCATA, _, err := solana.FindAssociatedTokenAddress(user, b.USDCMint)
	if err != nil {
		return nil, err
	}
	data := anchorDiscriminator("redeem")
	data = append(data, outcome)
	data = binary.LittleEndian.AppendUint64(data, amount)

	return solana.NewInstruction(b.ProgramID, solana.AccountMetaSlice{
		solana.Meta(m.Market),
		solana.Meta(vault),
		solana.Meta(outcomeMint).WRITE(),
		solana.Meta(vaultOutcomeATA).WRITE(),
		solana.Meta(m.PoolUSDC).WRITE(),
		solana.Meta(userUSDCATA).WRITE(),
		solana.Meta(user).WRITE().SIGNER(),
		solana.Meta(solana.TokenProgramID),
	}, data), nil
}
