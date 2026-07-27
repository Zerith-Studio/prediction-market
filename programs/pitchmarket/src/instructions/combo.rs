use anchor_lang::prelude::*;

use crate::errors::PitchMarketError;
use crate::state::Leg;

/// TODO(E1 Jul 10–11, PROJECT_PLAN.md §7): verify quote signature + expiry +
/// QuoteStatus.!spent, pull stake from taker, pull (payout-stake) from MM
/// vault, open ComboEscrow, mark QuoteStatus.spent (ADR 0004).
pub fn combo_accept_handler(
    _ctx: Context<ComboAccept>,
    _quote: ComboQuoteArgs,
    _taker_sig: [u8; 64],
) -> Result<()> {
    err!(PitchMarketError::NotImplemented)
}

/// TODO(E1 Jul 10–11): read the N leg Market PDAs (ctx.remaining_accounts),
/// compute AND across them, pay ComboEscrow to taker (all legs Yes) or MM
/// (any leg No), VOID any leg → refund both pro-rata (ADR 0004).
pub fn resolve_combo_handler(_ctx: Context<ResolveCombo>) -> Result<()> {
    err!(PitchMarketError::NotImplemented)
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Debug)]
pub struct ComboQuoteArgs {
    pub maker: Pubkey,
    pub legs: Vec<Leg>,
    pub stake: u64,
    pub payout: u64,
    pub expiry: i64,
    pub salt: u64,
}

#[derive(Accounts)]
pub struct ComboAccept<'info> {
    /// CHECK: TODO — accounts finalized when combo_accept is implemented (E1 Jul 10–11)
    pub placeholder: UncheckedAccount<'info>,
}

#[derive(Accounts)]
pub struct ResolveCombo<'info> {
    /// CHECK: TODO — accounts finalized when resolve_combo is implemented (E1 Jul 10–11)
    pub placeholder: UncheckedAccount<'info>,
}
