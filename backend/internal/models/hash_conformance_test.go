package models

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

// goldenOrder must stay byte-identical to the fixture in
// programs/pitchmarket/src/sig_verify.rs `mod tests` (test_borsh_order_golden_vector).
// If either side's encoder drifts, its golden test fails — this is the guard
// progress.md §5 calls out: a silent drift here fails closed on-chain as
// BadSignature with no useful error.
func goldenOrder() *Order {
	o := &Order{
		Outcome: OutcomeYes,
		Side:    SideBuy,
		Price:   61,
		Size:    1_000_000,
		FeeBps:  0,
		Expiry:  1_700_000_000,
		Salt:    0xDEADBEEF,
	}
	for i := 0; i < 32; i++ {
		o.Maker[i] = byte(i + 1)
		o.MarketID[i] = byte(i + 33)
	}
	return o
}

const (
	goldenBorshHex = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
		"2122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f40" +
		"01003d0040420f0000000000000000f1536500000000efbeadde00000000"
	goldenHashHex = "92d9ef2fb291b8dda3f183d40973d85be3bb73398f2b3d4db9d12511bbccba7e"
)

func TestBorshOrderGoldenVector(t *testing.T) {
	got := hex.EncodeToString(BorshOrder(goldenOrder()))
	if got != goldenBorshHex {
		t.Errorf("borsh encoding drifted from the pinned golden vector\n got: %s\nwant: %s", got, goldenBorshHex)
	}
	if len(got)/2 != 94 {
		t.Errorf("borsh(Order) must be 94 bytes, got %d", len(got)/2)
	}
}

func TestOrderHashGoldenVector(t *testing.T) {
	h := OrderHash(goldenOrder())
	if got := hex.EncodeToString(h[:]); got != goldenHashHex {
		t.Errorf("order hash drifted\n got: %s\nwant: %s", got, goldenHashHex)
	}
}

func TestSignAndVerifyOrder(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := goldenOrder()
	copy(o.Maker[:], pub)

	SignOrder(o, priv)
	if !VerifyOrderSig(o) {
		t.Fatal("freshly signed order must verify")
	}

	o.Price++ // any field mutation invalidates the signature
	if VerifyOrderSig(o) {
		t.Fatal("mutated order must not verify")
	}
}

func TestSignAndVerifyQuote(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	q := &ComboQuote{
		Legs:   []Leg{{Outcome: OutcomeYes}, {Outcome: OutcomeNo}},
		Stake:  5_000_000,
		Payout: 20_000_000,
		Expiry: 1_700_000_000,
		Salt:   7,
	}
	copy(q.Maker[:], pub)
	SignQuote(q, priv)
	if !VerifyQuoteSig(q) {
		t.Fatal("freshly signed quote must verify")
	}
	q.Payout++
	if VerifyQuoteSig(q) {
		t.Fatal("mutated quote must not verify")
	}
}

func TestFee(t *testing.T) {
	// fee = bps × min(p,100−p) × size micro-USDC. 100bps on 1000 shares @ 61¢:
	// min = 39 → 100×39×1000 = 3_900_000 micro = 3.9 USDC.
	if got := Fee(100, 61, 1000); got != 3_900_000 {
		t.Errorf("Fee(100,61,1000) = %d, want 3900000", got)
	}
	if got := Fee(0, 61, 1000); got != 0 {
		t.Errorf("demo fee_bps=0 must be free, got %d", got)
	}
	// symmetric: min(39,61) same either side of 50
	if Fee(100, 39, 1000) != Fee(100, 61, 1000) {
		t.Error("fee must be symmetric around 50")
	}
}

func TestBuyCost(t *testing.T) {
	// 61¢ × 100 shares = 6100¢ = 61 USDC = 61_000_000 micro; fee 0.
	if got := BuyCost(61, 100, 0); got != 61_000_000 {
		t.Errorf("BuyCost(61,100,0) = %d, want 61000000", got)
	}
}
