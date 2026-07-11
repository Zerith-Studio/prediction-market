package models

import (
	"encoding/hex"
	"fmt"

	"github.com/mr-tron/base58"
)

// PubkeyString renders a 32-byte ed25519 pubkey the way the rest of the Solana
// world does (base58). Used for Postgres TEXT columns and JSON payloads.
func PubkeyString(pk [32]byte) string {
	return base58.Encode(pk[:])
}

// ParsePubkey parses a base58 Solana pubkey into its raw 32 bytes.
func ParsePubkey(s string) ([32]byte, error) {
	var pk [32]byte
	raw, err := base58.Decode(s)
	if err != nil {
		return pk, fmt.Errorf("bad base58 pubkey: %w", err)
	}
	if len(raw) != 32 {
		return pk, fmt.Errorf("pubkey must be 32 bytes, got %d", len(raw))
	}
	copy(pk[:], raw)
	return pk, nil
}

// HashString renders order/quote/market hashes as lowercase hex (64 chars) —
// the canonical string form in URLs, JSON, and logs.
func HashString(h [32]byte) string {
	return hex.EncodeToString(h[:])
}

// ParseHash parses a 64-char hex string into a 32-byte hash.
func ParseHash(s string) ([32]byte, error) {
	var h [32]byte
	raw, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("bad hex hash: %w", err)
	}
	if len(raw) != 32 {
		return h, fmt.Errorf("hash must be 32 bytes, got %d", len(raw))
	}
	copy(h[:], raw)
	return h, nil
}

// ParseSig parses a 128-char hex ed25519 signature.
func ParseSig(s string) ([64]byte, error) {
	var sig [64]byte
	raw, err := hex.DecodeString(s)
	if err != nil {
		return sig, fmt.Errorf("bad hex signature: %w", err)
	}
	if len(raw) != 64 {
		return sig, fmt.Errorf("signature must be 64 bytes, got %d", len(raw))
	}
	copy(sig[:], raw)
	return sig, nil
}

// SigString renders a 64-byte signature as hex.
func SigString(sig [64]byte) string {
	return hex.EncodeToString(sig[:])
}
