//! $GRID Burn — buyback-and-burn registry (Layer 1 of TOKENOMICS.md).
//!
//! Two flows:
//!  1. `record_burn(amount, revenue_cents, source_tag)`: the off-chain billing-svc has already
//!     swapped a portion of revenue to $GRID and burned it via the Token-2022 `Burn`
//!     instruction (or transferred to the well-known incinerator address). This program then
//!     records the burn for public verifiability — total-burned counter, source breakdown,
//!     timestamp.
//!  2. `burn_via_program(amount)`: alternative path where this program does the burn CPI
//!     itself (caller transfers tokens to the burn vault first; then this instruction burns
//!     them in one atomic step + records). Used when we want the burn to be cryptographically
//!     enforced by the same tx that updates the registry.
//!
//! Public registry is callable read-only via `simulate` — `get_registry()` returns the running
//! total. UI at `burn.iogrid.org` pulls from the BurnRegistry account.

use anchor_lang::prelude::*;
use anchor_spl::token_2022::{self, Burn, Token2022};
use anchor_spl::token_interface::{Mint, TokenAccount};

declare_id!("GR1Dburnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn");

/// Maximum source-tag length (e.g., "stripe-revenue-burn", "customer-grid-payment").
pub const MAX_SOURCE_TAG_LEN: usize = 32;

#[program]
pub mod burn {
    use super::*;

    /// Bootstrap the burn registry for a mint. Anyone can read; only `attestor` can record.
    pub fn initialize_registry(ctx: Context<InitializeRegistry>) -> Result<()> {
        let r = &mut ctx.accounts.registry;
        r.mint = ctx.accounts.mint.key();
        r.admin = ctx.accounts.admin.key();
        r.attestor = ctx.accounts.attestor.key();
        r.burn_count = 0;
        r.total_burned = 0;
        r.total_revenue_cents_attributed = 0;
        r.bump = ctx.bumps.registry;
        Ok(())
    }

    /// Record a burn event (post-fact). Off-chain billing-svc has already moved tokens to
    /// the canonical incinerator address; this just appends to the registry.
    pub fn record_burn(
        ctx: Context<RecordBurn>,
        amount: u64,
        revenue_cents: u64,
        source_tag: String,
    ) -> Result<()> {
        require!(amount > 0, BurnError::ZeroAmount);
        require!(
            source_tag.as_bytes().len() <= MAX_SOURCE_TAG_LEN,
            BurnError::TagTooLong
        );

        let now = Clock::get()?.unix_timestamp;
        let r = &mut ctx.accounts.registry;
        let new_count = r.burn_count + 1;

        let receipt = &mut ctx.accounts.receipt;
        receipt.registry = r.key();
        receipt.seq = new_count;
        receipt.amount = amount;
        receipt.revenue_cents = revenue_cents;
        receipt.source_tag_len = source_tag.as_bytes().len() as u8;
        let mut buf = [0u8; MAX_SOURCE_TAG_LEN];
        let bytes = source_tag.as_bytes();
        buf[..bytes.len()].copy_from_slice(bytes);
        receipt.source_tag = buf;
        receipt.burned_at = now;
        receipt.bump = ctx.bumps.receipt;

        r.burn_count = new_count;
        r.total_burned = r
            .total_burned
            .checked_add(amount)
            .ok_or(BurnError::Overflow)?;
        r.total_revenue_cents_attributed = r
            .total_revenue_cents_attributed
            .checked_add(revenue_cents)
            .ok_or(BurnError::Overflow)?;

        emit!(BurnRecorded {
            seq: new_count,
            amount,
            revenue_cents,
            source_tag,
            total_burned: r.total_burned,
        });
        Ok(())
    }

    /// Atomic burn-and-record: tokens in `source` are burned in this same tx. The signer must
    /// be the source ATA's owner OR an authority delegated by it. Records the burn receipt.
    pub fn burn_via_program(
        ctx: Context<BurnViaProgram>,
        amount: u64,
        revenue_cents: u64,
        source_tag: String,
    ) -> Result<()> {
        require!(amount > 0, BurnError::ZeroAmount);
        require!(
            source_tag.as_bytes().len() <= MAX_SOURCE_TAG_LEN,
            BurnError::TagTooLong
        );

        // Burn CPI
        let cpi_ctx = CpiContext::new(
            ctx.accounts.token_program.to_account_info(),
            Burn {
                mint: ctx.accounts.mint.to_account_info(),
                from: ctx.accounts.source.to_account_info(),
                authority: ctx.accounts.source_authority.to_account_info(),
            },
        );
        token_2022::burn(cpi_ctx, amount)?;

        // Append receipt
        let now = Clock::get()?.unix_timestamp;
        let r = &mut ctx.accounts.registry;
        let new_count = r.burn_count + 1;
        let receipt = &mut ctx.accounts.receipt;
        receipt.registry = r.key();
        receipt.seq = new_count;
        receipt.amount = amount;
        receipt.revenue_cents = revenue_cents;
        receipt.source_tag_len = source_tag.as_bytes().len() as u8;
        let mut buf = [0u8; MAX_SOURCE_TAG_LEN];
        let bytes = source_tag.as_bytes();
        buf[..bytes.len()].copy_from_slice(bytes);
        receipt.source_tag = buf;
        receipt.burned_at = now;
        receipt.bump = ctx.bumps.receipt;
        r.burn_count = new_count;
        r.total_burned = r
            .total_burned
            .checked_add(amount)
            .ok_or(BurnError::Overflow)?;
        r.total_revenue_cents_attributed = r
            .total_revenue_cents_attributed
            .checked_add(revenue_cents)
            .ok_or(BurnError::Overflow)?;
        emit!(BurnRecorded {
            seq: new_count,
            amount,
            revenue_cents,
            source_tag,
            total_burned: r.total_burned,
        });
        Ok(())
    }

    /// Rotate the attestor (e.g., move from a hot key to a Squads multisig).
    pub fn rotate_attestor(ctx: Context<RotateAttestor>, new_attestor: Pubkey) -> Result<()> {
        let r = &mut ctx.accounts.registry;
        let old = r.attestor;
        r.attestor = new_attestor;
        emit!(AttestorRotated {
            old,
            new: new_attestor,
        });
        Ok(())
    }
}

// -- Accounts ---------------------------------------------------------------------------------

#[derive(Accounts)]
pub struct InitializeRegistry<'info> {
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(
        init,
        payer = admin,
        space = 8 + BurnRegistry::INIT_SPACE,
        seeds = [b"burn-registry", mint.key().as_ref()],
        bump
    )]
    pub registry: Account<'info, BurnRegistry>,
    /// CHECK: attestor pubkey is just recorded for later auth.
    pub attestor: UncheckedAccount<'info>,
    #[account(mut)]
    pub admin: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct RecordBurn<'info> {
    #[account(
        mut,
        seeds = [b"burn-registry", registry.mint.as_ref()],
        bump = registry.bump,
        has_one = attestor
    )]
    pub registry: Account<'info, BurnRegistry>,
    #[account(
        init,
        payer = attestor,
        space = 8 + BurnReceipt::INIT_SPACE,
        seeds = [
            b"burn-receipt",
            registry.key().as_ref(),
            &(registry.burn_count + 1).to_le_bytes()
        ],
        bump
    )]
    pub receipt: Account<'info, BurnReceipt>,
    #[account(mut)]
    pub attestor: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct BurnViaProgram<'info> {
    #[account(
        mut,
        seeds = [b"burn-registry", registry.mint.as_ref()],
        bump = registry.bump,
        has_one = attestor
    )]
    pub registry: Account<'info, BurnRegistry>,
    #[account(mut, address = registry.mint)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub source: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: must be the source ATA's authority/owner; SPL enforces.
    pub source_authority: Signer<'info>,
    #[account(
        init,
        payer = attestor,
        space = 8 + BurnReceipt::INIT_SPACE,
        seeds = [
            b"burn-receipt",
            registry.key().as_ref(),
            &(registry.burn_count + 1).to_le_bytes()
        ],
        bump
    )]
    pub receipt: Account<'info, BurnReceipt>,
    #[account(mut)]
    pub attestor: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct RotateAttestor<'info> {
    #[account(
        mut,
        seeds = [b"burn-registry", registry.mint.as_ref()],
        bump = registry.bump,
        has_one = admin
    )]
    pub registry: Account<'info, BurnRegistry>,
    pub admin: Signer<'info>,
}

// -- State ------------------------------------------------------------------------------------

#[account]
#[derive(InitSpace)]
pub struct BurnRegistry {
    pub mint: Pubkey,
    pub admin: Pubkey,
    pub attestor: Pubkey,
    pub burn_count: u64,
    pub total_burned: u64,
    pub total_revenue_cents_attributed: u64,
    pub bump: u8,
}

#[account]
#[derive(InitSpace)]
pub struct BurnReceipt {
    pub registry: Pubkey,
    pub seq: u64,
    pub amount: u64,
    pub revenue_cents: u64,
    pub burned_at: i64,
    pub source_tag: [u8; MAX_SOURCE_TAG_LEN],
    pub source_tag_len: u8,
    pub bump: u8,
}

// -- Events -----------------------------------------------------------------------------------

#[event]
pub struct BurnRecorded {
    pub seq: u64,
    pub amount: u64,
    pub revenue_cents: u64,
    pub source_tag: String,
    pub total_burned: u64,
}

#[event]
pub struct AttestorRotated {
    pub old: Pubkey,
    pub new: Pubkey,
}

// -- Errors -----------------------------------------------------------------------------------

#[error_code]
pub enum BurnError {
    #[msg("Numeric overflow")]
    Overflow,
    #[msg("Cannot burn zero")]
    ZeroAmount,
    #[msg("source_tag exceeds MAX_SOURCE_TAG_LEN")]
    TagTooLong,
}
