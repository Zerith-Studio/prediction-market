use anchor_lang::prelude::*;
use anchor_spl::associated_token::AssociatedToken;
use anchor_spl::token::{self, Mint, Token, TokenAccount, Transfer};

use crate::errors::PitchMarketError;
use crate::state::Vault;

/// Opens a per-user custody PDA. No balance is stored on Vault itself — see
/// state::Vault doc comment. Call once per user; deposit/withdraw ATAs are
/// created lazily by `deposit`/`redeem` via init_if_needed.
pub fn init_vault_handler(ctx: Context<InitVault>) -> Result<()> {
    let vault = &mut ctx.accounts.vault;
    vault.owner = ctx.accounts.user.key();
    vault.bump = ctx.bumps.vault;
    Ok(())
}

/// Moves real USDC from the user's wallet ATA into their vault-owned ATA.
/// This is the only step where the user signs live (Privy popup) — trading
/// itself is silent, off-chain-signed orders relayed by the operator.
pub fn deposit_handler(ctx: Context<Deposit>, amount: u64) -> Result<()> {
    let cpi_accounts = Transfer {
        from: ctx.accounts.user_usdc_ata.to_account_info(),
        to: ctx.accounts.vault_usdc_ata.to_account_info(),
        authority: ctx.accounts.user.to_account_info(),
    };
    token::transfer(
        CpiContext::new(ctx.accounts.token_program.to_account_info(), cpi_accounts),
        amount,
    )?;
    Ok(())
}

#[derive(Accounts)]
pub struct InitVault<'info> {
    #[account(init, payer = user, space = Vault::SPACE, seeds = [b"vault", user.key().as_ref()], bump)]
    pub vault: Account<'info, Vault>,
    #[account(mut)]
    pub user: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct Deposit<'info> {
    #[account(seeds = [b"vault", user.key().as_ref()], bump = vault.bump, has_one = owner @ PitchMarketError::Unauthorized)]
    pub vault: Account<'info, Vault>,
    /// CHECK: constrained via vault.has_one above
    pub owner: UncheckedAccount<'info>,
    #[account(mut)]
    pub user_usdc_ata: Account<'info, TokenAccount>,
    #[account(
        init_if_needed,
        payer = user,
        associated_token::mint = usdc_mint,
        associated_token::authority = vault,
    )]
    pub vault_usdc_ata: Account<'info, TokenAccount>,
    pub usdc_mint: Account<'info, Mint>,
    #[account(mut)]
    pub user: Signer<'info>,
    pub token_program: Program<'info, Token>,
    pub associated_token_program: Program<'info, AssociatedToken>,
    pub system_program: Program<'info, System>,
}
