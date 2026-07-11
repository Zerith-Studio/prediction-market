package models

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
)

// BorshOrder encodes an Order in the same field order and integer widths as the
// Rust struct in programs/pitchmarket/src/state.rs (borsh: fixed ints little-endian,
// fixed byte arrays raw, no length prefix). Sig is excluded — it signs over this encoding.
// Golden vectors pinning this byte-for-byte against sig_verify.rs borsh_order() live in
// hash_conformance_test.go (Go side) and sig_verify.rs tests (Rust side).
func BorshOrder(o *Order) []byte {
	buf := make([]byte, 0, 32+32+1+1+2+8+2+8+8)
	buf = append(buf, o.Maker[:]...)
	buf = append(buf, o.MarketID[:]...)
	buf = append(buf, o.Outcome, o.Side)
	buf = binary.LittleEndian.AppendUint16(buf, o.Price)
	buf = binary.LittleEndian.AppendUint64(buf, o.Size)
	buf = binary.LittleEndian.AppendUint16(buf, o.FeeBps)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(o.Expiry))
	buf = binary.LittleEndian.AppendUint64(buf, o.Salt)
	return buf
}

// OrderHash = sha256(borsh(Order)) per interface-contract.md §1. Primary key everywhere.
func OrderHash(o *Order) [32]byte {
	return sha256.Sum256(BorshOrder(o))
}

// BorshComboQuote mirrors ComboQuoteArgs in programs/pitchmarket/src/lib.rs
// (borsh Vec = u32 LE length prefix + elements).
func BorshComboQuote(q *ComboQuote) []byte {
	buf := make([]byte, 0, 32+4+len(q.Legs)*33+8+8+8+8)
	buf = append(buf, q.Maker[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(q.Legs)))
	for _, leg := range q.Legs {
		buf = append(buf, leg.MarketID[:]...)
		buf = append(buf, leg.Outcome)
	}
	buf = binary.LittleEndian.AppendUint64(buf, q.Stake)
	buf = binary.LittleEndian.AppendUint64(buf, q.Payout)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(q.Expiry))
	buf = binary.LittleEndian.AppendUint64(buf, q.Salt)
	return buf
}

// QuoteHash = sha256(borsh(ComboQuote)) per interface-contract.md §2.
func QuoteHash(q *ComboQuote) [32]byte {
	return sha256.Sum256(BorshComboQuote(q))
}

// VerifyOrderSig checks the order's ed25519 signature over BorshOrder at E2 entry
// (defense in depth — E1 re-verifies on-chain regardless, docs/adr/0003).
func VerifyOrderSig(o *Order) bool {
	return ed25519.Verify(o.Maker[:], BorshOrder(o), o.Sig[:])
}

// VerifyQuoteSig checks the combo quote's ed25519 signature over BorshComboQuote.
func VerifyQuoteSig(q *ComboQuote) bool {
	return ed25519.Verify(q.Maker[:], BorshComboQuote(q), q.Sig[:])
}

// SignOrder fills o.Sig with priv's signature over BorshOrder. Test/bot helper —
// real users sign client-side (Privy embedded wallet).
func SignOrder(o *Order, priv ed25519.PrivateKey) {
	copy(o.Sig[:], ed25519.Sign(priv, BorshOrder(o)))
}

// SignQuote fills q.Sig with priv's signature over BorshComboQuote.
func SignQuote(q *ComboQuote, priv ed25519.PrivateKey) {
	copy(q.Sig[:], ed25519.Sign(priv, BorshComboQuote(q)))
}

// Fee = fee_bps × min(p, 1−p) × size in micro-USDC, charged in the output asset
// (core-features-spec M1). fee_bps is zero for the demo, so this is usually 0.
func Fee(feeBps uint16, price uint16, size uint64) uint64 {
	p := uint64(price)
	if 100-p < p {
		p = 100 - p
	}
	// price is cents: p/100. fee = bps/10000 × p/100 × size shares × 1e6 micro/share
	// = bps × p × size × 1e6 / 1e6 = bps × p × size (exact, no rounding loss).
	return uint64(feeBps) * p * size
}

// BuyCost is the entry-time USDC soft-lock for a BUY: price×size + fee, micro-USDC
// (interface-contract.md §1).
func BuyCost(price uint16, size uint64, feeBps uint16) uint64 {
	return uint64(price)*size*MicroPerCent + Fee(feeBps, price, size)
}

// MicroPerCent converts price-in-cents × shares into micro-USDC
// (mirrors MICRO_PER_CENT in programs/pitchmarket/src/lib.rs).
const MicroPerCent uint64 = 10_000
