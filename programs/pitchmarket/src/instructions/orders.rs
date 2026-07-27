use anchor_lang::prelude::*;

use crate::errors::PitchMarketError;
use crate::state::OrderStatus;

/// Maker directly signs this tx to cancel (interface-contract.md §4). If the
/// order was never touched by settle_match, OrderStatus is created fresh with
/// remaining=0 — is_filled_or_cancelled short-circuits any later fill attempt
/// regardless of the remaining value.
pub fn cancel_order_handler(ctx: Context<CancelOrder>, _order_hash: [u8; 32]) -> Result<()> {
    let status = &mut ctx.accounts.order_status;
    require!(!status.is_filled_or_cancelled, PitchMarketError::OrderClosed);
    status.is_filled_or_cancelled = true;
    status.bump = ctx.bumps.order_status;
    Ok(())
}

#[derive(Accounts)]
#[instruction(order_hash: [u8; 32])]
pub struct CancelOrder<'info> {
    #[account(
        init_if_needed,
        payer = maker,
        space = OrderStatus::SPACE,
        seeds = [b"ostatus", order_hash.as_ref()],
        bump,
    )]
    pub order_status: Account<'info, OrderStatus>,
    #[account(mut)]
    pub maker: Signer<'info>,
    pub system_program: Program<'info, System>,
}
