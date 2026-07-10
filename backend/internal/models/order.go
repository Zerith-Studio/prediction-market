// Package models mirrors the canonical structs from docs/interface-contract.md §1-2.
// Field order and types must stay byte-identical to the Anchor program's borsh structs
// (programs/pitchmarket/src/state.rs) since order_hash/quote_hash are computed over this encoding.
package models

// Outcome and Side enums per interface-contract.md §1.
const (
	OutcomeNo  uint8 = 0
	OutcomeYes uint8 = 1

	SideBuy  uint8 = 0
	SideSell uint8 = 1
)

// Order is the canonical borsh-serialized struct the user signs client-side (ed25519).
// E2 stores it, the crank passes it into settle_match, E1 verifies the signature on-chain.
type Order struct {
	Maker    [32]byte `json:"maker"`     // user pubkey
	MarketID [32]byte `json:"market_id"` // sha256(match_id, template_key)
	Outcome  uint8    `json:"outcome"`   // 0 = NO, 1 = YES
	Side     uint8    `json:"side"`      // 0 = BUY (provide USDC), 1 = SELL (provide tokens)
	Price    uint16   `json:"price"`     // 1..99 (¢, implied P of `outcome`)
	Size     uint64   `json:"size"`      // shares
	FeeBps   uint16   `json:"fee_bps"`   // baseFeeRate; 0 for demo
	Expiry   int64    `json:"expiry"`    // unix; 0 = GTC, else GTD
	Salt     uint64   `json:"salt"`      // per-order uniqueness, not a sequential nonce
	Sig      [64]byte `json:"-"`         // ed25519(maker_privkey, borsh(Order))
}

// ComboQuote is the MM/bot → taker signed quote (interface-contract.md §2).
type ComboQuote struct {
	Maker  [32]byte `json:"maker"`
	Legs   []Leg    `json:"legs"`
	Stake  uint64   `json:"stake"`  // taker pays (micro-USDC)
	Payout uint64   `json:"payout"` // total pot; MM risk = payout - stake
	Expiry int64    `json:"expiry"`
	Salt   uint64   `json:"salt"`
	Sig    [64]byte `json:"-"`
}

type Leg struct {
	MarketID [32]byte `json:"market_id"`
	Outcome  uint8    `json:"outcome"`
}

// MatchType mirrors the on-chain settle_match match_type (interface-contract.md §4).
type MatchType uint8

const (
	MatchNormal MatchType = iota
	MatchMint
	MatchMerge
)

// OrderStatus is the off-chain mirror of the on-chain OrderStatus PDA.
// The chain (OrderStatus PDA, seeds ["ostatus", order_hash]) is authoritative;
// this is a read cache for fast UI/book queries only.
type OrderStatus struct {
	OrderHash            string `json:"order_hash"`
	Remaining            uint64 `json:"remaining"`
	IsFilledOrCancelled  bool   `json:"is_filled_or_cancelled"`
}
