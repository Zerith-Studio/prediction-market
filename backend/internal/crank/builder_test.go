package crank

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"log/slog"
	"testing"

	"github.com/gagliardetto/solana-go"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

func signedOrder(t *testing.T, outcome, side uint8, price uint16, size uint64, salt uint64) (*models.Order, [32]byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := &models.Order{Outcome: outcome, Side: side, Price: price, Size: size, Salt: salt}
	copy(o.Maker[:], pub)
	o.MarketID[0] = 0xAB
	models.SignOrder(o, priv)
	return o, models.OrderHash(o)
}

func testFill(t *testing.T) matching.Fill {
	t.Helper()
	maker, makerHash := signedOrder(t, models.OutcomeYes, models.SideSell, 60, 50, 1)
	taker, takerHash := signedOrder(t, models.OutcomeYes, models.SideBuy, 65, 30, 2)
	return matching.Fill{
		MarketID:  taker.MarketID,
		Taker:     &matching.RestingOrder{Order: taker, Hash: takerHash, Remaining: 0},
		Maker:     &matching.RestingOrder{Order: maker, Hash: makerHash, Remaining: 20},
		Price:     60,
		Size:      30,
		MatchType: models.MatchNormal,
	}
}

func testBuilder() *TxBuilder {
	return &TxBuilder{
		ProgramID: solana.MustPublicKeyFromBase58("3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs"),
		USDCMint:  solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"), // devnet USDC
	}
}

// verifyLikeChain re-implements sig_verify.rs verify_order_signature byte
// checks in Go and runs them against a built instruction: if this passes and
// the Rust golden tests pass, the crank's layout is what the chain will accept.
func verifyLikeChain(t *testing.T, ix solana.Instruction, o *models.Order) {
	t.Helper()
	if !ix.ProgramID().Equals(Ed25519ProgramID) {
		t.Fatalf("ix program = %s, want Ed25519 precompile", ix.ProgramID())
	}
	data, err := ix.Data()
	if err != nil {
		t.Fatal(err)
	}
	msg := models.BorshOrder(o)
	if wantLen := 16 + 64 + 32 + len(msg); len(data) != wantLen {
		t.Fatalf("data len = %d, want %d", len(data), wantLen)
	}
	if data[0] != 1 {
		t.Fatalf("num_signatures = %d, want 1", data[0])
	}
	sigOff := binary.LittleEndian.Uint16(data[2:4])
	sigIx := binary.LittleEndian.Uint16(data[4:6])
	pkOff := binary.LittleEndian.Uint16(data[6:8])
	pkIx := binary.LittleEndian.Uint16(data[8:10])
	msgOff := binary.LittleEndian.Uint16(data[10:12])
	msgLen := binary.LittleEndian.Uint16(data[12:14])
	msgIx := binary.LittleEndian.Uint16(data[14:16])
	if sigIx != 0xFFFF || pkIx != 0xFFFF || msgIx != 0xFFFF {
		t.Fatalf("instruction indices must be self (0xFFFF): %d %d %d", sigIx, pkIx, msgIx)
	}
	gotSig := data[sigOff : sigOff+64]
	gotPk := data[pkOff : pkOff+32]
	gotMsg := data[msgOff : msgOff+uint16(len(msg))]
	if int(msgLen) != len(msg) {
		t.Fatalf("msg len field = %d, want %d", msgLen, len(msg))
	}
	if !bytes.Equal(gotSig, o.Sig[:]) {
		t.Fatal("embedded signature != order sig")
	}
	if !bytes.Equal(gotPk, o.Maker[:]) {
		t.Fatal("embedded pubkey != order maker")
	}
	if !bytes.Equal(gotMsg, msg) {
		t.Fatal("embedded message != borsh(order)")
	}
	// And the precompile itself would accept it:
	if !ed25519.Verify(gotPk, gotMsg, gotSig) {
		t.Fatal("signature does not verify — precompile would abort the tx")
	}
}

func TestSettleMatchInstructionLayout(t *testing.T) {
	f := testFill(t)
	operator := solana.NewWallet().PublicKey()

	ixs, err := testBuilder().SettleMatchInstructions(f, operator)
	if err != nil {
		t.Fatal(err)
	}
	if len(ixs) != 3 {
		t.Fatalf("want exactly 3 instructions (§6.5), got %d", len(ixs))
	}

	// ix[0] verifies the TAKER, ix[1] the MAKER — order is pinned; swapping
	// them fails on-chain because settle_match reads them by index.
	verifyLikeChain(t, ixs[0], f.Taker.Order)
	verifyLikeChain(t, ixs[1], f.Maker.Order)

	// ix[2]: settle_match with anchor discriminator + args.
	data, err := ixs[2].Data()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[:8], anchorDiscriminator("settle_match")) {
		t.Fatalf("bad discriminator %x", data[:8])
	}
	want := anchorDiscriminator("settle_match")
	want = append(want, models.BorshOrder(f.Taker.Order)...)
	want = append(want, f.Taker.Order.Sig[:]...)
	want = append(want, models.BorshOrder(f.Maker.Order)...)
	want = append(want, f.Maker.Order.Sig[:]...)
	want = append(want, byte(f.MatchType))
	want = binary.LittleEndian.AppendUint16(want, f.Price)
	want = binary.LittleEndian.AppendUint64(want, f.Size)
	if !bytes.Equal(data, want) {
		t.Fatal("settle_match instruction data drifted from the anchor arg encoding")
	}

	if !ixs[2].ProgramID().Equals(testBuilder().ProgramID) {
		t.Fatal("settle_match must target the pitchmarket program")
	}
	accounts := ixs[2].Accounts()
	if len(accounts) != 16 {
		t.Fatalf("SettleMatch context has 16 accounts, got %d", len(accounts))
	}
	// Spot-check the pinned positions that would fail silently if shuffled.
	if !accounts[12].PublicKey.Equals(operator) || !accounts[12].IsSigner {
		t.Errorf("accounts[12] must be the operator signer: %+v", accounts[12])
	}
	if !accounts[13].PublicKey.Equals(solana.SysVarInstructionsPubkey) {
		t.Errorf("accounts[13] must be the instructions sysvar")
	}
	if !accounts[15].PublicKey.Equals(solana.SystemProgramID) {
		t.Errorf("accounts[15] must be the system program")
	}
}

func TestBuildSettleMatchTxSignsAndCompiles(t *testing.T) {
	f := testFill(t)
	operator := solana.NewWallet()

	tx, err := testBuilder().BuildSettleMatchTx(f, operator.PublicKey(), solana.Hash{1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(operator.PublicKey()) {
			return &operator.PrivateKey
		}
		return nil
	}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	// The compiled message must be the exact 3-ix program order.
	msg := tx.Message
	if len(msg.Instructions) != 3 {
		t.Fatalf("compiled ix count = %d", len(msg.Instructions))
	}
	prog0, _ := msg.Program(msg.Instructions[0].ProgramIDIndex)
	prog2, _ := msg.Program(msg.Instructions[2].ProgramIDIndex)
	if !prog0.Equals(Ed25519ProgramID) {
		t.Error("compiled ix[0] must be the ed25519 precompile")
	}
	if !prog2.Equals(testBuilder().ProgramID) {
		t.Error("compiled ix[2] must be pitchmarket")
	}
	// Exactly one signature: the operator. Users never sign the settle tx.
	if len(tx.Signatures) != 1 {
		t.Errorf("want 1 signature (operator only), got %d", len(tx.Signatures))
	}
}

type fakeSubmitter struct {
	fail bool
	got  []matching.Fill
}

func (s *fakeSubmitter) SettleMatch(_ context.Context, f matching.Fill) (string, error) {
	s.got = append(s.got, f)
	if s.fail {
		return "", errors.New("custom program error: 0x1770 (BadSignature)")
	}
	return "tx-sig-ok", nil
}

func TestCrankHooks(t *testing.T) {
	f := testFill(t)

	var confirmed, reverted []string
	hooks := Hooks{
		OnConfirmed: func(_ context.Context, fillID, txSig string) {
			confirmed = append(confirmed, fillID+":"+txSig)
		},
		OnReverted: func(_ context.Context, fillID string, _ matching.Fill) {
			reverted = append(reverted, fillID)
		},
	}

	ok := New(&fakeSubmitter{}, hooks, slog.Default())
	ok.SettleOne(context.Background(), "fill-1", f)
	if len(confirmed) != 1 || confirmed[0] != "fill-1:tx-sig-ok" || len(reverted) != 0 {
		t.Fatalf("confirmed=%v reverted=%v", confirmed, reverted)
	}

	confirmed, reverted = nil, nil
	bad := New(&fakeSubmitter{fail: true}, hooks, slog.Default())
	bad.SettleOne(context.Background(), "fill-2", f)
	if len(reverted) != 1 || reverted[0] != "fill-2" || len(confirmed) != 0 {
		t.Fatalf("revert path: confirmed=%v reverted=%v", confirmed, reverted)
	}
}
