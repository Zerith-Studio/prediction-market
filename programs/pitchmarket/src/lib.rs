use anchor_lang::prelude::*;
use anchor_lang::solana_program::sysvar::instructions::ID as IX_SYSVAR_ID;
use anchor_spl::associated_token::AssociatedToken;
use anchor_spl::token::{self, Burn, Mint, MintTo, Token, TokenAccount, Transfer};

mod errors;
mod sig_verify;
mod state;

use errors::PitchMarketError;
use state::*;

declare_id!("3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs");

/// 1 share == 1 USDC of redemption value, priced in integer cents 1..99
/// (interface-contract.md §0). MICRO_PER_CENT converts price-in-cents * shares
/// into micro-USDC (1 USDC == 1_000_000 micro-USDC == 100 cents).
pub const MICRO_PER_CENT: u64 = 10_000;
pub const MICRO_PER_SHARE: u64 = 1_000_000;

#[program]
pub mod pitchmarket {
    use super::*;

    /// Creates the Market condition PDA + its yes/no outcome mints + collateral
    /// pool ATA. Operator-only (whoever pays is the de facto market creator; E2's
    /// auto market creation calls this per PROJECT_PLAN.md §3).
    pub fn initialize_market(
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

    /// Opens a per-user custody PDA. No balance is stored on Vault itself — see
    /// state::Vault doc comment. Call once per user; deposit/withdraw ATAs are
    /// created lazily by `deposit`/`redeem` via init_if_needed.
    pub fn init_vault(ctx: Context<InitVault>) -> Result<()> {
        let vault = &mut ctx.accounts.vault;
        vault.owner = ctx.accounts.user.key();
        vault.bump = ctx.bumps.vault;
        Ok(())
    }

    /// Moves real USDC from the user's wallet ATA into their vault-owned ATA.
    /// This is the only step where the user signs live (Privy popup) — trading
    /// itself is silent, off-chain-signed orders relayed by the operator.
    pub fn deposit(ctx: Context<Deposit>, amount: u64) -> Result<()> {
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

    /// Maker directly signs this tx to cancel (interface-contract.md §4). If the
    /// order was never touched by settle_match, OrderStatus is created fresh with
    /// remaining=0 — is_filled_or_cancelled short-circuits any later fill attempt
    /// regardless of the remaining value.
    pub fn cancel_order(ctx: Context<CancelOrder>, _order_hash: [u8; 32]) -> Result<()> {
        let status = &mut ctx.accounts.order_status;
        require!(!status.is_filled_or_cancelled, PitchMarketError::OrderClosed);
        status.is_filled_or_cancelled = true;
        status.bump = ctx.bumps.order_status;
        Ok(())
    }

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

        let market_id = ctx.accounts.market.market_id;
        let market_bump = ctx.accounts.market.bump;
        let market_signer: &[&[&[u8]]] = &[&[b"market", &market_id, &[market_bump]]];

        match MatchType::try_from(match_type).map_err(|_| PitchMarketError::NotImplemented)? {
            MatchType::Normal => settle_normal(&ctx, &taker, &maker, fill_price, fill_size)?,
            MatchType::Mint => settle_mint(&ctx, &taker, &maker, fill_size, market_signer)?,
            MatchType::Merge => settle_merge(&ctx, &taker, &maker, fill_size, market_signer)?,
        }

        Ok(())
    }

    /// Tier-a resolution only (single resolver authority key). Tier b (bonded
    /// challenge window) and tier d (TxODDS ed25519-signed outcome, ADR 0005) are
    /// the E1 Jul 12–13 milestone per PROJECT_PLAN.md §7 — not yet implemented.
    pub fn resolve_market(ctx: Context<ResolveMarket>, outcome: u8) -> Result<()> {
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

    /// Burns `amount` winning (or, if VOID, either) outcome shares from the
    /// caller's vault-owned ATA and pays out `amount` USDC 1:1 from the market's
    /// collateral pool directly to the caller's own wallet ATA.
    pub fn redeem(ctx: Context<Redeem>, outcome: u8, amount: u64) -> Result<()> {
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

    /// TODO(E1 Jul 10–11, PROJECT_PLAN.md §7): verify quote signature + expiry +
    /// QuoteStatus.!spent, pull stake from taker, pull (payout-stake) from MM
    /// vault, open ComboEscrow, mark QuoteStatus.spent (ADR 0004).
    pub fn combo_accept(
        _ctx: Context<ComboAccept>,
        _quote: ComboQuoteArgs,
        _taker_sig: [u8; 64],
    ) -> Result<()> {
        err!(PitchMarketError::NotImplemented)
    }

    /// TODO(E1 Jul 10–11): read the N leg Market PDAs (ctx.remaining_accounts),
    /// compute AND across them, pay ComboEscrow to taker (all legs Yes) or MM
    /// (any leg No), VOID any leg → refund both pro-rata (ADR 0004).
    pub fn resolve_combo(_ctx: Context<ResolveCombo>) -> Result<()> {
        err!(PitchMarketError::NotImplemented)
    }
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
fn settle_mint(ctx: &Context<SettleMatch>, taker: &OrderArgs, maker: &OrderArgs, fill_size: u64, _market_signer: &[&[&[u8]]]) -> Result<()> {
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
fn settle_merge(ctx: &Context<SettleMatch>, taker: &OrderArgs, maker: &OrderArgs, fill_size: u64, _market_signer: &[&[&[u8]]]) -> Result<()> {
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

#[derive(Accounts)]
#[instruction(taker: OrderArgs, taker_sig: [u8; 64], maker: OrderArgs, maker_sig: [u8; 64], match_type: u8, fill_price: u16, fill_size: u64)]
pub struct SettleMatch<'info> {
    #[account(seeds = [b"market", market.market_id.as_ref()], bump = market.bump)]
    pub market: Account<'info, Market>,
    // Which of market.{yes,no}_mint each of these must equal depends on the
    // runtime taker.outcome/maker.outcome — checked explicitly in the handler
    // rather than via a static `address =` constraint.
    #[account(mut)]
    pub taker_outcome_mint: Account<'info, Mint>,
    #[account(mut)]
    pub maker_outcome_mint: Account<'info, Mint>,
    #[account(mut, seeds = [b"pool", market.market_id.as_ref()], bump)]
    pub pool_usdc: Account<'info, TokenAccount>,

    #[account(init_if_needed, payer = operator, space = OrderStatus::SPACE, seeds = [b"ostatus", sig_verify::order_hash(&taker).as_ref()], bump)]
    pub taker_order_status: Account<'info, OrderStatus>,
    #[account(init_if_needed, payer = operator, space = OrderStatus::SPACE, seeds = [b"ostatus", sig_verify::order_hash(&maker).as_ref()], bump)]
    pub maker_order_status: Account<'info, OrderStatus>,

    #[account(seeds = [b"vault", taker.maker.as_ref()], bump)]
    pub taker_vault: Account<'info, Vault>,
    #[account(seeds = [b"vault", maker.maker.as_ref()], bump)]
    pub maker_vault: Account<'info, Vault>,

    #[account(mut, associated_token::mint = market.usdc_mint, associated_token::authority = taker_vault)]
    pub taker_usdc_ata: Account<'info, TokenAccount>,
    #[account(mut, associated_token::mint = market.usdc_mint, associated_token::authority = maker_vault)]
    pub maker_usdc_ata: Account<'info, TokenAccount>,
    #[account(mut, associated_token::mint = taker_outcome_mint, associated_token::authority = taker_vault)]
    pub taker_outcome_ata: Account<'info, TokenAccount>,
    #[account(mut, associated_token::mint = maker_outcome_mint, associated_token::authority = maker_vault)]
    pub maker_outcome_ata: Account<'info, TokenAccount>,

    #[account(mut)]
    pub operator: Signer<'info>,
    /// CHECK: verified by address == IX_SYSVAR_ID in the handler
    pub instructions_sysvar: UncheckedAccount<'info>,
    pub token_program: Program<'info, Token>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct ResolveMarket<'info> {
    #[account(mut, seeds = [b"market", market.market_id.as_ref()], bump = market.bump)]
    pub market: Account<'info, Market>,
    pub resolver: Signer<'info>,
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
