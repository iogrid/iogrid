//! $GRID Emission — halving curve + provider rewards distribution.
//!
//! Implements the Bitcoin-style emission schedule from docs/TOKENOMICS.md (Layer 2):
//!
//! | Year  | Annual emission |
//! |-------|-----------------|
//! | 0-2   | 50,000,000      |
//! | 2-4   | 25,000,000      |
//! | 4-6   | 12,500,000      |
//! | 6-8   | 6,250,000       |
//! | 8-10  | 3,125,000       |
//! | 10+   | 0               |
//!
//! Year-10 cumulative target: ~485M (49% of 1B supply). All amounts are in raw token units
//! (i.e., multiplied by 10^9 for the 9-decimal $GRID mint).
//!
//! Halving period = 2 years = 63_072_000 seconds (365.25 * 86_400 * 2).
//!
//! Distribution model:
//! - `claim_epoch(epoch_id, total_reward)`: callable only by the off-chain billing-svc
//!   signer (configured in EmissionConfig). The function checks the halving curve, ensures
//!   `total_reward` ≤ the emission budget for the epoch, mints tokens to a vault, and records
//!   `EpochClaim`.
//! - `distribute_batch(epoch_id, [(provider, amount)])`: paginated CPI from vault → recipient
//!   ATAs. Recipient ATAs are typically vesting program PDAs (so the tokens land already-locked
//!   per Layer 3 of TOKENOMICS.md).
//!
//! Authority pattern: the grid_token mint authority is transferred to a PDA derived from this
//! program (`["emission-mint-authority"]`) once production starts. After that, only halving-
//! curve calls can mint $GRID.

use anchor_lang::prelude::*;
use anchor_spl::token_2022::{self, MintTo, Token2022, Transfer as Transfer2022};
use anchor_spl::token_interface::{Mint, TokenAccount};

declare_id!("GR1Demissionnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn");

/// Halving period: 2 years (in seconds, Julian-year averaged).
pub const HALVING_PERIOD_SECS: i64 = 365 * 86_400 * 2 + 86_400 / 2; // 63_115_200 ≈ 2y

/// Year-1 emission target (50M $GRID at 9 decimals).
pub const YEAR1_EMISSION_RAW: u64 = 50_000_000u64 * 1_000_000_000u64;

/// Hard halving cap — after 10 halvings emission is functionally zero. We use 5 because
/// year-10 (= halving #5) is the documented end of the provider rewards pool.
pub const MAX_HALVINGS: u32 = 5;

#[program]
pub mod emission {
    use super::*;

    /// Bootstraps the emission config: records the mint, vault, billing signer, and TGE time.
    pub fn initialize(ctx: Context<InitializeEmission>, tge_unix: i64) -> Result<()> {
        let cfg = &mut ctx.accounts.config;
        cfg.admin = ctx.accounts.admin.key();
        cfg.mint = ctx.accounts.mint.key();
        cfg.vault = ctx.accounts.vault.key();
        cfg.billing_signer = ctx.accounts.billing_signer.key();
        cfg.tge_unix = tge_unix;
        cfg.minted_total = 0;
        cfg.epoch_counter = 0;
        cfg.bump = ctx.bumps.config;
        cfg.vault_bump = ctx.bumps.vault_authority;
        Ok(())
    }

    /// Compute the annual emission for the elapsed seconds since TGE, capped by the halving
    /// schedule. Pure-on-chain view function callable from clients via `simulate`.
    pub fn current_annual_emission(ctx: Context<ViewCtx>) -> Result<u64> {
        let cfg = &ctx.accounts.config;
        let now = Clock::get()?.unix_timestamp;
        Ok(annual_emission_for(now, cfg.tge_unix))
    }

    /// Mint up to `total_reward` $GRID for `epoch_id`, into the program-owned vault, then
    /// emit an EpochClaim record. The billing signer attests that the epoch's accounting
    /// (per-provider shares) is finalized off-chain and ready to be distributed.
    ///
    /// On-chain checks: amount ≤ budget(epoch_window), epoch monotonic, billing signer matches.
    pub fn claim_epoch(
        ctx: Context<ClaimEpoch>,
        epoch_id: u64,
        epoch_start_unix: i64,
        epoch_end_unix: i64,
        total_reward: u64,
    ) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        require!(epoch_end_unix <= now, EmissionError::EpochInFuture);
        require!(
            epoch_end_unix > epoch_start_unix,
            EmissionError::InvalidEpochRange
        );
        require!(
            epoch_id == ctx.accounts.config.epoch_counter + 1,
            EmissionError::EpochOutOfOrder
        );

        // Compute budget = ∫ annual_emission(t) dt over [start, end], discretised by halving.
        let budget = budget_for_window(
            epoch_start_unix,
            epoch_end_unix,
            ctx.accounts.config.tge_unix,
        );
        require!(total_reward <= budget, EmissionError::BudgetExceeded);

        // CPI: mint to vault.
        let mint_key = ctx.accounts.mint.key();
        let seeds: &[&[u8]] = &[
            b"emission-vault-authority",
            mint_key.as_ref(),
            &[ctx.accounts.config.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[seeds];
        let cpi_ctx = CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            MintTo {
                mint: ctx.accounts.mint.to_account_info(),
                to: ctx.accounts.vault.to_account_info(),
                authority: ctx.accounts.vault_authority.to_account_info(),
            },
            signer_seeds,
        );
        token_2022::mint_to(cpi_ctx, total_reward)?;

        // Record epoch.
        let claim = &mut ctx.accounts.epoch_claim;
        claim.epoch_id = epoch_id;
        claim.epoch_start_unix = epoch_start_unix;
        claim.epoch_end_unix = epoch_end_unix;
        claim.total_reward = total_reward;
        claim.distributed = 0;
        claim.finalized = false;
        claim.bump = ctx.bumps.epoch_claim;

        let cfg = &mut ctx.accounts.config;
        cfg.epoch_counter = epoch_id;
        cfg.minted_total = cfg
            .minted_total
            .checked_add(total_reward)
            .ok_or(EmissionError::Overflow)?;

        emit!(EpochClaimed {
            epoch_id,
            total_reward,
            budget,
        });
        Ok(())
    }

    /// Distribute `amount` from the vault to a single recipient ATA. Called repeatedly by
    /// billing-svc until the epoch's `total_reward` is fully distributed. Recipient is
    /// typically a vesting-program PDA.
    pub fn distribute_to(ctx: Context<DistributeTo>, epoch_id: u64, amount: u64) -> Result<()> {
        require!(epoch_id == ctx.accounts.epoch_claim.epoch_id, EmissionError::EpochMismatch);
        require!(
            !ctx.accounts.epoch_claim.finalized,
            EmissionError::EpochFinalized
        );

        let new_dist = ctx
            .accounts
            .epoch_claim
            .distributed
            .checked_add(amount)
            .ok_or(EmissionError::Overflow)?;
        require!(
            new_dist <= ctx.accounts.epoch_claim.total_reward,
            EmissionError::OverDistribution
        );

        let mint_key = ctx.accounts.mint.key();
        let seeds: &[&[u8]] = &[
            b"emission-vault-authority",
            mint_key.as_ref(),
            &[ctx.accounts.config.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[seeds];
        let cpi_ctx = CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            Transfer2022 {
                from: ctx.accounts.vault.to_account_info(),
                to: ctx.accounts.recipient.to_account_info(),
                authority: ctx.accounts.vault_authority.to_account_info(),
            },
            signer_seeds,
        );
        #[allow(deprecated)]
        token_2022::transfer(cpi_ctx, amount)?;

        ctx.accounts.epoch_claim.distributed = new_dist;
        emit!(EpochDistributed {
            epoch_id,
            recipient: ctx.accounts.recipient.key(),
            amount,
            distributed: new_dist,
        });
        Ok(())
    }

    /// Mark the epoch as finalized — no further distributions allowed.
    pub fn finalize_epoch(ctx: Context<FinalizeEpoch>) -> Result<()> {
        let claim = &mut ctx.accounts.epoch_claim;
        require!(!claim.finalized, EmissionError::EpochFinalized);
        claim.finalized = true;
        emit!(EpochFinalizedEv {
            epoch_id: claim.epoch_id,
            distributed: claim.distributed,
            remainder: claim.total_reward - claim.distributed,
        });
        Ok(())
    }
}

// -- Curve helpers ----------------------------------------------------------------------------

/// Compute annual emission rate (raw units / year) at a given absolute timestamp.
/// Halves every `HALVING_PERIOD_SECS`. After `MAX_HALVINGS` halvings, returns 0.
pub fn annual_emission_for(now: i64, tge_unix: i64) -> u64 {
    if now < tge_unix {
        return 0;
    }
    let elapsed = now - tge_unix;
    let halvings = (elapsed / HALVING_PERIOD_SECS) as u32;
    if halvings >= MAX_HALVINGS {
        return 0;
    }
    YEAR1_EMISSION_RAW >> halvings
}

/// Budget for `[start, end]` window: integrates annual emission across halving boundaries.
/// Result is in raw token units. Returns 0 if start is before TGE.
pub fn budget_for_window(start: i64, end: i64, tge_unix: i64) -> u64 {
    if end <= tge_unix || start >= end {
        return 0;
    }
    let lo = start.max(tge_unix);
    let hi = end;
    let mut total: u128 = 0;
    let mut t = lo;
    while t < hi {
        // current halving slice
        let elapsed = t - tge_unix;
        let h = (elapsed / HALVING_PERIOD_SECS) as u32;
        if h >= MAX_HALVINGS {
            break;
        }
        let slice_end = tge_unix + ((h as i64 + 1) * HALVING_PERIOD_SECS);
        let chunk_end = slice_end.min(hi);
        let dt = (chunk_end - t) as u128;
        let annual = (YEAR1_EMISSION_RAW >> h) as u128;
        // emission per second = annual / seconds-per-year
        let secs_per_year: u128 = 365 * 86_400;
        total = total.saturating_add(annual.saturating_mul(dt) / secs_per_year);
        t = chunk_end;
    }
    total.min(u64::MAX as u128) as u64
}

// -- Accounts ---------------------------------------------------------------------------------

#[derive(Accounts)]
pub struct InitializeEmission<'info> {
    #[account(
        init,
        payer = admin,
        space = 8 + EmissionConfig::INIT_SPACE,
        seeds = [b"emission-config", mint.key().as_ref()],
        bump
    )]
    pub config: Account<'info, EmissionConfig>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(
        mut,
        token::mint = mint,
        token::authority = vault_authority,
        token::token_program = token_program,
    )]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA, signs for vault outflows.
    #[account(
        seeds = [b"emission-vault-authority", mint.key().as_ref()],
        bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    /// CHECK: just stored as a key, used to validate `claim_epoch` signer.
    pub billing_signer: UncheckedAccount<'info>,
    #[account(mut)]
    pub admin: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct ViewCtx<'info> {
    #[account(seeds = [b"emission-config", config.mint.as_ref()], bump = config.bump)]
    pub config: Account<'info, EmissionConfig>,
}

#[derive(Accounts)]
#[instruction(epoch_id: u64)]
pub struct ClaimEpoch<'info> {
    #[account(
        mut,
        seeds = [b"emission-config", mint.key().as_ref()],
        bump = config.bump,
        has_one = mint,
        has_one = vault,
        has_one = billing_signer,
    )]
    pub config: Account<'info, EmissionConfig>,
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA used as vault authority signer for MintTo CPI.
    #[account(
        seeds = [b"emission-vault-authority", mint.key().as_ref()],
        bump = config.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    pub billing_signer: Signer<'info>,
    #[account(
        init,
        payer = payer,
        space = 8 + EpochClaim::INIT_SPACE,
        seeds = [b"emission-epoch", mint.key().as_ref(), &epoch_id.to_le_bytes()],
        bump
    )]
    pub epoch_claim: Account<'info, EpochClaim>,
    #[account(mut)]
    pub payer: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
#[instruction(epoch_id: u64)]
pub struct DistributeTo<'info> {
    #[account(
        seeds = [b"emission-config", mint.key().as_ref()],
        bump = config.bump,
        has_one = mint,
        has_one = vault,
        has_one = billing_signer,
    )]
    pub config: Account<'info, EmissionConfig>,
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA used as vault authority signer for Transfer CPI.
    #[account(
        seeds = [b"emission-vault-authority", mint.key().as_ref()],
        bump = config.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    #[account(
        mut,
        seeds = [b"emission-epoch", mint.key().as_ref(), &epoch_id.to_le_bytes()],
        bump = epoch_claim.bump
    )]
    pub epoch_claim: Account<'info, EpochClaim>,
    pub billing_signer: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct FinalizeEpoch<'info> {
    #[account(
        seeds = [b"emission-config", config.mint.as_ref()],
        bump = config.bump,
        has_one = billing_signer
    )]
    pub config: Account<'info, EmissionConfig>,
    #[account(
        mut,
        seeds = [b"emission-epoch", config.mint.as_ref(), &epoch_claim.epoch_id.to_le_bytes()],
        bump = epoch_claim.bump
    )]
    pub epoch_claim: Account<'info, EpochClaim>,
    pub billing_signer: Signer<'info>,
}

// -- State ------------------------------------------------------------------------------------

#[account]
#[derive(InitSpace)]
pub struct EmissionConfig {
    pub admin: Pubkey,
    pub mint: Pubkey,
    pub vault: Pubkey,
    pub billing_signer: Pubkey,
    pub tge_unix: i64,
    pub epoch_counter: u64,
    pub minted_total: u64,
    pub bump: u8,
    pub vault_bump: u8,
}

#[account]
#[derive(InitSpace)]
pub struct EpochClaim {
    pub epoch_id: u64,
    pub epoch_start_unix: i64,
    pub epoch_end_unix: i64,
    pub total_reward: u64,
    pub distributed: u64,
    pub finalized: bool,
    pub bump: u8,
}

// -- Events -----------------------------------------------------------------------------------

#[event]
pub struct EpochClaimed {
    pub epoch_id: u64,
    pub total_reward: u64,
    pub budget: u64,
}

#[event]
pub struct EpochDistributed {
    pub epoch_id: u64,
    pub recipient: Pubkey,
    pub amount: u64,
    pub distributed: u64,
}

#[event]
pub struct EpochFinalizedEv {
    pub epoch_id: u64,
    pub distributed: u64,
    pub remainder: u64,
}

// -- Errors -----------------------------------------------------------------------------------

#[error_code]
pub enum EmissionError {
    #[msg("Numeric overflow")]
    Overflow,
    #[msg("Epoch end is in the future")]
    EpochInFuture,
    #[msg("Invalid epoch range (end <= start)")]
    InvalidEpochRange,
    #[msg("Epoch counter is not monotonic")]
    EpochOutOfOrder,
    #[msg("Requested reward exceeds halving-curve budget for the window")]
    BudgetExceeded,
    #[msg("Epoch id does not match the supplied EpochClaim account")]
    EpochMismatch,
    #[msg("Epoch has been finalized; no further distributions allowed")]
    EpochFinalized,
    #[msg("Distribution would exceed the epoch's total_reward")]
    OverDistribution,
}
