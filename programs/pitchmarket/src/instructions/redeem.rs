use anchor_lang::prelude::*;
use anchor_spl::token::{self, Burn, Mint, Token, TokenAccount, Transfer};

use crate::errors::PitchMarketError;
use crate::state::{Market, MarketOutcome, Vault, OUTCOME_NO, OUTCOME_YES};
use crate::MICRO_PER_SHARE;

/// Burns `amount` winning (or, if VOID, either) outcome shares from the
/// caller's vault-owned ATA and pays out `amount` USDC 1:1 from the market's
/// collateral pool directly to the caller's own wallet ATA.
pub fn redeem_handler(ctx: Context<Redeem>, outcome: u8, amount: u64) -> Result<()> {
    let market = &ctx.accounts.market;
    require!(market.outcome != MarketOutcome::Unresolved, PitchMarketError::MarketNotResolved);
    if market.outcome != MarketOutcome::Void {
        let winning_outcome = if market.outcome == MarketOutcome::Yes { OUTCOME_YES } else { OUTCOME_NO };
        require!(outcome == winning_outcome, PitchMarketError::MarketNotOpen);
    }

    let burn_accounts = Burn {
        mint: ctx.accounts.outcome_mint.to_account_info(),
        from: ctx.accounts.user_outcome_ata.to_account_info(),
        authority: ctx.accounts.vault.to_account_info(),
    };
    let vault_bump = ctx.accounts.vault.bump;
    let owner_key = ctx.accounts.user.key();
    let vault_signer: &[&[&[u8]]] = &[&[b"vault", owner_key.as_ref(), &[vault_bump]]];
    token::burn(
        CpiContext::new_with_signer(ctx.accounts.token_program.to_account_info(), burn_accounts, vault_signer),
        amount,
    )?;

    let market_id = market.market_id;
    let market_bump = market.bump;
    let market_signer: &[&[&[u8]]] = &[&[b"market", &market_id, &[market_bump]]];
    let payout_accounts = Transfer {
        from: ctx.accounts.pool_usdc.to_account_info(),
        to: ctx.accounts.user_usdc_ata.to_account_info(),
        authority: ctx.accounts.market.to_account_info(),
    };
    token::transfer(
        CpiContext::new_with_signer(ctx.accounts.token_program.to_account_info(), payout_accounts, market_signer),
        amount.checked_mul(MICRO_PER_SHARE).ok_or(PitchMarketError::OverFill)?,
    )?;
    Ok(())
}

#[derive(Accounts)]
pub struct Redeem<'info> {
    #[account(seeds = [b"market", market.market_id.as_ref()], bump = market.bump)]
    pub market: Account<'info, Market>,
    #[account(seeds = [b"vault", user.key().as_ref()], bump = vault.bump)]
    pub vault: Account<'info, Vault>,
    #[account(mut)]
    pub outcome_mint: Account<'info, Mint>,
    #[account(mut, associated_token::mint = outcome_mint, associated_token::authority = vault)]
    pub user_outcome_ata: Account<'info, TokenAccount>,
    #[account(mut, seeds = [b"pool", market.market_id.as_ref()], bump)]
    pub pool_usdc: Account<'info, TokenAccount>,
    #[account(mut)]
    pub user_usdc_ata: Account<'info, TokenAccount>,
    #[account(mut)]
    pub user: Signer<'info>,
    pub token_program: Program<'info, Token>,
}
