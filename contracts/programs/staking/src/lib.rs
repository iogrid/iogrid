//! $GRID Staking — routing-priority weight + customer-discount stake.
//!
//! Two stake kinds (TOKENOMICS.md Layer 3):
//!  - `Provider`: stakes count toward routing-priority weight. Compatible with "stake-while-
//!    locked" (locked vesting balances also count toward weight, but THAT calculation lives in
//!    the off-chain coordinator; this program only counts physically-staked tokens). Minimum
//!    stake duration: 30 days.
//!  - `Customer`: stakes earn volume discounts (up to 25% off list price). Minimum 30 days.
//!
//! Yield accrual: a per-stake-account counter is incremented on each `accrue_yield` call by
//! the configured rate; clients can then `claim_yield` to mint rewards. (Reward mint is
//! authorised to a separate PDA distinct from the emission program — keeps staking yield off
//! the halving schedule.)

use anchor_lang::prelude::*;
use anchor_spl::token_2022::{self, MintTo, Token2022, Transfer as Transfer2022};
use anchor_spl::token_interface::{Mint, TokenAccount};

declare_id!("GR1Dstakingggggggggggggggggggggggggggggggggg");

pub const DAY_SECS: i64 = 86_400;
pub const MIN_STAKE_SECS: i64 = 30 * DAY_SECS;

/// Default annual yield in bps (1.00% = 100 bps). Configurable per pool.
pub const DEFAULT_ANNUAL_YIELD_BPS: u16 = 500; // 5.00%

/// Routing-priority weight ramps from 1.0× (10_000 bps) at MIN_STAKE_SECS up to MAX_WEIGHT_BPS
/// after 2 years past minimum.
pub const MAX_WEIGHT_BPS: u16 = 20_000;

/// Customer-discount voucher bps bounds (5%–25% per TOKENOMICS.md).
pub const MIN_DISCOUNT_BPS: u16 = 500;
pub const MAX_DISCOUNT_BPS: u16 = 2_500;

#[program]
pub mod staking {
    use super::*;

    /// Bootstrap the staking pool config for a given mint.
    pub fn initialize_pool(
        ctx: Context<InitializePool>,
        annual_yield_bps: u16,
    ) -> Result<()> {
        let p = &mut ctx.accounts.pool;
        p.mint = ctx.accounts.mint.key();
        p.admin = ctx.accounts.admin.key();
        p.stake_vault = ctx.accounts.stake_vault.key();
        p.reward_vault = ctx.accounts.reward_vault.key();
        p.total_provider_staked = 0;
        p.total_customer_staked = 0;
        p.annual_yield_bps = annual_yield_bps;
        p.bump = ctx.bumps.pool;
        p.vault_bump = ctx.bumps.vault_authority;
        Ok(())
    }

    /// Open a stake position. Transfers `amount` from the staker's ATA into the pool vault.
    pub fn stake(
        ctx: Context<Stake>,
        amount: u64,
        kind: StakeKind,
    ) -> Result<()> {
        require!(amount > 0, StakingError::ZeroAmount);
        let now = Clock::get()?.unix_timestamp;
        let pos = &mut ctx.accounts.position;
        pos.pool = ctx.accounts.pool.key();
        pos.owner = ctx.accounts.staker.key();
        pos.amount = amount;
        pos.kind = kind;
        pos.staked_at = now;
        pos.last_accrual = now;
        pos.unclaimed_yield = 0;
        pos.bump = ctx.bumps.position;

        // Transfer staker → stake_vault
        let cpi_ctx = CpiContext::new(
            ctx.accounts.token_program.to_account_info(),
            Transfer2022 {
                from: ctx.accounts.staker_ata.to_account_info(),
                to: ctx.accounts.stake_vault.to_account_info(),
                authority: ctx.accounts.staker.to_account_info(),
            },
        );
        #[allow(deprecated)]
        token_2022::transfer(cpi_ctx, amount)?;

        let pool = &mut ctx.accounts.pool;
        match kind {
            StakeKind::Provider => {
                pool.total_provider_staked = pool
                    .total_provider_staked
                    .checked_add(amount)
                    .ok_or(StakingError::Overflow)?;
            }
            StakeKind::Customer => {
                pool.total_customer_staked = pool
                    .total_customer_staked
                    .checked_add(amount)
                    .ok_or(StakingError::Overflow)?;
            }
        }

        emit!(Staked {
            staker: ctx.accounts.staker.key(),
            amount,
            kind,
        });
        Ok(())
    }

    /// Accrue yield up to `now`. Anyone can call this (e.g., a keeper) to roll forward the
    /// counter. Yield = amount * annual_bps * elapsed / (10_000 * seconds_per_year).
    pub fn accrue_yield(ctx: Context<AccrueYield>) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let pos = &mut ctx.accounts.position;
        require!(now > pos.last_accrual, StakingError::NoTimeElapsed);
        let dt = (now - pos.last_accrual) as u128;
        let annual = ctx.accounts.pool.annual_yield_bps as u128;
        let secs_per_year: u128 = 365 * 86_400;
        let increment = (pos.amount as u128 * annual * dt) / (10_000u128 * secs_per_year);
        pos.unclaimed_yield = pos
            .unclaimed_yield
            .checked_add(increment as u64)
            .ok_or(StakingError::Overflow)?;
        pos.last_accrual = now;
        emit!(Accrued {
            position: pos.key(),
            new_unclaimed: pos.unclaimed_yield,
        });
        Ok(())
    }

    /// Claim accrued yield to the staker's ATA. The reward vault PDA signs.
    pub fn claim_yield(ctx: Context<ClaimYield>) -> Result<()> {
        let pos = &mut ctx.accounts.position;
        let amount = pos.unclaimed_yield;
        require!(amount > 0, StakingError::NothingToClaim);

        let mint_key = ctx.accounts.mint.key();
        let seeds: &[&[u8]] = &[
            b"staking-vault-authority",
            mint_key.as_ref(),
            &[ctx.accounts.pool.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[seeds];
        // Mint rewards directly into the staker's ATA — staking pool is the reward mint
        // authority for an ancillary "rewards stream" allocation.
        let cpi_ctx = CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            MintTo {
                mint: ctx.accounts.mint.to_account_info(),
                to: ctx.accounts.recipient.to_account_info(),
                authority: ctx.accounts.vault_authority.to_account_info(),
            },
            signer_seeds,
        );
        token_2022::mint_to(cpi_ctx, amount)?;

        pos.unclaimed_yield = 0;
        emit!(YieldClaimed {
            staker: pos.owner,
            amount,
        });
        Ok(())
    }

    /// Pure-view: compute the routing-priority weight for a provider's currently-active stake
    /// position. The weight model is `amount * time_remaining_factor`, where
    /// `time_remaining_factor` is a bps value in `[10_000, MAX_WEIGHT_BPS]` derived from how
    /// long the staker is past the 30-day minimum:
    ///   - At MIN_STAKE_SECS exactly: 1.00× (10_000 bps)
    ///   - At MIN_STAKE_SECS + 365d:  ~1.50× (15_000 bps) — i.e., 50% boost per year held
    ///   - Capped at 2.00× (20_000 bps) at 2 years held
    ///
    /// Returned weight is in raw token units (same precision as `amount`).
    ///
    /// Consumed by the off-chain workloads-svc scheduler when ranking providers for routing.
    pub fn compute_weight(ctx: Context<ComputeWeight>) -> Result<u64> {
        let now = Clock::get()?.unix_timestamp;
        let pos = &ctx.accounts.position;
        if matches!(pos.kind, StakeKind::Customer) {
            // Customer stakes don't contribute to routing-priority weight.
            return Ok(0);
        }
        let elapsed = now.saturating_sub(pos.staked_at);
        if elapsed < MIN_STAKE_SECS {
            // Position has not yet matured past the minimum stake duration.
            return Ok(0);
        }
        // Linear ramp: +5_000 bps over 730 days past min-stake, capped at MAX_WEIGHT_BPS.
        let two_years_secs: u128 = (2 * 365 * DAY_SECS) as u128;
        let bonus_window: u128 = (elapsed - MIN_STAKE_SECS).min(2 * 365 * DAY_SECS) as u128;
        let bonus_bps =
            (bonus_window * (MAX_WEIGHT_BPS as u128 - 10_000u128)) / two_years_secs;
        let weight_bps = (10_000u128 + bonus_bps).min(MAX_WEIGHT_BPS as u128);
        let weight = (pos.amount as u128 * weight_bps) / 10_000u128;
        Ok(weight.min(u64::MAX as u128) as u64)
    }

    /// Customer-side: open a discount voucher backed by a stake. The customer locks
    /// `amount` $GRID for at least `lock_seconds` (min 30 days). The resulting
    /// `DiscountVoucher` PDA records:
    ///   - principal staked
    ///   - lock start / end
    ///   - implied discount bps (linear from 5% at 30d → 25% at 365d, capped at 2_500 bps)
    /// The off-chain billing-svc reads the voucher at invoice time and applies the discount.
    ///
    /// Unlike `stake`, the voucher PDA seed is `["discount-voucher", pool, owner, voucher_id]`,
    /// so a customer can hold multiple concurrent vouchers.
    pub fn customer_stake_for_discount(
        ctx: Context<CustomerStakeForDiscount>,
        voucher_id: u64,
        amount: u64,
        lock_seconds: i64,
    ) -> Result<()> {
        require!(amount > 0, StakingError::ZeroAmount);
        require!(
            lock_seconds >= MIN_STAKE_SECS,
            StakingError::MinStakeNotMet
        );
        let now = Clock::get()?.unix_timestamp;
        let lock_end = now
            .checked_add(lock_seconds)
            .ok_or(StakingError::Overflow)?;

        // Linear discount ramp:
        //   30d → 500 bps (5% off)
        //   365d → 2_500 bps (25% off — TOKENOMICS cap)
        // Linear interpolation: bps = 500 + (lock-30d) / (365d-30d) * (2500-500)
        let one_year = 365 * DAY_SECS;
        let lock_capped = lock_seconds.min(one_year);
        let over_min = (lock_capped - MIN_STAKE_SECS) as i128; // ≥0 by check
        let ramp_range = (one_year - MIN_STAKE_SECS) as i128;
        let extra_bps = if ramp_range > 0 {
            (over_min * (MAX_DISCOUNT_BPS as i128 - MIN_DISCOUNT_BPS as i128)) / ramp_range
        } else {
            0
        };
        let discount_bps = (MIN_DISCOUNT_BPS as i128 + extra_bps)
            .min(MAX_DISCOUNT_BPS as i128) as u16;

        let v = &mut ctx.accounts.voucher;
        v.pool = ctx.accounts.pool.key();
        v.owner = ctx.accounts.staker.key();
        v.voucher_id = voucher_id;
        v.amount = amount;
        v.locked_at = now;
        v.lock_end = lock_end;
        v.discount_bps = discount_bps;
        v.consumed = false;
        v.bump = ctx.bumps.voucher;

        // Transfer staker → stake_vault (same pooled vault; voucher accounts segregate ownership)
        let cpi_ctx = CpiContext::new(
            ctx.accounts.token_program.to_account_info(),
            Transfer2022 {
                from: ctx.accounts.staker_ata.to_account_info(),
                to: ctx.accounts.stake_vault.to_account_info(),
                authority: ctx.accounts.staker.to_account_info(),
            },
        );
        #[allow(deprecated)]
        token_2022::transfer(cpi_ctx, amount)?;

        let pool = &mut ctx.accounts.pool;
        pool.total_customer_staked = pool
            .total_customer_staked
            .checked_add(amount)
            .ok_or(StakingError::Overflow)?;

        emit!(DiscountVoucherIssued {
            owner: v.owner,
            voucher_id,
            amount,
            lock_end,
            discount_bps,
        });
        Ok(())
    }

    /// Redeem the voucher after `lock_end`. Returns principal to the staker's ATA and closes
    /// the voucher PDA. The discount itself is applied off-chain at billing time; this just
    /// returns the underlying stake once the lockup matures.
    pub fn redeem_discount_voucher(ctx: Context<RedeemDiscountVoucher>) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let voucher = &ctx.accounts.voucher;
        require!(now >= voucher.lock_end, StakingError::MinStakeNotMet);
        require!(!voucher.consumed, StakingError::AlreadyConsumed);
        let amount = voucher.amount;

        let mint_key = ctx.accounts.mint.key();
        let seeds: &[&[u8]] = &[
            b"staking-vault-authority",
            mint_key.as_ref(),
            &[ctx.accounts.pool.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[seeds];
        let cpi_ctx = CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            Transfer2022 {
                from: ctx.accounts.stake_vault.to_account_info(),
                to: ctx.accounts.recipient.to_account_info(),
                authority: ctx.accounts.vault_authority.to_account_info(),
            },
            signer_seeds,
        );
        #[allow(deprecated)]
        token_2022::transfer(cpi_ctx, amount)?;

        let pool = &mut ctx.accounts.pool;
        pool.total_customer_staked = pool
            .total_customer_staked
            .checked_sub(amount)
            .ok_or(StakingError::Overflow)?;

        emit!(DiscountVoucherRedeemed {
            owner: ctx.accounts.staker.key(),
            voucher_id: voucher.voucher_id,
            amount,
        });
        Ok(())
    }

    /// Close the position and return staked principal. Reverts before MIN_STAKE_SECS.
    pub fn unstake(ctx: Context<Unstake>) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let pos = &ctx.accounts.position;
        require!(
            now - pos.staked_at >= MIN_STAKE_SECS,
            StakingError::MinStakeNotMet
        );
        let amount = pos.amount;
        require!(amount > 0, StakingError::ZeroAmount);

        let mint_key = ctx.accounts.mint.key();
        let seeds: &[&[u8]] = &[
            b"staking-vault-authority",
            mint_key.as_ref(),
            &[ctx.accounts.pool.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[seeds];
        let cpi_ctx = CpiContext::new_with_signer(
            ctx.accounts.token_program.to_account_info(),
            Transfer2022 {
                from: ctx.accounts.stake_vault.to_account_info(),
                to: ctx.accounts.recipient.to_account_info(),
                authority: ctx.accounts.vault_authority.to_account_info(),
            },
            signer_seeds,
        );
        #[allow(deprecated)]
        token_2022::transfer(cpi_ctx, amount)?;

        let kind = ctx.accounts.position.kind;
        let pool = &mut ctx.accounts.pool;
        match kind {
            StakeKind::Provider => {
                pool.total_provider_staked = pool
                    .total_provider_staked
                    .checked_sub(amount)
                    .ok_or(StakingError::Overflow)?;
            }
            StakeKind::Customer => {
                pool.total_customer_staked = pool
                    .total_customer_staked
                    .checked_sub(amount)
                    .ok_or(StakingError::Overflow)?;
            }
        }

        emit!(Unstaked {
            staker: pos.owner,
            amount,
            kind,
        });
        Ok(())
    }
}

// -- Accounts ---------------------------------------------------------------------------------

#[derive(Accounts)]
pub struct InitializePool<'info> {
    #[account(
        init,
        payer = admin,
        space = 8 + StakingPool::INIT_SPACE,
        seeds = [b"staking-pool", mint.key().as_ref()],
        bump
    )]
    pub pool: Account<'info, StakingPool>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(
        init,
        payer = admin,
        token::mint = mint,
        token::authority = vault_authority,
        token::token_program = token_program,
        seeds = [b"staking-stake-vault", mint.key().as_ref()],
        bump
    )]
    pub stake_vault: InterfaceAccount<'info, TokenAccount>,
    #[account(
        init,
        payer = admin,
        token::mint = mint,
        token::authority = vault_authority,
        token::token_program = token_program,
        seeds = [b"staking-reward-vault", mint.key().as_ref()],
        bump
    )]
    pub reward_vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA, vault authority.
    #[account(
        seeds = [b"staking-vault-authority", mint.key().as_ref()],
        bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(mut)]
    pub admin: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
    pub rent: Sysvar<'info, Rent>,
}

#[derive(Accounts)]
#[instruction(amount: u64, kind: StakeKind)]
pub struct Stake<'info> {
    #[account(
        mut,
        seeds = [b"staking-pool", mint.key().as_ref()],
        bump = pool.bump,
        has_one = mint,
        has_one = stake_vault,
    )]
    pub pool: Account<'info, StakingPool>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub stake_vault: InterfaceAccount<'info, TokenAccount>,
    #[account(mut)]
    pub staker_ata: InterfaceAccount<'info, TokenAccount>,
    #[account(
        init,
        payer = staker,
        space = 8 + StakePosition::INIT_SPACE,
        seeds = [b"stake-position", pool.key().as_ref(), staker.key().as_ref()],
        bump
    )]
    pub position: Account<'info, StakePosition>,
    #[account(mut)]
    pub staker: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct AccrueYield<'info> {
    #[account(
        seeds = [b"staking-pool", pool.mint.as_ref()],
        bump = pool.bump
    )]
    pub pool: Account<'info, StakingPool>,
    #[account(
        mut,
        seeds = [b"stake-position", pool.key().as_ref(), position.owner.as_ref()],
        bump = position.bump,
        constraint = position.pool == pool.key() @ StakingError::PoolMismatch
    )]
    pub position: Account<'info, StakePosition>,
}

#[derive(Accounts)]
pub struct ClaimYield<'info> {
    #[account(
        seeds = [b"staking-pool", mint.key().as_ref()],
        bump = pool.bump,
        has_one = mint,
    )]
    pub pool: Account<'info, StakingPool>,
    #[account(
        mut,
        seeds = [b"stake-position", pool.key().as_ref(), staker.key().as_ref()],
        bump = position.bump,
        constraint = position.owner == staker.key() @ StakingError::OwnerMismatch
    )]
    pub position: Account<'info, StakePosition>,
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA, vault authority signs MintTo CPI.
    #[account(
        seeds = [b"staking-vault-authority", mint.key().as_ref()],
        bump = pool.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    pub staker: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct Unstake<'info> {
    #[account(
        mut,
        seeds = [b"staking-pool", mint.key().as_ref()],
        bump = pool.bump,
        has_one = mint,
        has_one = stake_vault,
    )]
    pub pool: Account<'info, StakingPool>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(
        mut,
        seeds = [b"stake-position", pool.key().as_ref(), staker.key().as_ref()],
        bump = position.bump,
        close = staker,
        constraint = position.owner == staker.key() @ StakingError::OwnerMismatch
    )]
    pub position: Account<'info, StakePosition>,
    #[account(mut)]
    pub stake_vault: InterfaceAccount<'info, TokenAccount>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA, vault authority signs Transfer CPI.
    #[account(
        seeds = [b"staking-vault-authority", mint.key().as_ref()],
        bump = pool.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(mut)]
    pub staker: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct ComputeWeight<'info> {
    #[account(
        seeds = [b"staking-pool", pool.mint.as_ref()],
        bump = pool.bump
    )]
    pub pool: Account<'info, StakingPool>,
    #[account(
        seeds = [b"stake-position", pool.key().as_ref(), position.owner.as_ref()],
        bump = position.bump,
        constraint = position.pool == pool.key() @ StakingError::PoolMismatch
    )]
    pub position: Account<'info, StakePosition>,
}

#[derive(Accounts)]
#[instruction(voucher_id: u64)]
pub struct CustomerStakeForDiscount<'info> {
    #[account(
        mut,
        seeds = [b"staking-pool", mint.key().as_ref()],
        bump = pool.bump,
        has_one = mint,
        has_one = stake_vault,
    )]
    pub pool: Account<'info, StakingPool>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub stake_vault: InterfaceAccount<'info, TokenAccount>,
    #[account(mut)]
    pub staker_ata: InterfaceAccount<'info, TokenAccount>,
    #[account(
        init,
        payer = staker,
        space = 8 + DiscountVoucher::INIT_SPACE,
        seeds = [
            b"discount-voucher",
            pool.key().as_ref(),
            staker.key().as_ref(),
            &voucher_id.to_le_bytes()
        ],
        bump
    )]
    pub voucher: Account<'info, DiscountVoucher>,
    #[account(mut)]
    pub staker: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct RedeemDiscountVoucher<'info> {
    #[account(
        mut,
        seeds = [b"staking-pool", mint.key().as_ref()],
        bump = pool.bump,
        has_one = mint,
        has_one = stake_vault,
    )]
    pub pool: Account<'info, StakingPool>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub stake_vault: InterfaceAccount<'info, TokenAccount>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA, vault authority signs Transfer CPI.
    #[account(
        seeds = [b"staking-vault-authority", mint.key().as_ref()],
        bump = pool.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(
        mut,
        close = staker,
        seeds = [
            b"discount-voucher",
            pool.key().as_ref(),
            staker.key().as_ref(),
            &voucher.voucher_id.to_le_bytes()
        ],
        bump = voucher.bump,
        has_one = pool,
        constraint = voucher.owner == staker.key() @ StakingError::OwnerMismatch,
    )]
    pub voucher: Account<'info, DiscountVoucher>,
    #[account(mut)]
    pub staker: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

// -- State ------------------------------------------------------------------------------------

#[account]
#[derive(InitSpace)]
pub struct StakingPool {
    pub mint: Pubkey,
    pub admin: Pubkey,
    pub stake_vault: Pubkey,
    pub reward_vault: Pubkey,
    pub total_provider_staked: u64,
    pub total_customer_staked: u64,
    pub annual_yield_bps: u16,
    pub bump: u8,
    pub vault_bump: u8,
}

#[account]
#[derive(InitSpace)]
pub struct StakePosition {
    pub pool: Pubkey,
    pub owner: Pubkey,
    pub amount: u64,
    pub kind: StakeKind,
    pub staked_at: i64,
    pub last_accrual: i64,
    pub unclaimed_yield: u64,
    pub bump: u8,
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, Debug, PartialEq, Eq, InitSpace)]
pub enum StakeKind {
    Provider,
    Customer,
}

#[account]
#[derive(InitSpace)]
pub struct DiscountVoucher {
    pub pool: Pubkey,
    pub owner: Pubkey,
    pub voucher_id: u64,
    pub amount: u64,
    pub locked_at: i64,
    pub lock_end: i64,
    pub discount_bps: u16,
    pub consumed: bool,
    pub bump: u8,
}

// -- Events -----------------------------------------------------------------------------------

#[event]
pub struct Staked {
    pub staker: Pubkey,
    pub amount: u64,
    pub kind: StakeKind,
}

#[event]
pub struct Unstaked {
    pub staker: Pubkey,
    pub amount: u64,
    pub kind: StakeKind,
}

#[event]
pub struct Accrued {
    pub position: Pubkey,
    pub new_unclaimed: u64,
}

#[event]
pub struct YieldClaimed {
    pub staker: Pubkey,
    pub amount: u64,
}

#[event]
pub struct DiscountVoucherIssued {
    pub owner: Pubkey,
    pub voucher_id: u64,
    pub amount: u64,
    pub lock_end: i64,
    pub discount_bps: u16,
}

#[event]
pub struct DiscountVoucherRedeemed {
    pub owner: Pubkey,
    pub voucher_id: u64,
    pub amount: u64,
}

// -- Errors -----------------------------------------------------------------------------------

#[error_code]
pub enum StakingError {
    #[msg("Numeric overflow")]
    Overflow,
    #[msg("Cannot stake/unstake zero")]
    ZeroAmount,
    #[msg("30-day minimum stake period not yet elapsed")]
    MinStakeNotMet,
    #[msg("Nothing to claim")]
    NothingToClaim,
    #[msg("No time has elapsed since last accrual")]
    NoTimeElapsed,
    #[msg("Pool/position mismatch")]
    PoolMismatch,
    #[msg("Position owner does not match signer")]
    OwnerMismatch,
    #[msg("Voucher has already been consumed/redeemed")]
    AlreadyConsumed,
}
