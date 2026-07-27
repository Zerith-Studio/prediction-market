use anchor_lang::prelude::*;
use anchor_lang::solana_program::sysvar::instructions::ID as IX_SYSVAR_ID;
use anchor_spl::token::{self, Burn, Mint, MintTo, Token, TokenAccount, Transfer};

use crate::errors::PitchMarketError;
use crate::sig_verify;
use crate::state::*;
use crate::MICRO_PER_CENT;

/// Settles one match produced by the E2 matching engine (interface-contract.md
/// §4). Operator is fee payer; can only move funds according to the two
/// user-signed Order messages passed in — it cannot forge or over-fill
/// (docs/adr/0003). One tx per match (single taker + single maker; E2's crank
/// currently submits one Fill per tx — see backend/internal/crank/crank.go).
///
/// **Required transaction layout** (crank must build txs this way —
/// backend/internal/crank/crank.go doesn't yet, that's the next TODO there):
/// ix[0] = Ed25519Program instruction verifying `taker_sig` over
/// `borsh(taker)` by `taker.maker`; ix[1] = same for `maker_sig`/`maker`;
/// ix[2] = this settle_match call. sig_verify::verify_order_signature reads
/// ix[0]/ix[1] via the instructions sysvar — see sig_verify.rs for the exact
/// precompile data layout being checked.
pub fn settle_match_handler(
    ctx: Context<SettleMatch>,
    taker: OrderArgs,
    taker_sig: [u8; 64],
    maker: OrderArgs,
    maker_sig: [u8; 64],
    match_type: u8,
    fill_price: u16,
    fill_size: u64,
) -> Result<()> {
    require!(
        ctx.accounts.market.outcome == MarketOutcome::Unresolved,
        PitchMarketError::MarketNotOpen
    );
    require!(fill_price >= 1 && fill_price <= 99, PitchMarketError::InvalidPrice);
    require_keys_eq!(
        ctx.accounts.instructions_sysvar.key(),
        IX_SYSVAR_ID,
        PitchMarketError::BadSignature
    );

    // taker_outcome_mint/maker_outcome_mint are caller-supplied (not pinned by
    // an `address =` constraint, since which of yes_mint/no_mint applies
    // depends on the runtime outcome field) — verify explicitly here instead.
    let expected_taker_mint = if taker.outcome == OUTCOME_YES { ctx.accounts.market.yes_mint } else { ctx.accounts.market.no_mint };
    let expected_maker_mint = if maker.outcome == OUTCOME_YES { ctx.accounts.market.yes_mint } else { ctx.accounts.market.no_mint };
    require_keys_eq!(ctx.accounts.taker_outcome_mint.key(), expected_taker_mint, PitchMarketError::NotImplemented);
    require_keys_eq!(ctx.accounts.maker_outcome_mint.key(), expected_maker_mint, PitchMarketError::NotImplemented);

    sig_verify::verify_order_signature(
        &ctx.accounts.instructions_sysvar,
        0,
        &taker,
        &taker_sig,
    )?;
    sig_verify::verify_order_signature(
        &ctx.accounts.instructions_sysvar,
        1,
        &maker,
        &maker_sig,
    )?;

    apply_order_fill(&mut ctx.accounts.taker_order_status, &taker, fill_size, ctx.bumps.taker_order_status)?;
    apply_order_fill(&mut ctx.accounts.maker_order_status, &maker, fill_size, ctx.bumps.maker_order_status)?;

    match MatchType::try_from(match_type).map_err(|_| PitchMarketError::NotImplemented)? {
        MatchType::Normal => settle_normal(&ctx, &taker, &maker, fill_price, fill_size)?,
        MatchType::Mint => settle_mint(&ctx, &taker, &maker, fill_size)?,
        MatchType::Merge => settle_merge(&ctx, &taker, &maker, fill_size)?,
    }

    Ok(())
}

fn apply_order_fill(status: &mut OrderStatus, order: &OrderArgs, fill_size: u64, bump: u8) -> Result<()> {
    if status.order_hash == [0u8; 32] {
        status.order_hash = sig_verify::order_hash(order);
        status.remaining = order.size;
        status.bump = bump;
    }
    require!(!status.is_filled_or_cancelled, PitchMarketError::OrderClosed);
    require!(status.remaining >= fill_size, PitchMarketError::OverFill);
    status.remaining -= fill_size;
    if status.remaining == 0 {
        status.is_filled_or_cancelled = true;
    }
    Ok(())
}

/// Direct peer-to-peer swap: taker and maker on the SAME outcome, opposite sides.
/// No minting/burning, no pool involvement — just USDC-for-shares between the two
/// vault-owned ATA pairs, each CPI signed by its own vault PDA.
fn settle_normal(ctx: &Context<SettleMatch>, taker: &OrderArgs, maker: &OrderArgs, fill_price: u16, fill_size: u64) -> Result<()> {
    require!(taker.outcome == maker.outcome, PitchMarketError::NotImplemented);
    require!(taker.side != maker.side, PitchMarketError::NotImplemented);

    // Resolve buyer/seller to the correct (vault AccountInfo, ATA, bump) triple for
    // whichever of taker/maker actually holds that role this trade.
    let (buyer_key, buyer_vault_ai, buyer_usdc, buyer_outcome, buyer_bump,
         seller_key, seller_vault_ai, seller_usdc, seller_outcome, seller_bump) =
        if taker.side == SIDE_BUY {
            (taker.maker, ctx.accounts.taker_vault.to_account_info(), &ctx.accounts.taker_usdc_ata, &ctx.accounts.taker_outcome_ata, ctx.bumps.taker_vault,
             maker.maker, ctx.accounts.maker_vault.to_account_info(), &ctx.accounts.maker_usdc_ata, &ctx.accounts.maker_outcome_ata, ctx.bumps.maker_vault)
        } else {
            (maker.maker, ctx.accounts.maker_vault.to_account_info(), &ctx.accounts.maker_usdc_ata, &ctx.accounts.maker_outcome_ata, ctx.bumps.maker_vault,
             taker.maker, ctx.accounts.taker_vault.to_account_info(), &ctx.accounts.taker_usdc_ata, &ctx.accounts.taker_outcome_ata, ctx.bumps.taker_vault)
        };

    let usdc_amount = (fill_price as u64)
        .checked_mul(fill_size)
        .and_then(|v| v.checked_mul(MICRO_PER_CENT))
        .ok_or(PitchMarketError::OverFill)?;

    let buyer_signer: &[&[&[u8]]] = &[&[b"vault", buyer_key.as_ref(), &[buyer_bump]]];
    token::transfer(
        CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            Transfer { from: buyer_usdc.to_account_info(), to: seller_usdc.to_account_info(), authority: buyer_vault_ai },
            buyer_signer,
        ),
        usdc_amount,
    )?;

    let seller_signer: &[&[&[u8]]] = &[&[b"vault", seller_key.as_ref(), &[seller_bump]]];
    token::transfer(
        CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            Transfer { from: seller_outcome.to_account_info(), to: buyer_outcome.to_account_info(), authority: seller_vault_ai },
            seller_signer,
        ),
        fill_size,
    )?;

    Ok(())
}

/// Two BUY orders on opposite outcomes cross: mints one complete set's worth of
/// shares. taker pays taker.price¢/share into the pool and receives taker.outcome
/// shares; maker pays maker.price¢/share and receives maker.outcome shares
/// (prices should sum to 100 — the matching engine enforces this, ADR 0002).
fn settle_mint(ctx: &Context<SettleMatch>, taker: &OrderArgs, maker: &OrderArgs, fill_size: u64) -> Result<()> {
    require!(taker.side == SIDE_BUY && maker.side == SIDE_BUY, PitchMarketError::NotImplemented);
    require!(taker.outcome != maker.outcome, PitchMarketError::NotImplemented);

    for (order, vault, vault_bump, usdc_ata, outcome_mint, outcome_ata) in [
        (taker, &ctx.accounts.taker_vault, ctx.bumps.taker_vault, &ctx.accounts.taker_usdc_ata, &ctx.accounts.taker_outcome_mint, &ctx.accounts.taker_outcome_ata),
        (maker, &ctx.accounts.maker_vault, ctx.bumps.maker_vault, &ctx.accounts.maker_usdc_ata, &ctx.accounts.maker_outcome_mint, &ctx.accounts.maker_outcome_ata),
    ] {
        let pay = (order.price as u64)
            .checked_mul(fill_size)
            .and_then(|v| v.checked_mul(MICRO_PER_CENT))
            .ok_or(PitchMarketError::OverFill)?;
        let signer: &[&[&[u8]]] = &[&[b"vault", order.maker.as_ref(), &[vault_bump]]];
        token::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                Transfer { from: usdc_ata.to_account_info(), to: ctx.accounts.pool_usdc.to_account_info(), authority: vault.to_account_info() },
                signer,
            ),
            pay,
        )?;

        let mint_signer: &[&[&[u8]]] = &[&[b"market", &ctx.accounts.market.market_id, &[ctx.accounts.market.bump]]];
        token::mint_to(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                MintTo { mint: outcome_mint.to_account_info(), to: outcome_ata.to_account_info(), authority: ctx.accounts.market.to_account_info() },
                mint_signer,
            ),
            fill_size,
        )?;
    }
    Ok(())
}

/// Two SELL orders on opposite outcomes cross: burns one complete set's worth of
/// shares and releases the pooled collateral back to the two sellers.
fn settle_merge(ctx: &Context<SettleMatch>, taker: &OrderArgs, maker: &OrderArgs, fill_size: u64) -> Result<()> {
    require!(taker.side == SIDE_SELL && maker.side == SIDE_SELL, PitchMarketError::NotImplemented);
    require!(taker.outcome != maker.outcome, PitchMarketError::NotImplemented);

    for (order, vault, vault_bump, usdc_ata, outcome_mint, outcome_ata) in [
        (taker, &ctx.accounts.taker_vault, ctx.bumps.taker_vault, &ctx.accounts.taker_usdc_ata, &ctx.accounts.taker_outcome_mint, &ctx.accounts.taker_outcome_ata),
        (maker, &ctx.accounts.maker_vault, ctx.bumps.maker_vault, &ctx.accounts.maker_usdc_ata, &ctx.accounts.maker_outcome_mint, &ctx.accounts.maker_outcome_ata),
    ] {
        let vault_signer: &[&[&[u8]]] = &[&[b"vault", order.maker.as_ref(), &[vault_bump]]];
        token::burn(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                Burn { mint: outcome_mint.to_account_info(), from: outcome_ata.to_account_info(), authority: vault.to_account_info() },
                vault_signer,
            ),
            fill_size,
        )?;

        let payout = (order.price as u64)
            .checked_mul(fill_size)
            .and_then(|v| v.checked_mul(MICRO_PER_CENT))
            .ok_or(PitchMarketError::OverFill)?;
        let market_signer: &[&[&[u8]]] = &[&[b"market", &ctx.accounts.market.market_id, &[ctx.accounts.market.bump]]];
        token::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                Transfer { from: ctx.accounts.pool_usdc.to_account_info(), to: usdc_ata.to_account_info(), authority: ctx.accounts.market.to_account_info() },
                market_signer,
            ),
            payout,
        )?;
    }
    Ok(())
}

#[derive(Accounts)]
#[instruction(taker: OrderArgs, taker_sig: [u8; 64], maker: OrderArgs, maker_sig: [u8; 64], match_type: u8, fill_price: u16, fill_size: u64)]
pub struct SettleMatch<'info> {
    #[account(seeds = [b"market", market.market_id.as_ref()], bump = market.bump)]
    pub market: Account<'info, Market>,
    // Which of market.{yes,no}_mint each of these must equal depends on the
    // runtime taker.outcome/maker.outcome — checked explicitly in the handler
    // rather than via a static `address =` constraint.
    //
    // Accounts are Boxed to keep them off the BPF stack: this context has 18
    // accounts and the generated `try_accounts` otherwise overflows the 4KB
    // frame. Boxing moves the deserialized data to the heap.
    #[account(mut)]
    pub taker_outcome_mint: Box<Account<'info, Mint>>,
    #[account(mut)]
    pub maker_outcome_mint: Box<Account<'info, Mint>>,
    #[account(mut, seeds = [b"pool", market.market_id.as_ref()], bump)]
    pub pool_usdc: Box<Account<'info, TokenAccount>>,

    #[account(init_if_needed, payer = operator, space = OrderStatus::SPACE, seeds = [b"ostatus", sig_verify::order_hash(&taker).as_ref()], bump)]
    pub taker_order_status: Box<Account<'info, OrderStatus>>,
    #[account(init_if_needed, payer = operator, space = OrderStatus::SPACE, seeds = [b"ostatus", sig_verify::order_hash(&maker).as_ref()], bump)]
    pub maker_order_status: Box<Account<'info, OrderStatus>>,

    #[account(seeds = [b"vault", taker.maker.as_ref()], bump)]
    pub taker_vault: Box<Account<'info, Vault>>,
    #[account(seeds = [b"vault", maker.maker.as_ref()], bump)]
    pub maker_vault: Box<Account<'info, Vault>>,

    #[account(mut, associated_token::mint = market.usdc_mint, associated_token::authority = taker_vault)]
    pub taker_usdc_ata: Box<Account<'info, TokenAccount>>,
    #[account(mut, associated_token::mint = market.usdc_mint, associated_token::authority = maker_vault)]
    pub maker_usdc_ata: Box<Account<'info, TokenAccount>>,
    #[account(mut, associated_token::mint = taker_outcome_mint, associated_token::authority = taker_vault)]
    pub taker_outcome_ata: Box<Account<'info, TokenAccount>>,
    #[account(mut, associated_token::mint = maker_outcome_mint, associated_token::authority = maker_vault)]
    pub maker_outcome_ata: Box<Account<'info, TokenAccount>>,

    #[account(mut)]
    pub operator: Signer<'info>,
    /// CHECK: verified by address == IX_SYSVAR_ID in the handler
    pub instructions_sysvar: UncheckedAccount<'info>,
    pub token_program: Program<'info, Token>,
    pub system_program: Program<'info, System>,
}
