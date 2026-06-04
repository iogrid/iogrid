# Runbook — $GRID mainnet cutover (#665)

The Ping/$GRID payment integration is built and runs **end-to-end on devnet**
today (`coordinator/services/vpn-svc/internal/payment/sig_verify.go` +
`mobile/ios/src/lib/wallets/ping-pay.ts`, 24 jest tests green, gated by
`PHANTOM_CLUSTER`). Two external residuals gate the *mainnet* go-live; this
runbook is the turnkey cutover once they clear.

## External gates (not engineering)

1. **$GRID SPL mint on Solana mainnet** — a real-money on-chain token deploy.
   Founder go-live decision. Produces the mainnet mint address.
2. **Ping C-8 sig-verify ruling** — Ping's decision on the approve-signature
   contract (filed `ping-cash#188`). Their call.

## Cutover (once the mint exists + C-8 is settled)

The integration is a config flip — no code change:

```bash
# 1. vpn-svc escrow -> mainnet mint (image-only reroll picks up the env)
kubectl set env deploy/vpn-svc -n iogrid \
  GRID_TOKEN_MINT_ADDRESS=<mainnet-mint-address> \
  SOLANA_RPC_URL=<mainnet-rpc>
# (GRID_TOKEN_PROGRAM_ID stays default unless Ping specifies otherwise)

# 2. mobile build -> mainnet (next TestFlight/Play build)
#    set in the CI build env:
#      EXPO_PUBLIC_SOLANA_NETWORK=mainnet-beta
#      (PHANTOM_CLUSTER flips devnet -> mainnet-beta in phantom.ts)

# 3. if Ping's C-8 ruling changes the sig-verify shape, the one-commit swap is
#    pre-staged both ways (documented in #665 / ping-cash#188).
```

## Verify

- vpn-svc: a real wallet-signed `payment_authorization` on `POST /sessions/mobile`
  passes `Authorize` against the mainnet mint (no `ErrInsufficientBalance` for a
  funded wallet).
- mobile: a real $GRID top-up + a VPN session paid on mainnet.

Until both gates clear, devnet is the live, tested path — the cutover above is
the entire remaining work and it is two config edits plus a redeploy.
