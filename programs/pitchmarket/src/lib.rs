use anchor_lang::prelude::*;

mod errors;
mod instructions;
mod sig_verify;
mod state;

use instructions::*;
use state::OrderArgs;

declare_id!("3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs");

/// 1 share == 1 USDC of redemption value, priced in integer cents 1..99
/// (interface-contract.md §0). MICRO_PER_CENT converts price-in-cents * shares
/// into micro-USDC (1 USDC == 1_000_000 micro-USDC == 100 cents).
pub const MICRO_PER_CENT: u64 = 10_000;
pub const MICRO_PER_SHARE: u64 = 1_000_000;

#[program]
pub mod pitchmarket {
    use super::*;

    pub fn initialize_market(
        ctx: Context<InitializeMarket>,
        market_id: [u8; 32],
        oracle_tier: u8,
        resolver_authority: Pubkey,
    ) -> Result<()> {
        initialize_market_handler(ctx, market_id, oracle_tier, resolver_authority)
    }

    pub fn init_vault(ctx: Context<InitVault>) -> Result<()> {
        init_vault_handler(ctx)
    }

    pub fn deposit(ctx: Context<Deposit>, amount: u64) -> Result<()> {
        deposit_handler(ctx, amount)
    }

    /// Maker directly signs this tx to cancel (interface-contract.md §4).
    pub fn cancel_order(ctx: Context<CancelOrder>, order_hash: [u8; 32]) -> Result<()> {
        cancel_order_handler(ctx, order_hash)
    }

    pub fn settle_match(
        ctx: Context<SettleMatch>,
        taker: OrderArgs,
        taker_sig: [u8; 64],
        maker: OrderArgs,
        maker_sig: [u8; 64],
        match_type: u8,
        fill_price: u16,
        fill_size: u64,
    ) -> Result<()> {
        settle_match_handler(ctx, taker, taker_sig, maker, maker_sig, match_type, fill_price, fill_size)
    }

    pub fn resolve_market(ctx: Context<ResolveMarket>, outcome: u8) -> Result<()> {
        resolve_market_handler(ctx, outcome)
    }

    pub fn redeem(ctx: Context<Redeem>, outcome: u8, amount: u64) -> Result<()> {
        redeem_handler(ctx, outcome, amount)
    }

    pub fn combo_accept(
        ctx: Context<ComboAccept>,
        quote: ComboQuoteArgs,
        taker_sig: [u8; 64],
    ) -> Result<()> {
        combo_accept_handler(ctx, quote, taker_sig)
    }

    pub fn resolve_combo(ctx: Context<ResolveCombo>) -> Result<()> {
        resolve_combo_handler(ctx)
    }
}
