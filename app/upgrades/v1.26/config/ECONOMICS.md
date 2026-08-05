# Economics — GENERATED, do not edit

Generated from `app/upgrades/v1.26/config/config.go` by
`TestEconomicsDocIsCurrent`. Editing this file by hand will be overwritten,
and CI fails if the code changes without regenerating.

**Every other document links here instead of restating these numbers.**

| Bucket | Total SCRT | Liquid at execution | Lockup |
|---|---:|---:|---|
| foundation | 299,000,000 | 10% | linear over 60 months, after a 6-month cliff |
| core_development | 299,000,000 | 10% | linear over 60 months, after a 6-month cliff |
| advisors | 72,000,000 | 0% | 6-month cliff, then 16 quarterly unlocks of 4,500,000 SCRT |
| ecosystem_fund | 178,000,000 | 100% | none — fully liquid |
| research_development | 72,000,000 | 10% | linear over 60 months, no cliff |
| builder_relayer_support | 43,000,000 | 100% | none — fully liquid |
| remediation | 44,000,000 | 30% | linear over 24 months, after a 6-month cliff |
| validator_program | 72,000,000 | 10% seat pool | 64,800,000 SCRT quarterly-vested at the program address over 60 months (20 unlocks) |
| **Total** | **1,079,000,000** | **308,400,000** | |

## Derived figures

- Mint total: **1,079,000,000 SCRT** (asserted equal to `TotalMintSCRT`)
- Day-one liquid: **308,400,000 SCRT**
- Validator seats: **30 × 240,000 SCRT** = 7,200,000 SCRT seat pool
- Inflation pinned at: **0.050000000000000000**
- Secret foundation tax zeroed: **true**
- Upgrade name (all chains): **`v1.26.0-community-continuance`**
- Chains recognised: `secret-4` (mainnet), `pulsar-3` (testnet), `secretdev-1` (localsecret)
