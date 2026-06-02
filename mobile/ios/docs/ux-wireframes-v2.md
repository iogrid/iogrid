# iogrid mobile iOS — UX wireframes v2

> **v2 rewrite** after founder rejected the Mullvad-style anonymous-account-number model:
> *"I dont understannd what is that account nuber concept is for? dont we use apple or google accoutn?"*
>
> Borrowing Mullvad's pattern was wrong for iogrid — iogrid users have identity (Apple ID / Google), payment is via ping wallet ($GRID tokens), and the VPN is a consumption surface on top of that, not a standalone product like Mullvad.

## Account model (the corrected one)

- **Auth**: Sign in with Apple (mandatory on iOS per App Store policy) + Sign in with Google (later, Android v1.1).
- **Identity**: Apple ID → maps to a iogrid account on first launch.
- **Wallet**: $GRID balance held in ping (the openova-group wallet/payments app). iogrid embeds ping's wallet view.
- **VPN bandwidth**: burns $GRID at a fixed rate (e.g. 0.001 $GRID/GB).
- **No "iogrid account number" invented** — Apple ID + ping wallet IS the identity.

## User journey

```
Cold launch
   ↓
Onboarding (2 screens — what iogrid is + privacy promise)
   ↓
Sign in with Apple  (system sheet, one tap)
   ↓
Main screen (DISCONNECTED) ─────────────────┐
   │                                         │
   ├─ Tap big button → CONNECTING            │
   │     ↓                                   │
   │   CONNECTED (live IP + stats + balance) │ Disconnect
   │                                         │
   ├─ Tap region card → Region picker        │
   │     ↓                                   │
   │   Pick region → returns to main         │
   │                                         │
   └─ Tap ⚙ → Settings                       │
         ├─ Account row → ping wallet view   │
         ├─ Top up → ping deeplink           │
         ├─ Connection prefs                 │
         └─ About                            │
                                             │
   ←─────────────────────────────────────────┘
```

## Screen 1 — Cold launch (T+0s)

```
┌─────────────────────────────┐
│ 9:41          ●●● ▮▮▮ ●●●  │
├─────────────────────────────┤
│            ◯                │
│          ╱╲╲╲              │
│         (   )              │
│          ╲╱                │
│         iogrid              │
│       ········              │
└─────────────────────────────┘
```

Disappears in <500ms.

## Screen 2 — Onboarding 1 of 2

```
┌─────────────────────────────┐
│  Skip               • ○     │
│       ╱╲ ╱╲ ╱╲             │
│      (  )(  )(  )           │
│       ╲╱ ╲╱ ╲╱             │
│         │  │  │             │
│          ╲ │ ╱              │
│           ▼                 │
│         ╱─┴─╲               │
│        │ you │              │
│         ╲───╱               │
│                             │
│  A VPN powered by           │
│  people, not data centers   │
│                             │
│  iogrid routes traffic      │
│  through real homes from    │
│  real people who rent       │
│  their idle bandwidth.      │
│  Pay only for what you use, │
│  in $GRID tokens.           │
│                             │
│  ┌───────────────────────┐  │
│  │       Continue        │  │
│  └───────────────────────┘  │
└─────────────────────────────┘
```

## Screen 3 — Onboarding 2 of 2

```
┌─────────────────────────────┐
│  ‹ Back             ○ •     │
│    ┌──────────────────┐     │
│    │  ☑ No logs       │     │
│    │  ☑ Apple-only ID │     │
│    │  ☑ No tracking   │     │
│    │  ☑ Pay with $GRID│     │
│    │     or Apple Pay │     │
│    └──────────────────┘     │
│                             │
│  Privacy by default.        │
│                             │
│  iogrid never stores traffic│
│  logs. Apple knows your ID; │
│  iogrid only sees a salted  │
│  hash. We can't link your   │
│  IP back to your account.   │
│                             │
│  ┌───────────────────────┐  │
│  │  Sign in with Apple   │  │
│  │   🍎                  │  │
│  └───────────────────────┘  │
└─────────────────────────────┘
```

## Screen 4 — Apple Sign-in (system sheet)

iOS native modal. After "Continue with Apple" tap:
- iogrid receives Apple ID token
- POST `/v1/identity/apple-signin` to identity-svc
- Backend creates customer record (if new) with: `apple_sub_hash`, `iogrid_account_id`, `wallet_address` (ping-issued)
- App caches a session token, navigates to Main

## Screen 5 — Main / DISCONNECTED

```
┌─────────────────────────────┐
│  iogrid              ⚙      │
├─────────────────────────────┤
│         ╭─────────╮         │
│        ╱           ╲        │
│       │  Tap to     │       │
│       │  connect    │       │
│        ╲           ╱        │
│         ╰─────────╯         │
│         DISCONNECTED        │
│                             │
│  ┌─────────────────────┐    │
│  │ 🌐 Best (auto)    › │    │
│  └─────────────────────┘    │
│                             │
│  Wallet                     │
│  ┌─────────────────────┐    │
│  │ 432 $GRID  ≈ $4.32  │    │
│  │ ~12 days @ usual    │    │
│  │ [   Top up      ›  ]│    │
│  └─────────────────────┘    │
└─────────────────────────────┘
```

## Screen 6 — Main / CONNECTING

```
┌─────────────────────────────┐
│  iogrid              ⚙      │
├─────────────────────────────┤
│         ╭─────────╮         │
│        ╱  ●●●●●    ╲        │
│       │  ●     ●   │       │
│       │    ●●●     │       │
│        ╲           ╱        │
│         ╰─────────╯         │
│         CONNECTING…         │
│         Frankfurt           │
│                             │
│   • Resolving peer          │
│   ○ Establishing tunnel     │
│   ○ Verifying egress IP     │
└─────────────────────────────┘
```

## Screen 7 — Main / CONNECTED

```
┌─────────────────────────────┐
│  iogrid              ⚙      │
├─────────────────────────────┤
│         ╭─────────╮         │
│        ╱           ╲        │
│       │  Connected  │       │
│        ╲           ╱        │
│         ╰─────────╯         │
│        🇩🇪 Frankfurt         │
│      45.79.83.117           │
│                             │
│   ↓ 1.2 MB                  │
│   ↑ 248 KB                  │
│   Speed: 47 Mbps            │
│   Latency: 23 ms            │
│                             │
│  Wallet                     │
│  ┌─────────────────────┐    │
│  │ 432 → 430 $GRID     │    │
│  │ 0.002 $GRID/min     │    │
│  │ [   Top up      ›  ]│    │
│  └─────────────────────┘    │
│                             │
│  ┌───────────────────────┐  │
│  │     Disconnect        │  │
│  └───────────────────────┘  │
└─────────────────────────────┘
```

## Screen 8 — Region picker

```
┌─────────────────────────────┐
│  ‹ Back  Choose region   ⌕  │
├─────────────────────────────┤
│  ┌─────────────────────┐    │
│  │ ✓ 🌐 Best (auto)    │    │
│  └─────────────────────┘    │
│                             │
│  EUROPE                     │
│  ┌─────────────────────┐    │
│  │ 🇩🇪 Germany       › │    │
│  │   3 cities • 12 ms │    │
│  └─────────────────────┘    │
│  ┌─────────────────────┐    │
│  │ 🇳🇱 Netherlands   › │    │
│  │   1 city • 18 ms   │    │
│  └─────────────────────┘    │
│                             │
│  AMERICAS                   │
│  ┌─────────────────────┐    │
│  │ 🇺🇸 United States  › │    │
│  │   8 cities • 89 ms │    │
│  └─────────────────────┘    │
└─────────────────────────────┘
```

## Screen 9 — Settings

```
┌─────────────────────────────┐
│  ‹ Done            Settings │
├─────────────────────────────┤
│  ACCOUNT                    │
│  ┌─────────────────────┐    │
│  │ Signed in as      › │    │
│  │ emrahbaysal@…       │    │
│  └─────────────────────┘    │
│  ┌─────────────────────┐    │
│  │ Wallet (ping)     › │    │
│  │ 432 $GRID  ≈ $4.32 │    │
│  └─────────────────────┘    │
│                             │
│  CONNECTION                 │
│  ┌─────────────────────┐    │
│  │ Auto-connect      ◯ │    │
│  └─────────────────────┘    │
│  ┌─────────────────────┐    │
│  │ Kill switch       ● │    │
│  └─────────────────────┘    │
│  ┌─────────────────────┐    │
│  │ DNS-leak protection● │    │
│  └─────────────────────┘    │
│                             │
│  ABOUT                      │
│  ┌─────────────────────┐    │
│  │ Privacy policy    › │    │
│  └─────────────────────┘    │
│  ┌─────────────────────┐    │
│  │ Terms of service  › │    │
│  └─────────────────────┘    │
│  ┌─────────────────────┐    │
│  │ Sign out          › │    │
│  └─────────────────────┘    │
└─────────────────────────────┘
```

## Screen 10 — Top up (ping deeplink / embed)

```
┌─────────────────────────────┐
│  ✕ Close             Top up │
├─────────────────────────────┤
│  Add $GRID to your wallet   │
│  Current balance            │
│  432 $GRID  ≈  $4.32        │
│                             │
│  Quick amounts              │
│  [ +500 $GRID  $5  ]        │
│  [ +2500 $GRID $25 ]        │
│  [ +10000 $GRID $100]       │
│  [ Custom amount    ]       │
│                             │
│  Pay with                   │
│  [  Apple Pay  ]            │
│  [  Card       ]            │
│  [  Bitcoin    ]            │
│  [  USDC       ]            │
│  [  Transfer $GRID from     │
│     another wallet         ]│
│                             │
│  ┌───────────────────────┐  │
│  │     Continue          │  │
│  └───────────────────────┘  │
│  Powered by ping            │
└─────────────────────────────┘
```

## Open questions (need founder answer to lock)

1. **Apple sign-in mandatory, or Apple OR Google OR wallet-direct?**
2. **Free tier?** (200MB/mo, ads, referrals) or pure pay-per-byte day 1?
3. **Ping iOS SDK or deeplink only?**
4. **Price display** — $GRID only, $USD only, or both?

## What changes vs v1 (Mullvad-pattern wireframes)

| v1 | v2 |
|---|---|
| 3 onboarding screens incl. "save your account number" | 2 onboarding screens, no number to memorize |
| Account-number reveal screen (16-digit) | DELETED — Apple ID is the identity |
| Free tier quota bar "200/2048 MB" | Wallet balance "432 $GRID ≈ $4.32" |
| "Upgrade for unlimited" CTA | "Top up" CTA → ping |
| 5 payment chips (Monero / BTC / Apple Pay / Card / Cash by mail) | Apple Pay / Card / BTC / USDC / $GRID transfer, all via ping |
| Settings → "Account number" row | Settings → "Signed in as <apple-email>" row |
