package crank

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// TxBuilder derives every account settle_match needs and assembles the exact
// 3-instruction transaction pinned in interface-contract.md §6.5:
//
//	ix[0] = Ed25519 verify (taker sig over borsh(taker))
//	ix[1] = Ed25519 verify (maker sig over borsh(maker))
//	ix[2] = settle_match
//
// Wrong order or omitted Ed25519 instructions fail closed on-chain with
// BadSignature (sig_verify.rs reads ix 0 and 1 via the instructions sysvar).
type TxBuilder struct {
	ProgramID solana.PublicKey
	USDCMint  solana.PublicKey
}

// anchorDiscriminator is Anchor's 8-byte global instruction dispatch tag.
func anchorDiscriminator(name string) []byte {
	h := sha256.Sum256([]byte("global:" + name))
	return h[:8]
}

func pda(programID solana.PublicKey, seeds ...[]byte) (solana.PublicKey, error) {
	addr, _, err := solana.FindProgramAddress(seeds, programID)
	return addr, err
}

// settleMatchData encodes the instruction args exactly as Anchor deserializes
// them: disc ‖ borsh(taker) ‖ taker_sig ‖ borsh(maker) ‖ maker_sig ‖
// match_type u8 ‖ fill_price u16 LE ‖ fill_size u64 LE.
func settleMatchData(f matching.Fill) []byte {
	taker, maker := f.Taker.Order, f.Maker.Order
	data := anchorDiscriminator("settle_match")
	data = append(data, models.BorshOrder(taker)...)
	data = append(data, taker.Sig[:]...)
	data = append(data, models.BorshOrder(maker)...)
	data = append(data, maker.Sig[:]...)
	data = append(data, byte(f.MatchType))
	data = binary.LittleEndian.AppendUint16(data, f.Price)
	data = binary.LittleEndian.AppendUint64(data, f.Size)
	return data
}

func (b *TxBuilder) outcomeMint(marketID [32]byte, outcome uint8) (solana.PublicKey, error) {
	seed := []byte("no")
	if outcome == models.OutcomeYes {
		seed = []byte("yes")
	}
	return pda(b.ProgramID, seed, marketID[:])
}

// SettleMatchInstructions builds the pinned [ed25519, ed25519, settle_match]
// triple for one fill. Pure — no RPC, fully unit-testable offline.
func (b *TxBuilder) SettleMatchInstructions(f matching.Fill, operator solana.PublicKey) ([]solana.Instruction, error) {
	taker, maker := f.Taker.Order, f.Maker.Order
	marketID := f.MarketID

	market, err := pda(b.ProgramID, []byte("market"), marketID[:])
	if err != nil {
		return nil, fmt.Errorf("crank: market pda: %w", err)
	}
	takerMint, err := b.outcomeMint(marketID, taker.Outcome)
	if err != nil {
		return nil, err
	}
	makerMint, err := b.outcomeMint(marketID, maker.Outcome)
	if err != nil {
		return nil, err
	}
	pool, err := pda(b.ProgramID, []byte("pool"), marketID[:])
	if err != nil {
		return nil, err
	}
	takerStatus, err := pda(b.ProgramID, []byte("ostatus"), f.Taker.Hash[:])
	if err != nil {
		return nil, err
	}
	makerStatus, err := pda(b.ProgramID, []byte("ostatus"), f.Maker.Hash[:])
	if err != nil {
		return nil, err
	}
	takerVault, err := pda(b.ProgramID, []byte("vault"), taker.Maker[:])
	if err != nil {
		return nil, err
	}
	makerVault, err := pda(b.ProgramID, []byte("vault"), maker.Maker[:])
	if err != nil {
		return nil, err
	}
	takerUSDC, _, err := solana.FindAssociatedTokenAddress(takerVault, b.USDCMint)
	if err != nil {
		return nil, err
	}
	makerUSDC, _, err := solana.FindAssociatedTokenAddress(makerVault, b.USDCMint)
	if err != nil {
		return nil, err
	}
	takerOutcomeATA, _, err := solana.FindAssociatedTokenAddress(takerVault, takerMint)
	if err != nil {
		return nil, err
	}
	makerOutcomeATA, _, err := solana.FindAssociatedTokenAddress(makerVault, makerMint)
	if err != nil {
		return nil, err
	}

	// Account order mirrors the SettleMatch context in
	// programs/pitchmarket/src/lib.rs — Anchor resolves by position.
	accounts := solana.AccountMetaSlice{
		solana.Meta(market),
		solana.Meta(takerMint).WRITE(),
		solana.Meta(makerMint).WRITE(),
		solana.Meta(pool).WRITE(),
		solana.Meta(takerStatus).WRITE(),
		solana.Meta(makerStatus).WRITE(),
		solana.Meta(takerVault),
		solana.Meta(makerVault),
		solana.Meta(takerUSDC).WRITE(),
		solana.Meta(makerUSDC).WRITE(),
		solana.Meta(takerOutcomeATA).WRITE(),
		solana.Meta(makerOutcomeATA).WRITE(),
		solana.Meta(operator).WRITE().SIGNER(),
		solana.Meta(solana.SysVarInstructionsPubkey),
		solana.Meta(solana.TokenProgramID),
		solana.Meta(solana.SystemProgramID),
	}

	return []solana.Instruction{
		NewEd25519Instruction(taker.Maker, models.BorshOrder(taker), taker.Sig),
		NewEd25519Instruction(maker.Maker, models.BorshOrder(maker), maker.Sig),
		solana.NewInstruction(b.ProgramID, accounts, settleMatchData(f)),
	}, nil
}

// BuildSettleMatchTx assembles the unsigned LEGACY transaction (operator = fee
// payer). ⚠️ The full settle tx is 1453 bytes — OVER the 1232-byte legacy limit
// (progress.md §2 finding) — so this only serializes for tests; on-chain
// submission must go through BuildSettleMatchTxV0.
func (b *TxBuilder) BuildSettleMatchTx(f matching.Fill, operator solana.PublicKey, blockhash solana.Hash) (*solana.Transaction, error) {
	ixs, err := b.SettleMatchInstructions(f, operator)
	if err != nil {
		return nil, err
	}
	return solana.NewTransaction(ixs, blockhash, solana.TransactionPayer(operator))
}

// LookupAddresses returns the settle accounts eligible for the market's address
// lookup table: everything except the operator (fee payer/signer must stay a
// static key) and the two per-order OrderStatus PDAs (unique per order — putting
// them in the table would force an extend + activation wait on EVERY settle;
// keeping them static costs 64 bytes and keeps the table contents stable per
// (market, wallet) pair). Top-level program ids (ed25519, pitchmarket) are
// omitted too — the runtime forbids invoked programs from tables.
func (b *TxBuilder) LookupAddresses(f matching.Fill) (solana.PublicKeySlice, error) {
	// NB: the operator placeholder below must NOT be the zero key — that IS the
	// system program's address, which legitimately belongs in the table. The
	// operator is excluded via the IsSigner check instead.
	placeholder := solana.MustPublicKeyFromBase58("Sysvar1111111111111111111111111111111111111")
	ixs, err := b.SettleMatchInstructions(f, placeholder)
	if err != nil {
		return nil, err
	}
	skip := map[solana.PublicKey]bool{}
	takerStatus, err := pda(b.ProgramID, []byte("ostatus"), f.Taker.Hash[:])
	if err != nil {
		return nil, err
	}
	makerStatus, err := pda(b.ProgramID, []byte("ostatus"), f.Maker.Hash[:])
	if err != nil {
		return nil, err
	}
	skip[takerStatus] = true
	skip[makerStatus] = true

	var out solana.PublicKeySlice
	seen := map[solana.PublicKey]bool{}
	for _, meta := range ixs[2].Accounts() {
		k := meta.PublicKey
		if skip[k] || seen[k] || meta.IsSigner {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, nil
}

// BuildSettleMatchTxV0 assembles the v0 transaction against the market's
// lookup table (tableAddr → its current address list). This is the ONLY form
// that fits on-chain; interface-contract §6.5's 3-instruction order is
// unchanged, just compiled with lookups.
func (b *TxBuilder) BuildSettleMatchTxV0(f matching.Fill, operator solana.PublicKey,
	blockhash solana.Hash, tableAddr solana.PublicKey, tableEntries solana.PublicKeySlice) (*solana.Transaction, error) {
	ixs, err := b.SettleMatchInstructions(f, operator)
	if err != nil {
		return nil, err
	}
	return solana.NewTransaction(ixs, blockhash,
		solana.TransactionPayer(operator),
		solana.TransactionAddressTables(map[solana.PublicKey]solana.PublicKeySlice{
			tableAddr: tableEntries,
		}),
	)
}
