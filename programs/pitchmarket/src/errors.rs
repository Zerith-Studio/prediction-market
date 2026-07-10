use anchor_lang::prelude::*;

#[error_code]
pub enum PitchMarketError {
    #[msg("order has expired")]
    OrderExpired,
    #[msg("order price out of range (1..99)")]
    InvalidPrice,
    #[msg("order already filled or cancelled")]
    OrderClosed,
    #[msg("fill size exceeds order remaining")]
    OverFill,
    #[msg("insufficient vault balance")]
    InsufficientFunds,
    #[msg("ed25519 order signature missing or invalid")]
    BadSignature,
    #[msg("market is not open for settlement")]
    MarketNotOpen,
    #[msg("market already resolved")]
    MarketAlreadyResolved,
    #[msg("market not yet resolved")]
    MarketNotResolved,
    #[msg("caller is not the configured resolver authority")]
    Unauthorized,
    #[msg("combo quote already spent or expired")]
    QuoteClosed,
    #[msg("too many combo legs (see ComboEscrow::MAX_LEGS)")]
    TooManyLegs,
    #[msg("this instruction path is not yet implemented — see TODO in source")]
    NotImplemented,
}
