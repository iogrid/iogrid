//! $GRID Vesting — mandatory provider-earnings lockup (Layer 3 of TOKENOMICS.md).
//!
//! Behaviour per docs:
//!  - Every $GRID earned by a provider is auto-locked at distribution.
//!  - Base tier: 30-day cliff + 60-day linear vest (= 90 days total).
//!  - Tier multipliers — applied at the EMISSION step (not here); this program just enforces
//!    whatever cliff+linear schedule the provider chose at onboarding.
//!  - Rolling per payout: each deposit starts its own clock.
//!  - Early-unlock allowed once per 12 months per provider, with **50% burn penalty** on the
//!    still-locked portion.
//!  - Stake-while-locked: the staking program can read this account's `locked_amount` to
//!    compute routing-priority weight; tokens never physically move (vesting vault retains).
//!
//! Tiers (cliff_secs / linear_secs / multiplier_bps):
//!  - Standard:   30d / 60d  / 10000 (1.00×)
//!  - Loyalty:    90d / 180d / 12500 (1.25×)
//!  - Conviction: 180d / 365d / 15000 (1.50×)
//!  - Maximum:    365d / 730d / 20000 (2.00×)
//!
//! On-chain state:
//!  - `ProviderVesting`: per-(mint, provider) PDA; aggregates per-deposit schedules.
//!  - `VestingDeposit`: per-deposit PDA (one per emission distribution).
//!
//! NOTE: rather than storing every deposit inline (which would blow account size), we store
//! aggregate counters on `ProviderVesting` and require the client to enumerate deposits as
//! separate PDAs. `withdraw_unlocked` requires the specific deposit PDA the client wants to
//! draw from.

use anchor_lang::prelude::*;
use anchor_spl::token_2022::{self, Burn, Token2022, Transfer as Transfer2022};
use anchor_spl::token_interface::{Mint, TokenAccount};

declare_id!("GR1Dvestingggggggggggggggggggggggggggggggggg");

pub const DAY_SECS: i64 = 86_400;

/// 12 months in seconds — minimum time between early-unlock events per provider.
pub const EARLY_UNLOCK_COOLDOWN_SECS: i64 = 365 * DAY_SECS;

/// 50% penalty (in bps, 10000 = 100%).
pub const EARLY_UNLOCK_PENALTY_BPS: u16 = 5_000;

#[program]
pub mod vesting {
    use super::*;

    /// Initialize a provider's vesting profile (tier + vault).
    pub fn initialize_provider(
        ctx: Context<InitializeProvider>,
        tier: VestTier,
    ) -> Result<()> {
        let v = &mut ctx.accounts.provider_vesting;
        v.mint = ctx.accounts.mint.key();
        v.provider = ctx.accounts.provider.key();
        v.vault = ctx.accounts.vault.key();
        v.tier = tier;
        v.total_deposited = 0;
        v.total_withdrawn = 0;
        v.total_burned = 0;
        v.deposit_counter = 0;
        v.last_early_unlock = 0;
        v.bump = ctx.bumps.provider_vesting;
        v.vault_bump = ctx.bumps.vault_authority;
        Ok(())
    }

    /// Upgrade tier (lock more, never less). Reverts if `new_tier` is shorter than current.
    pub fn upgrade_tier(ctx: Context<UpgradeTier>, new_tier: VestTier) -> Result<()> {
        let v = &mut ctx.accounts.provider_vesting;
        let cur = v.tier.schedule();
        let new = new_tier.schedule();
        require!(
            new.cliff_secs >= cur.cliff_secs && new.linear_secs >= cur.linear_secs,
            VestingError::CannotDowngradeTier
        );
        v.tier = new_tier;
        emit!(TierUpgraded {
            provider: v.provider,
            new_tier,
        });
        Ok(())
    }

    /// Record an emission deposit. Called by emission program via CPI (or by billing signer
    /// during testing). Each call creates a `VestingDeposit` PDA with its own clock.
    ///
    /// The actual tokens must already be in the vault (transferred from emission vault before
    /// this call) — this function only records the schedule.
    pub fn record_deposit(
        ctx: Context<RecordDeposit>,
        amount: u64,
    ) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let v = &mut ctx.accounts.provider_vesting;
        let deposit_id = v.deposit_counter + 1;

        let d = &mut ctx.accounts.deposit;
        d.provider_vesting = v.key();
        d.deposit_id = deposit_id;
        d.amount = amount;
        d.withdrawn = 0;
        d.deposited_at = now;
        d.tier = v.tier;
        d.bump = ctx.bumps.deposit;

        v.deposit_counter = deposit_id;
        v.total_deposited = v
            .total_deposited
            .checked_add(amount)
            .ok_or(VestingError::Overflow)?;

        emit!(DepositRecorded {
            provider: v.provider,
            deposit_id,
            amount,
            tier: v.tier,
        });
        Ok(())
    }

    /// Withdraw any newly-unlocked portion of a specific deposit to the provider's wallet.
    pub fn withdraw_unlocked(ctx: Context<WithdrawUnlocked>) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let d = &mut ctx.accounts.deposit;
        let unlocked_now = vested_amount(d.amount, d.deposited_at, now, d.tier);
        let claimable = unlocked_now
            .checked_sub(d.withdrawn)
            .ok_or(VestingError::Overflow)?;
        require!(claimable > 0, VestingError::NothingToWithdraw);

        // CPI transfer from vault → recipient
        let mint_key = ctx.accounts.mint.key();
        let provider_key = ctx.accounts.provider_vesting.provider;
        let vault_seeds: &[&[u8]] = &[
            b"vesting-vault-authority",
            mint_key.as_ref(),
            provider_key.as_ref(),
            &[ctx.accounts.provider_vesting.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[vault_seeds];
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
        token_2022::transfer(cpi_ctx, claimable)?;

        d.withdrawn = unlocked_now;
        let v = &mut ctx.accounts.provider_vesting;
        v.total_withdrawn = v
            .total_withdrawn
            .checked_add(claimable)
            .ok_or(VestingError::Overflow)?;

        emit!(Withdrew {
            provider: v.provider,
            deposit_id: d.deposit_id,
            amount: claimable,
        });
        Ok(())
    }

    /// Early unlock: the provider takes the still-locked portion, paying a 50% burn penalty.
    /// Allowed once per `EARLY_UNLOCK_COOLDOWN_SECS`.
    pub fn early_unlock(ctx: Context<EarlyUnlock>) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let v_key = ctx.accounts.provider_vesting.key();
        let v = &mut ctx.accounts.provider_vesting;
        require!(
            now - v.last_early_unlock >= EARLY_UNLOCK_COOLDOWN_SECS,
            VestingError::EarlyUnlockOnCooldown
        );

        let d = &mut ctx.accounts.deposit;
        let unlocked_now = vested_amount(d.amount, d.deposited_at, now, d.tier);
        let locked_remaining = d
            .amount
            .checked_sub(unlocked_now)
            .ok_or(VestingError::Overflow)?;
        require!(locked_remaining > 0, VestingError::NothingToEarlyUnlock);

        let penalty = (locked_remaining as u128 * EARLY_UNLOCK_PENALTY_BPS as u128 / 10_000) as u64;
        let payout = locked_remaining
            .checked_sub(penalty)
            .ok_or(VestingError::Overflow)?;

        let mint_key = ctx.accounts.mint.key();
        let provider_key = v.provider;
        let vault_seeds: &[&[u8]] = &[
            b"vesting-vault-authority",
            mint_key.as_ref(),
            provider_key.as_ref(),
            &[v.vault_bump],
        ];
        let signer_seeds: &[&[&[u8]]] = &[vault_seeds];

        // Burn penalty
        if penalty > 0 {
            let cpi_burn = CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                Burn {
                    mint: ctx.accounts.mint.to_account_info(),
                    from: ctx.accounts.vault.to_account_info(),
                    authority: ctx.accounts.vault_authority.to_account_info(),
                },
                signer_seeds,
            );
            token_2022::burn(cpi_burn, penalty)?;
        }

        // Pay out remaining to provider
        if payout > 0 {
            let cpi_pay = CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                Transfer2022 {
                    from: ctx.accounts.vault.to_account_info(),
                    to: ctx.accounts.recipient.to_account_info(),
                    authority: ctx.accounts.vault_authority.to_account_info(),
                },
                signer_seeds,
            );
            #[allow(deprecated)]
            token_2022::transfer(cpi_pay, payout)?;
        }

        // Mark deposit fully consumed
        d.withdrawn = d.amount;
        v.total_withdrawn = v
            .total_withdrawn
            .checked_add(payout)
            .ok_or(VestingError::Overflow)?;
        v.total_burned = v
            .total_burned
            .checked_add(penalty)
            .ok_or(VestingError::Overflow)?;
        v.last_early_unlock = now;

        emit!(EarlyUnlocked {
            provider: v.provider,
            provider_vesting: v_key,
            deposit_id: d.deposit_id,
            payout,
            burned: penalty,
        });
        Ok(())
    }
}

// -- Curve helper ------------------------------------------------------------------------------

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, Debug, PartialEq, Eq, InitSpace)]
pub enum VestTier {
    Standard,
    Loyalty,
    Conviction,
    Maximum,
}

#[derive(Clone, Copy, Debug)]
pub struct Schedule {
    pub cliff_secs: i64,
    pub linear_secs: i64,
    /// Bps multiplier — emission applies this BEFORE this program ever sees the tokens.
    pub multiplier_bps: u16,
}

impl VestTier {
    pub fn schedule(self) -> Schedule {
        match self {
            VestTier::Standard => Schedule {
                cliff_secs: 30 * DAY_SECS,
                linear_secs: 60 * DAY_SECS,
                multiplier_bps: 10_000,
            },
            VestTier::Loyalty => Schedule {
                cliff_secs: 90 * DAY_SECS,
                linear_secs: 180 * DAY_SECS,
                multiplier_bps: 12_500,
            },
            VestTier::Conviction => Schedule {
                cliff_secs: 180 * DAY_SECS,
                linear_secs: 365 * DAY_SECS,
                multiplier_bps: 15_000,
            },
            VestTier::Maximum => Schedule {
                cliff_secs: 365 * DAY_SECS,
                linear_secs: 730 * DAY_SECS,
                multiplier_bps: 20_000,
            },
        }
    }
}

/// Compute the unlocked (i.e., vested) portion of `amount` at time `now`, given the deposit
/// was made at `deposited_at` under `tier`.
///
/// Math:
///   if now < deposited_at + cliff  -> 0
///   elif now >= deposited_at + cliff + linear -> amount
///   else: amount * (now - deposited_at - cliff) / linear
pub fn vested_amount(amount: u64, deposited_at: i64, now: i64, tier: VestTier) -> u64 {
    let s = tier.schedule();
    if now < deposited_at + s.cliff_secs {
        return 0;
    }
    let end = deposited_at + s.cliff_secs + s.linear_secs;
    if now >= end {
        return amount;
    }
    let dt = (now - deposited_at - s.cliff_secs) as u128;
    let res = (amount as u128 * dt) / (s.linear_secs as u128);
    res as u64
}

// -- Accounts ---------------------------------------------------------------------------------

#[derive(Accounts)]
pub struct InitializeProvider<'info> {
    pub mint: InterfaceAccount<'info, Mint>,
    /// CHECK: provider identity; used as seed for PDA.
    pub provider: UncheckedAccount<'info>,
    #[account(
        init,
        payer = payer,
        space = 8 + ProviderVesting::INIT_SPACE,
        seeds = [b"provider-vesting", mint.key().as_ref(), provider.key().as_ref()],
        bump
    )]
    pub provider_vesting: Account<'info, ProviderVesting>,
    #[account(
        init,
        payer = payer,
        token::mint = mint,
        token::authority = vault_authority,
        token::token_program = token_program,
        seeds = [b"vesting-vault", mint.key().as_ref(), provider.key().as_ref()],
        bump
    )]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA, vault authority.
    #[account(
        seeds = [b"vesting-vault-authority", mint.key().as_ref(), provider.key().as_ref()],
        bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(mut)]
    pub payer: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
    pub rent: Sysvar<'info, Rent>,
}

#[derive(Accounts)]
pub struct UpgradeTier<'info> {
    #[account(
        mut,
        seeds = [b"provider-vesting", provider_vesting.mint.as_ref(), provider_vesting.provider.as_ref()],
        bump = provider_vesting.bump
    )]
    pub provider_vesting: Account<'info, ProviderVesting>,
    #[account(address = provider_vesting.provider)]
    pub provider: Signer<'info>,
}

#[derive(Accounts)]
#[instruction(amount: u64)]
pub struct RecordDeposit<'info> {
    #[account(
        mut,
        seeds = [b"provider-vesting", provider_vesting.mint.as_ref(), provider_vesting.provider.as_ref()],
        bump = provider_vesting.bump
    )]
    pub provider_vesting: Account<'info, ProviderVesting>,
    #[account(
        init,
        payer = payer,
        space = 8 + VestingDeposit::INIT_SPACE,
        seeds = [
            b"vesting-deposit",
            provider_vesting.key().as_ref(),
            &(provider_vesting.deposit_counter + 1).to_le_bytes()
        ],
        bump
    )]
    pub deposit: Account<'info, VestingDeposit>,
    #[account(mut)]
    pub payer: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct WithdrawUnlocked<'info> {
    #[account(
        mut,
        seeds = [b"provider-vesting", mint.key().as_ref(), provider.key().as_ref()],
        bump = provider_vesting.bump,
        has_one = mint,
        has_one = vault,
    )]
    pub provider_vesting: Account<'info, ProviderVesting>,
    #[account(
        mut,
        seeds = [b"vesting-deposit", provider_vesting.key().as_ref(), &deposit.deposit_id.to_le_bytes()],
        bump = deposit.bump,
        constraint = deposit.provider_vesting == provider_vesting.key() @ VestingError::DepositMismatch
    )]
    pub deposit: Account<'info, VestingDeposit>,
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: vault authority PDA.
    #[account(
        seeds = [b"vesting-vault-authority", mint.key().as_ref(), provider.key().as_ref()],
        bump = provider_vesting.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    #[account(address = provider_vesting.provider)]
    pub provider: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct EarlyUnlock<'info> {
    #[account(
        mut,
        seeds = [b"provider-vesting", mint.key().as_ref(), provider.key().as_ref()],
        bump = provider_vesting.bump,
        has_one = mint,
        has_one = vault,
    )]
    pub provider_vesting: Account<'info, ProviderVesting>,
    #[account(
        mut,
        seeds = [b"vesting-deposit", provider_vesting.key().as_ref(), &deposit.deposit_id.to_le_bytes()],
        bump = deposit.bump,
        constraint = deposit.provider_vesting == provider_vesting.key() @ VestingError::DepositMismatch
    )]
    pub deposit: Account<'info, VestingDeposit>,
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: vault authority PDA.
    #[account(
        seeds = [b"vesting-vault-authority", mint.key().as_ref(), provider.key().as_ref()],
        bump = provider_vesting.vault_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    #[account(address = provider_vesting.provider)]
    pub provider: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

// -- State ------------------------------------------------------------------------------------

#[account]
#[derive(InitSpace)]
pub struct ProviderVesting {
    pub mint: Pubkey,
    pub provider: Pubkey,
    pub vault: Pubkey,
    pub tier: VestTier,
    pub deposit_counter: u64,
    pub total_deposited: u64,
    pub total_withdrawn: u64,
    pub total_burned: u64,
    pub last_early_unlock: i64,
    pub bump: u8,
    pub vault_bump: u8,
}

#[account]
#[derive(InitSpace)]
pub struct VestingDeposit {
    pub provider_vesting: Pubkey,
    pub deposit_id: u64,
    pub amount: u64,
    pub withdrawn: u64,
    pub deposited_at: i64,
    pub tier: VestTier,
    pub bump: u8,
}

// -- Events -----------------------------------------------------------------------------------

#[event]
pub struct DepositRecorded {
    pub provider: Pubkey,
    pub deposit_id: u64,
    pub amount: u64,
    pub tier: VestTier,
}

#[event]
pub struct Withdrew {
    pub provider: Pubkey,
    pub deposit_id: u64,
    pub amount: u64,
}

#[event]
pub struct EarlyUnlocked {
    pub provider: Pubkey,
    pub provider_vesting: Pubkey,
    pub deposit_id: u64,
    pub payout: u64,
    pub burned: u64,
}

#[event]
pub struct TierUpgraded {
    pub provider: Pubkey,
    pub new_tier: VestTier,
}

// -- Errors -----------------------------------------------------------------------------------

#[error_code]
pub enum VestingError {
    #[msg("Numeric overflow")]
    Overflow,
    #[msg("Nothing currently unlocked")]
    NothingToWithdraw,
    #[msg("Nothing left to early-unlock (fully vested already)")]
    NothingToEarlyUnlock,
    #[msg("Early unlock is on cooldown (one event per 12 months)")]
    EarlyUnlockOnCooldown,
    #[msg("Tier cannot be downgraded; only longer lockups allowed")]
    CannotDowngradeTier,
    #[msg("Deposit does not belong to this provider")]
    DepositMismatch,
}
