use anchor_lang::prelude::*;

use crate::errors::PitchMarketError;
use crate::state::{Market, MarketOutcome};

/// Tier-a resolution only (single resolver authority key). Tier b (bonded
/// challenge window) and tier d (TxODDS ed25519-signed outcome, ADR 0005) are
/// the E1 Jul 12–13 milestone per PROJECT_PLAN.md §7 — not yet implemented.
pub fn resolve_market_handler(ctx: Context<ResolveMarket>, outcome: u8) -> Result<()> {
    let market = &mut ctx.accounts.market;
    require!(market.oracle_tier == 0, PitchMarketError::NotImplemented);
    require!(
        market.outcome == MarketOutcome::Unresolved,
        PitchMarketError::MarketAlreadyResolved
    );
    require_keys_eq!(
        ctx.accounts.resolver.key(),
        market.resolver_authority,
        PitchMarketError::Unauthorized
    );
    market.outcome = match outcome {
        0 => MarketOutcome::No,
        1 => MarketOutcome::Yes,
        2 => MarketOutcome::Void,
        _ => return err!(PitchMarketError::NotImplemented),
    };
    market.resolved_at = Clock::get()?.unix_timestamp;
    Ok(())
}

#[derive(Accounts)]
pub struct ResolveMarket<'info> {
    #[account(mut, seeds = [b"market", market.market_id.as_ref()], bump = market.bump)]
    pub market: Account<'info, Market>,
    pub resolver: Signer<'info>,
}
