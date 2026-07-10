use anchor_lang::prelude::*;

/// Condition account per interface-contract.md §3. Seeds: ["market", market_id].
#[account]
pub struct Market {
    pub market_id: [u8; 32],
    pub outcome: MarketOutcome,
    pub resolver_authority: Pubkey,
    pub resolved_at: i64,
    pub oracle_tier: u8, // a = single-key resolver, b = challenge window, d = TxODDS-signed (ADR 0005)
    pub yes_mint: Pubkey,
    pub no_mint: Pubkey,
    pub usdc_mint: Pubkey,
    pub bump: u8,
}

impl Market {
    // discriminator(8) + [u8;32] + enum(1+pad->1) + pubkey(32) + i64(8) + u8(1) + pubkey*3(96) + u8(1)
    pub const SPACE: usize = 8 + 32 + 1 + 32 + 8 + 1 + 32 + 32 + 32 + 1;
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, PartialEq, Eq, Debug)]
pub enum MarketOutcome {
    Unresolved,
    Yes,
    No,
    Void,
}

/// Seeds: ["ostatus", order_hash]. Chain-authoritative fill-accounting
/// (interface-contract.md §6.2) — Postgres `orders.remaining` is a mirror only.
#[account]
pub struct OrderStatus {
    pub order_hash: [u8; 32],
    pub remaining: u64,
    pub is_filled_or_cancelled: bool,
    pub bump: u8,
}

impl OrderStatus {
    pub const SPACE: usize = 8 + 32 + 8 + 1 + 1;
}

/// Per-user custody authority. Day-0 decision (interface-contract.md §6.1): Vault PDA
/// over SPL delegate — one deposit tx, simpler crank accounting. Deliberately holds no
/// balance field: real money lives in the vault-owned USDC/outcome-mint ATAs (derived
/// from this PDA as authority), so there is a single source of truth and nothing to
/// drift out of sync. Settlement moves tokens directly between vault-owned ATAs via
/// CPI signed with these seeds — never through an internal ledger.
#[account]
pub struct Vault {
    pub owner: Pubkey,
    pub bump: u8,
}

impl Vault {
    pub const SPACE: usize = 8 + 32 + 1;
}

/// Seeds: ["combo", quote_hash].
#[account]
pub struct ComboEscrow {
    pub quote_hash: [u8; 32],
    pub taker: Pubkey,
    pub maker: Pubkey,
    pub legs: [Leg; ComboEscrow::MAX_LEGS],
    pub leg_count: u8,
    pub stake: u64,
    pub payout: u64,
    pub status: ComboStatus,
    pub bump: u8,
}

impl ComboEscrow {
    pub const MAX_LEGS: usize = 6; // TODO: confirm final cap with combo builder UI (ADR 0004)
    pub const SPACE: usize =
        8 + 32 + 32 + 32 + (33 * Self::MAX_LEGS) + 1 + 8 + 8 + 1 + 1;
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, PartialEq, Eq, Debug)]
pub struct Leg {
    pub market_id: [u8; 32],
    pub outcome: u8,
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, PartialEq, Eq, Debug)]
pub enum ComboStatus {
    Accepted,
    Won,
    Lost,
    Void,
}

/// Seeds: ["qstatus", quote_hash]. Marks a combo quote single-use on accept.
#[account]
pub struct QuoteStatus {
    pub quote_hash: [u8; 32],
    pub spent: bool,
    pub bump: u8,
}

impl QuoteStatus {
    pub const SPACE: usize = 8 + 32 + 1 + 1;
}

/// Canonical order message (interface-contract.md §1). Not an account — passed as
/// an instruction arg and hashed/verified against the maker's ed25519 signature.
/// Field order and widths MUST match backend/internal/models/order.go borshOrder().
#[derive(AnchorSerialize, AnchorDeserialize, Clone, Debug)]
pub struct OrderArgs {
    pub maker: Pubkey,
    pub market_id: [u8; 32],
    pub outcome: u8,
    pub side: u8,
    pub price: u16,
    pub size: u64,
    pub fee_bps: u16,
    pub expiry: i64,
    pub salt: u64,
}

pub const OUTCOME_NO: u8 = 0;
pub const OUTCOME_YES: u8 = 1;
pub const SIDE_BUY: u8 = 0;
pub const SIDE_SELL: u8 = 1;

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, PartialEq, Eq, Debug)]
pub enum MatchType {
    Normal,
    Mint,
    Merge,
}

impl TryFrom<u8> for MatchType {
    type Error = ();
    fn try_from(v: u8) -> core::result::Result<Self, Self::Error> {
        match v {
            0 => Ok(MatchType::Normal),
            1 => Ok(MatchType::Mint),
            2 => Ok(MatchType::Merge),
            _ => Err(()),
        }
    }
}
