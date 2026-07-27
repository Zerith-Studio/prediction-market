use anchor_lang::prelude::*;
use anchor_spl::token::{Mint, Token, TokenAccount};

use crate::state::{Market, MarketOutcome};

/// Creates the Market condition PDA + its yes/no outcome mints + collateral
/// pool ATA. Operator-only (whoever pays is the de facto market creator; E2's
/// auto market creation calls this per PROJECT_PLAN.md §3).
pub fn initialize_market_handler(
    ctx: Context<InitializeMarket>,
    market_id: [u8; 32],
    oracle_tier: u8,
    resolver_authority: Pubkey,
) -> Result<()> {
    let market = &mut ctx.accounts.market;
    market.market_id = market_id;
    market.outcome = MarketOutcome::Unresolved;
    market.resolver_authority = resolver_authority;
    market.resolved_at = 0;
    market.oracle_tier = oracle_tier;
    market.yes_mint = ctx.accounts.yes_mint.key();
    market.no_mint = ctx.accounts.no_mint.key();
    market.usdc_mint = ctx.accounts.usdc_mint.key();
    market.bump = ctx.bumps.market;
    Ok(())
}

#[derive(Accounts)]
#[instruction(market_id: [u8; 32])]
pub struct InitializeMarket<'info> {
    #[account(init, payer = operator, space = Market::SPACE, seeds = [b"market", market_id.as_ref()], bump)]
    pub market: Account<'info, Market>,
    #[account(init, payer = operator, mint::decimals = 0, mint::authority = market, seeds = [b"yes", market_id.as_ref()], bump)]
    pub yes_mint: Account<'info, Mint>,
    #[account(init, payer = operator, mint::decimals = 0, mint::authority = market, seeds = [b"no", market_id.as_ref()], bump)]
    pub no_mint: Account<'info, Mint>,
    #[account(
        init,
        payer = operator,
        seeds = [b"pool", market_id.as_ref()],
        bump,
        token::mint = usdc_mint,
        token::authority = market,
    )]
    pub pool_usdc: Account<'info, TokenAccount>,
    pub usdc_mint: Account<'info, Mint>,
    #[account(mut)]
    pub operator: Signer<'info>,
    pub token_program: Program<'info, Token>,
    pub system_program: Program<'info, System>,
    pub rent: Sysvar<'info, Rent>,
}
