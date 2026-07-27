use anchor_lang::prelude::*;

use crate::errors::PitchMarketError;
use crate::sig_verify;
use crate::state::{OrderArgs, OrderStatus};

/// Maker directly signs this tx to cancel (interface-contract.md §4).
///
/// SECURITY: `order` is the caller-supplied full order, not a bare hash — the
/// `order_status` PDA seed is derived on-chain from `sig_verify::order_hash(&order)`,
/// and `maker` must be the exact `Signer` named in `order.maker`. Without this
/// check, order hashes are public (book/API/WS), so anyone could have cancelled
/// anyone else's resting order by simply replaying its known hash.
pub fn cancel_order_handler(ctx: Context<CancelOrder>, order: OrderArgs) -> Result<()> {
    require_keys_eq!(ctx.accounts.maker.key(), order.maker, PitchMarketError::Unauthorized);

    let status = &mut ctx.accounts.order_status;
    ensure_order_status_initialized(status, &order, ctx.bumps.order_status);
    require!(!status.is_filled_or_cancelled, PitchMarketError::OrderClosed);
    status.is_filled_or_cancelled = true;
    Ok(())
}

/// Lazily stamps a freshly-`init_if_needed`'d `OrderStatus` with its owning
/// order's hash/remaining on first touch — shared between `cancel_order` and
/// `settle_match`, whichever instruction reaches a given order first.
pub fn ensure_order_status_initialized(status: &mut OrderStatus, order: &OrderArgs, bump: u8) {
    if status.order_hash == [0u8; 32] {
        status.order_hash = sig_verify::order_hash(order);
        status.remaining = order.size;
        status.bump = bump;
    }
}

#[derive(Accounts)]
#[instruction(order: OrderArgs)]
pub struct CancelOrder<'info> {
    #[account(
        init_if_needed,
        payer = maker,
        space = OrderStatus::SPACE,
        seeds = [b"ostatus", sig_verify::order_hash(&order).as_ref()],
        bump,
    )]
    pub order_status: Account<'info, OrderStatus>,
    #[account(mut)]
    pub maker: Signer<'info>,
    pub system_program: Program<'info, System>,
}
