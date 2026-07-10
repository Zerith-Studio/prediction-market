package models

import (
	"crypto/sha256"
	"encoding/binary"
)

// borshOrder encodes an Order in the same field order and integer widths as the
// Rust struct in programs/pitchmarket/src/state.rs (borsh: fixed ints little-endian,
// fixed byte arrays raw, no length prefix). Sig is excluded — it signs over this encoding.
//
// TODO(E1/E2 joint): once the Anchor program is deployed, cross-check this against a
// borsh(Order) golden vector produced on-chain. Hand-rolled to avoid a borsh dependency
// during scaffolding; the two encoders must never drift.
func borshOrder(o *Order) []byte {
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
	return sha256.Sum256(borshOrder(o))
}

func borshComboQuote(q *ComboQuote) []byte {
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
	return sha256.Sum256(borshComboQuote(q))
}
