/**
 * $GRID SPL token deploy (Solana devnet | mainnet-beta).
 *
 * Refs iogrid/iogrid#595 (Track 5 / EPIC #581).
 *
 * Behaviour:
 *
 *   1. Read TREASURY_KEYPAIR_PATH (default ~/.config/solana/grid-treasury.json)
 *      — this becomes both the fee payer + the mint authority + the initial
 *      token-holder. Pre-mainnet we MUST swap to a Squads multisig; tracked
 *      in TODO #grid-multisig.
 *   2. Create a fresh Mint with 9 decimals, mint authority = treasury,
 *      freeze authority = NULL (so no holder can ever be frozen — non-
 *      negotiable transparency property called out in TOKENOMICS.md).
 *   3. Create the treasury ATA and mint the full 1_000_000_000 supply
 *      (i.e. 10^9 with 9 decimals = 10^18 atomic units) into it. The
 *      payout cron transfers OUT of this ATA per provider settlement
 *      (#598); the burn cron BurnCheck's from it (#597 / TOKENOMICS).
 *   4. Attach Metaplex Token-Metadata: name='iogrid', symbol='GRID',
 *      uri='https://iogrid.org/grid-token.json'. URI is content-
 *      addressable JSON served by the marketing site — kept off-chain so
 *      we can rotate logo / description without re-deploy.
 *   5. Print all addresses + a JSON dump for the operator to commit to
 *      docs/SOLANA-ADDRESSES.md.
 *
 * Usage:
 *
 *   pnpm install
 *   solana-keygen new -o grid-treasury.json   # one-time, save .pub somewhere safe
 *   TREASURY_KEYPAIR_PATH=$PWD/grid-treasury.json \
 *     pnpm tsx deploy.ts --cluster devnet
 *
 * The script is intentionally idempotent-safe: re-running with an existing
 * mint at MINT_ADDRESS env will skip the create step and only refresh the
 * metadata URI / mint extra supply if the env says so.
 */
import {
  Connection,
  Keypair,
  PublicKey,
  clusterApiUrl,
  sendAndConfirmTransaction,
  SystemProgram,
  Transaction,
  LAMPORTS_PER_SOL,
} from '@solana/web3.js';
import {
  createMint,
  getOrCreateAssociatedTokenAccount,
  mintTo,
  getMint,
  TOKEN_2022_PROGRAM_ID,
  AuthorityType,
  setAuthority,
} from '@solana/spl-token';
import {
  createMetadataAccountV3,
  findMetadataPda,
  MPL_TOKEN_METADATA_PROGRAM_ID,
} from '@metaplex-foundation/mpl-token-metadata';
import { createUmi } from '@metaplex-foundation/umi-bundle-defaults';
import {
  keypairIdentity,
  publicKey as umiPublicKey,
  some,
  none,
  PublicKey as UmiPublicKey,
} from '@metaplex-foundation/umi';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

// $GRID is an SPL **Token-2022** mint (canonical per docs/TOKENOMICS.md + the
// devnet mint, created with `--program-2022`). Every spl-token call below must
// carry this program id, or it defaults to the legacy Tokenkeg program and
// fails / creates the wrong token (Refs #629, #659).
const GRID_TOKEN_PROGRAM = TOKEN_2022_PROGRAM_ID;

const DECIMALS = 9;
const TOTAL_SUPPLY_TOKENS = 1_000_000_000n; // 1B $GRID
const TOKEN_NAME = 'iogrid';
const TOKEN_SYMBOL = 'GRID';
const TOKEN_URI = 'https://iogrid.org/grid-token.json';

interface CLI {
  cluster: 'devnet' | 'mainnet-beta';
  treasuryKeypairPath: string;
  rpcURL?: string;
  airdrop: boolean;
}

function parseCLI(): CLI {
  const args = process.argv.slice(2);
  const get = (k: string, dflt?: string) => {
    const i = args.indexOf(`--${k}`);
    if (i < 0) return dflt;
    return args[i + 1];
  };
  const cluster = (get('cluster', 'devnet') as CLI['cluster']) ?? 'devnet';
  if (cluster !== 'devnet' && cluster !== 'mainnet-beta') {
    throw new Error(`unknown cluster ${cluster}; must be devnet|mainnet-beta`);
  }
  const treasuryKeypairPath =
    process.env.TREASURY_KEYPAIR_PATH ??
    path.join(os.homedir(), '.config', 'solana', 'grid-treasury.json');
  const rpcURL = process.env.SOLANA_RPC_URL || get('rpc');
  const airdrop = cluster === 'devnet' && !args.includes('--no-airdrop');
  return { cluster, treasuryKeypairPath, rpcURL, airdrop };
}

function loadKeypair(p: string): Keypair {
  if (!fs.existsSync(p)) {
    throw new Error(`treasury keypair not found at ${p} — run: solana-keygen new -o ${p}`);
  }
  const secret = JSON.parse(fs.readFileSync(p, 'utf8'));
  return Keypair.fromSecretKey(new Uint8Array(secret));
}

async function ensureFunded(conn: Connection, pk: PublicKey, airdrop: boolean) {
  const bal = await conn.getBalance(pk);
  console.log(`treasury balance: ${(bal / LAMPORTS_PER_SOL).toFixed(4)} SOL`);
  if (bal >= 0.05 * LAMPORTS_PER_SOL) return;
  if (!airdrop) {
    throw new Error(
      `treasury ${pk.toBase58()} has <0.05 SOL and --no-airdrop set; fund it manually`,
    );
  }
  console.log('requesting 1 SOL airdrop (devnet)…');
  const sig = await conn.requestAirdrop(pk, 1 * LAMPORTS_PER_SOL);
  await conn.confirmTransaction(sig, 'confirmed');
}

async function main() {
  const cli = parseCLI();
  const endpoint = cli.rpcURL ?? clusterApiUrl(cli.cluster);
  console.log(`cluster=${cli.cluster} rpc=${endpoint}`);

  const treasury = loadKeypair(cli.treasuryKeypairPath);
  console.log(`treasury pubkey: ${treasury.publicKey.toBase58()}`);

  const conn = new Connection(endpoint, 'confirmed');
  await ensureFunded(conn, treasury.publicKey, cli.airdrop);

  // 1) Create mint OR re-use $GRID_MINT_ADDRESS env if set.
  let mintPk: PublicKey;
  if (process.env.GRID_MINT_ADDRESS) {
    mintPk = new PublicKey(process.env.GRID_MINT_ADDRESS);
    const info = await getMint(conn, mintPk, undefined, GRID_TOKEN_PROGRAM);
    console.log(
      `re-using existing mint ${mintPk.toBase58()} decimals=${info.decimals} supply=${info.supply}`,
    );
  } else {
    console.log('creating fresh mint…');
    mintPk = await createMint(
      conn,
      treasury, // fee payer
      treasury.publicKey, // mint authority
      null, // freeze authority NULL — per TOKENOMICS, holders can never be frozen
      DECIMALS,
      undefined, // keypair (auto-generated)
      undefined, // confirmOptions
      GRID_TOKEN_PROGRAM, // SPL Token-2022
    );
    console.log(`mint created: ${mintPk.toBase58()}`);
  }

  // 2) Treasury ATA + initial mint of full supply.
  const treasuryAta = await getOrCreateAssociatedTokenAccount(
    conn,
    treasury,
    mintPk,
    treasury.publicKey,
    false, // allowOwnerOffCurve
    undefined, // commitment
    undefined, // confirmOptions
    GRID_TOKEN_PROGRAM, // SPL Token-2022
  );
  console.log(`treasury ATA: ${treasuryAta.address.toBase58()}`);

  const currentMint = await getMint(conn, mintPk, undefined, GRID_TOKEN_PROGRAM);
  if (currentMint.supply === 0n) {
    const atomic = TOTAL_SUPPLY_TOKENS * 10n ** BigInt(DECIMALS); // 10^18
    console.log(`minting initial supply ${TOTAL_SUPPLY_TOKENS} $GRID (atomic=${atomic}) …`);
    await mintTo(conn, treasury, mintPk, treasuryAta.address, treasury, atomic, [], undefined, GRID_TOKEN_PROGRAM);
    console.log('initial supply minted.');
  } else {
    console.log(`mint supply already ${currentMint.supply}; skipping mintTo`);
  }

  // 3) Metaplex metadata. Umi has a much cleaner API for the MPL flow.
  const umi = createUmi(endpoint).use(
    keypairIdentity({
      publicKey: umiPublicKey(treasury.publicKey.toBase58()) as UmiPublicKey,
      secretKey: treasury.secretKey,
    }),
  );
  const mintUmi = umiPublicKey(mintPk.toBase58());
  const [metadataPda] = findMetadataPda(umi, { mint: mintUmi });
  console.log(`metadata PDA: ${String(metadataPda)}`);

  const existing = await umi.rpc.getAccount(metadataPda);
  if (existing.exists) {
    console.log('metadata already exists; leaving it as-is (rotate via UpdateMetadataV2 in a follow-up)');
  } else {
    const ix = createMetadataAccountV3(umi, {
      metadata: metadataPda,
      mint: mintUmi,
      mintAuthority: umi.identity,
      payer: umi.identity,
      updateAuthority: umi.identity.publicKey,
      data: {
        name: TOKEN_NAME,
        symbol: TOKEN_SYMBOL,
        uri: TOKEN_URI,
        sellerFeeBasisPoints: 0,
        creators: none(),
        collection: none(),
        uses: none(),
      },
      isMutable: true,
      collectionDetails: none(),
    });
    const sig = await ix.sendAndConfirm(umi);
    console.log(`metadata created sig=${Buffer.from(sig.signature).toString('hex')}`);
  }

  // 4) Optionally null the mint authority once the full supply is minted.
  //    Skipped by default — we'll keep it on the treasury (then later move to
  //    Squads multisig) so we can mint extra in case of emergency (chain re-org
  //    that drops the initial mintTo, etc). To finalise supply for real, set
  //    LOCK_MINT_AUTHORITY=1.
  if (process.env.LOCK_MINT_AUTHORITY === '1') {
    console.log('locking mint authority (setting to null) — supply is now final');
    await setAuthority(conn, treasury, mintPk, treasury, AuthorityType.MintTokens, null, [], undefined, GRID_TOKEN_PROGRAM);
  } else {
    console.log('mint authority retained on treasury (set LOCK_MINT_AUTHORITY=1 to null it)');
  }

  // 5) Final report.
  const finalMint = await getMint(conn, mintPk, undefined, GRID_TOKEN_PROGRAM);
  const out = {
    cluster: cli.cluster,
    rpc: endpoint,
    mint_address: mintPk.toBase58(),
    decimals: finalMint.decimals,
    supply_atomic: finalMint.supply.toString(),
    supply_human: (finalMint.supply / 10n ** BigInt(DECIMALS)).toString(),
    mint_authority: finalMint.mintAuthority?.toBase58() ?? null,
    freeze_authority: finalMint.freezeAuthority?.toBase58() ?? null,
    treasury: treasury.publicKey.toBase58(),
    treasury_ata: treasuryAta.address.toBase58(),
    token_program: GRID_TOKEN_PROGRAM.toBase58(),
    metadata_program: String(MPL_TOKEN_METADATA_PROGRAM_ID),
    metadata_pda: String(metadataPda),
    token_name: TOKEN_NAME,
    token_symbol: TOKEN_SYMBOL,
    token_uri: TOKEN_URI,
  };
  console.log('\n=== deployment summary ===');
  console.log(JSON.stringify(out, null, 2));
  console.log('\nNext: commit to docs/SOLANA-ADDRESSES.md and set GRID_TOKEN_MINT_ADDRESS in billing-svc env.');
}

main().catch((err) => {
  console.error('deploy failed:', err);
  process.exit(1);
});
