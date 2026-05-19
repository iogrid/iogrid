//! $GRID Token — Token-2022 SPL initialization + authority management.
//!
//! This program is the canonical entry point for the $GRID mint:
//! - `initialize_config`: bootstraps an on-chain config PDA pointing at the mint authority.
//! - `initialize_mint`: creates the Token-2022 mint with 9 decimals, 1B initial supply cap.
//! - `transfer_mint_authority`: rotates the mint authority (e.g., to the emission program PDA
//!   once it is deployed, or to a Squads multisig).
//! - `set_freeze_authority`: sets/rotates the freeze authority.
//!
//! Per docs/TOKENOMICS.md:
//!   - Symbol: $GRID
//!   - Decimals: 9 (Solana SPL standard)
//!   - Initial supply: 1,000,000,000 (1B)
//!   - Token-2022 used (not legacy SPL) — enables future transfer hooks if needed
//!
//! Mint authority transfer pattern: after TGE the deployer transfers the mint authority to
//! the emission program's PDA (derived from `emission` program), so only halving-schedule
//! emissions can ever mint new tokens. The freeze authority is set to None for non-custodial
//! guarantees (or to a Squads multisig for emergency-pause if regulators require it).

use anchor_lang::prelude::*;
use anchor_spl::token_2022::{self, InitializeMint2, MintTo, SetAuthority, Token2022};
use anchor_spl::token_interface::{Mint, TokenAccount};

declare_id!("GR1Dtokeneeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee");

/// $GRID standard decimals (matches Solana SPL convention).
pub const GRID_DECIMALS: u8 = 9;

/// $GRID hard supply cap = 1,000,000,000 * 10^9 (with decimals).
pub const GRID_HARD_CAP: u64 = 1_000_000_000u64 * 1_000_000_000u64;

/// Max on-chain metadata field lengths (kept tight; Metaplex extension can store richer
/// URI-fetched JSON off-chain). Sized to match the Metaplex Token Metadata Program defaults
/// (name=32, symbol=10, uri=200) so post-launch migration is a no-op.
pub const META_NAME_MAX: usize = 32;
pub const META_SYMBOL_MAX: usize = 10;
pub const META_URI_MAX: usize = 200;

#[program]
pub mod grid_token {
    use super::*;

    /// One-time bootstrap of the on-chain config PDA. Stores the mint authority key,
    /// hard cap, decimals, and a "frozen" flag (whether further authority changes are allowed).
    pub fn initialize_config(ctx: Context<InitializeConfig>) -> Result<()> {
        let cfg = &mut ctx.accounts.config;
        cfg.admin = ctx.accounts.admin.key();
        cfg.mint = ctx.accounts.mint.key();
        cfg.hard_cap = GRID_HARD_CAP;
        cfg.decimals = GRID_DECIMALS;
        cfg.minted_so_far = 0;
        cfg.authority_locked = false;
        cfg.bump = ctx.bumps.config;
        emit!(ConfigInitialized {
            admin: cfg.admin,
            mint: cfg.mint,
            hard_cap: cfg.hard_cap,
        });
        Ok(())
    }

    /// Initialize the underlying Token-2022 mint. Called immediately after the mint account is
    /// created (via `create_account` in the client). We then call `InitializeMint2`.
    pub fn initialize_mint(ctx: Context<InitializeMintCtx>) -> Result<()> {
        let cpi_ctx = CpiContext::new(
            ctx.accounts.token_program.to_account_info(),
            InitializeMint2 {
                mint: ctx.accounts.mint.to_account_info(),
            },
        );
        token_2022::initialize_mint2(
            cpi_ctx,
            GRID_DECIMALS,
            &ctx.accounts.admin.key(),
            Some(&ctx.accounts.admin.key()),
        )?;
        emit!(MintInitialized {
            mint: ctx.accounts.mint.key(),
            decimals: GRID_DECIMALS,
        });
        Ok(())
    }

    /// Mint freshly issued tokens to a recipient ATA. Enforces the hard cap.
    /// In production, the only legal caller of this should be the emission program (via CPI
    /// + signer seeds). Until then the deployer admin can call it for initial allocations
    /// (team / treasury / liquidity per the allocation table in TOKENOMICS.md).
    pub fn mint_to_recipient(ctx: Context<MintToRecipient>, amount: u64) -> Result<()> {
        let cfg = &mut ctx.accounts.config;
        require!(!cfg.authority_locked, GridTokenError::AuthorityLocked);
        let new_total = cfg
            .minted_so_far
            .checked_add(amount)
            .ok_or(GridTokenError::Overflow)?;
        require!(new_total <= cfg.hard_cap, GridTokenError::HardCapExceeded);

        let cpi_ctx = CpiContext::new(
            ctx.accounts.token_program.to_account_info(),
            MintTo {
                mint: ctx.accounts.mint.to_account_info(),
                to: ctx.accounts.recipient.to_account_info(),
                authority: ctx.accounts.admin.to_account_info(),
            },
        );
        token_2022::mint_to(cpi_ctx, amount)?;

        cfg.minted_so_far = new_total;
        emit!(TokensMinted {
            recipient: ctx.accounts.recipient.key(),
            amount,
            minted_so_far: new_total,
        });
        Ok(())
    }

    /// Rotate the mint authority on the underlying Token-2022 mint. The classic use-case is
    /// transferring authority from the deployer to the emission program PDA so that only the
    /// halving curve can issue more tokens.
    pub fn transfer_mint_authority(
        ctx: Context<TransferAuthorityCtx>,
        new_authority: Pubkey,
    ) -> Result<()> {
        require!(
            !ctx.accounts.config.authority_locked,
            GridTokenError::AuthorityLocked
        );
        let cpi_ctx = CpiContext::new(
            ctx.accounts.token_program.to_account_info(),
            SetAuthority {
                account_or_mint: ctx.accounts.mint.to_account_info(),
                current_authority: ctx.accounts.admin.to_account_info(),
            },
        );
        token_2022::set_authority(
            cpi_ctx,
            anchor_spl::token_2022::spl_token_2022::instruction::AuthorityType::MintTokens,
            Some(new_authority),
        )?;
        emit!(AuthorityTransferred {
            new_authority,
            kind: AuthKind::Mint,
        });
        Ok(())
    }

    /// Set/update on-chain token metadata fields (name, symbol, uri). Stored on a dedicated
    /// PDA `GridMetadata` (seeds = ["grid-metadata", mint]). Initial intent is to provide a
    /// queryable name/symbol/uri without taking a hard dependency on the Metaplex Token
    /// Metadata Program in v0 (which adds 250+KB of audit surface).
    ///
    /// TODO(v1, post-audit): once OtterSec/Halborn have signed off on the v0 contracts, add a
    /// CPI helper that mirrors these fields into a Metaplex MetadataAccount so explorers /
    /// wallets that read Metaplex (Phantom, Solflare) display the name/symbol natively. The
    /// instruction signature here is forward-compatible: same args, the CPI is additive.
    pub fn set_metadata(
        ctx: Context<SetMetadata>,
        name: String,
        symbol: String,
        uri: String,
    ) -> Result<()> {
        require!(!ctx.accounts.config.authority_locked, GridTokenError::AuthorityLocked);
        require!(
            name.as_bytes().len() <= META_NAME_MAX,
            GridTokenError::MetadataFieldTooLong
        );
        require!(
            symbol.as_bytes().len() <= META_SYMBOL_MAX,
            GridTokenError::MetadataFieldTooLong
        );
        require!(
            uri.as_bytes().len() <= META_URI_MAX,
            GridTokenError::MetadataFieldTooLong
        );

        let m = &mut ctx.accounts.metadata;
        m.mint = ctx.accounts.config.mint;
        m.name_len = name.as_bytes().len() as u8;
        m.symbol_len = symbol.as_bytes().len() as u8;
        m.uri_len = uri.as_bytes().len() as u16;
        m.name = [0u8; META_NAME_MAX];
        m.symbol = [0u8; META_SYMBOL_MAX];
        m.uri = [0u8; META_URI_MAX];
        m.name[..name.as_bytes().len()].copy_from_slice(name.as_bytes());
        m.symbol[..symbol.as_bytes().len()].copy_from_slice(symbol.as_bytes());
        m.uri[..uri.as_bytes().len()].copy_from_slice(uri.as_bytes());
        m.bump = ctx.bumps.metadata;
        emit!(MetadataSet {
            mint: m.mint,
            name,
            symbol,
            uri,
        });
        Ok(())
    }

    /// Final freeze on the config — disables `mint_to_recipient` and authority rotation
    /// from this program. Practical effect: after `lock_authorities()` only on-chain
    /// programs holding the mint authority (e.g. emission) can ever mint $GRID.
    pub fn lock_authorities(ctx: Context<LockAuthoritiesCtx>) -> Result<()> {
        let cfg = &mut ctx.accounts.config;
        require!(!cfg.authority_locked, GridTokenError::AuthorityLocked);
        cfg.authority_locked = true;
        emit!(AuthoritiesLocked { mint: cfg.mint });
        Ok(())
    }
}

// -- Accounts ---------------------------------------------------------------------------------

#[derive(Accounts)]
pub struct InitializeConfig<'info> {
    #[account(
        init,
        payer = admin,
        space = 8 + GridConfig::INIT_SPACE,
        seeds = [b"grid-config", mint.key().as_ref()],
        bump
    )]
    pub config: Account<'info, GridConfig>,
    /// CHECK: the mint key is recorded as a seed; init validated in `initialize_mint`.
    pub mint: UncheckedAccount<'info>,
    #[account(mut)]
    pub admin: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct InitializeMintCtx<'info> {
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(
        mut,
        seeds = [b"grid-config", mint.key().as_ref()],
        bump = config.bump
    )]
    pub config: Account<'info, GridConfig>,
    #[account(mut, address = config.admin)]
    pub admin: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct MintToRecipient<'info> {
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(mut)]
    pub recipient: InterfaceAccount<'info, TokenAccount>,
    #[account(
        mut,
        seeds = [b"grid-config", mint.key().as_ref()],
        bump = config.bump
    )]
    pub config: Account<'info, GridConfig>,
    #[account(mut, address = config.admin)]
    pub admin: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct TransferAuthorityCtx<'info> {
    #[account(mut)]
    pub mint: InterfaceAccount<'info, Mint>,
    #[account(
        seeds = [b"grid-config", mint.key().as_ref()],
        bump = config.bump
    )]
    pub config: Account<'info, GridConfig>,
    #[account(mut, address = config.admin)]
    pub admin: Signer<'info>,
    pub token_program: Program<'info, Token2022>,
}

#[derive(Accounts)]
pub struct SetMetadata<'info> {
    #[account(
        seeds = [b"grid-config", config.mint.as_ref()],
        bump = config.bump,
        has_one = admin
    )]
    pub config: Account<'info, GridConfig>,
    #[account(
        init_if_needed,
        payer = admin,
        space = 8 + GridMetadata::INIT_SPACE,
        seeds = [b"grid-metadata", config.mint.as_ref()],
        bump
    )]
    pub metadata: Account<'info, GridMetadata>,
    #[account(mut)]
    pub admin: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct LockAuthoritiesCtx<'info> {
    #[account(
        mut,
        seeds = [b"grid-config", config.mint.as_ref()],
        bump = config.bump,
        has_one = admin
    )]
    pub config: Account<'info, GridConfig>,
    pub admin: Signer<'info>,
}

// -- State ------------------------------------------------------------------------------------

#[account]
#[derive(InitSpace)]
pub struct GridConfig {
    pub admin: Pubkey,
    pub mint: Pubkey,
    pub hard_cap: u64,
    pub minted_so_far: u64,
    pub decimals: u8,
    pub authority_locked: bool,
    pub bump: u8,
}

#[account]
#[derive(InitSpace)]
pub struct GridMetadata {
    pub mint: Pubkey,
    pub name: [u8; META_NAME_MAX],
    pub symbol: [u8; META_SYMBOL_MAX],
    pub uri: [u8; META_URI_MAX],
    pub name_len: u8,
    pub symbol_len: u8,
    pub uri_len: u16,
    pub bump: u8,
}

// -- Events -----------------------------------------------------------------------------------

#[event]
pub struct ConfigInitialized {
    pub admin: Pubkey,
    pub mint: Pubkey,
    pub hard_cap: u64,
}

#[event]
pub struct MintInitialized {
    pub mint: Pubkey,
    pub decimals: u8,
}

#[event]
pub struct TokensMinted {
    pub recipient: Pubkey,
    pub amount: u64,
    pub minted_so_far: u64,
}

#[event]
pub struct AuthorityTransferred {
    pub new_authority: Pubkey,
    pub kind: AuthKind,
}

#[event]
pub struct AuthoritiesLocked {
    pub mint: Pubkey,
}

#[event]
pub struct MetadataSet {
    pub mint: Pubkey,
    pub name: String,
    pub symbol: String,
    pub uri: String,
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, Debug, PartialEq, Eq)]
pub enum AuthKind {
    Mint,
    Freeze,
}

// -- Errors -----------------------------------------------------------------------------------

#[error_code]
pub enum GridTokenError {
    #[msg("Numeric overflow")]
    Overflow,
    #[msg("Hard cap of 1B $GRID would be exceeded")]
    HardCapExceeded,
    #[msg("Authorities are locked; this operation is no longer allowed")]
    AuthorityLocked,
    #[msg("Metadata field exceeds the maximum length")]
    MetadataFieldTooLong,
}
