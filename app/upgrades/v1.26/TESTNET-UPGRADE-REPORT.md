# Continuance testnet upgrade — verification report

| | |
|--|--|
| **Network** | pulsar-3 |
| **Upgrade** | `v1.26.0-community-continuance` |
| **Proposal** | #7 |
| **Plan height** | 3,198,416 |
| **Result** | **PASS** (46/46 checks) |
| **Verified** | 2026-08-05 (live re-query + pre/post packs) |

**1 uscrt = 0.000001 SCRT** · **1 SCRT = 1,000,000 uscrt**

---

## Timeline

| | Height | Binary | Note |
|--|-------:|--------|------|
| Pre (halt) | 3,198,416 | 1.25.0 | pre-upgrade evidence pack |
| Post | 3,198,444 | 1.26.0 | post-upgrade evidence pack |
| Re-verify | ~3,199,285 | 1.26.0 | live chain queries |

Enclave unchanged: sha256 `a1d923c21475c066f94e5388c091f56ff7ea08d25fddfe52c2757b45b3c8a2e8` · MRENCLAVE `cf8ac4c7023b0a1f3c76d83166806fc5771a1eb048d39e10888afacda236d703`

---

## Chain / policy

| Check | Pre | Post / live | Status |
|-------|-----|-------------|--------|
| Plan | set @ 3,198,416 | cleared `{}` | **PASS** |
| Inflation | ~0.866 | **0.05** (min=max=0.05) | **PASS** |
| Foundation tax | 0.90 | **0** | **PASS** |
| Enclave | fleet | same | **PASS** |
| Blocks | halted | advancing, not catching up | **PASS** |

### Mint (total supply uscrt)

| | uscrt | SCRT |
|--|------:|-----:|
| Pre | 152,523,386,800,242,937 | 152,523,386,800.242937 |
| Post (evidence) | 153,602,422,088,649,766 | 153,602,422,088.649766 |
| **Delta** | **+1,079,035,288,406,829** | **+1,079,035,288.406829** |
| Expected mint | +1,079,000,000,000,000 | **+1,079,000,000** |

Extra beyond mint ≈ post-upgrade block provision. **PASS**

---

## Buckets (live)

Each row: balance = allocation + **1 SCRT** seed (1,000,000 uscrt), unless noted.

| Bucket | Type | Balance | Spendable | Original vesting | Vesting detail | Status |
|--------|------|--------:|----------:|-----------------:|----------------|--------|
| foundation | ContinuousVesting | 299,000,001 SCRT · 299,000,001,000,000 uscrt | 29,900,001 SCRT · 29,900,001,000,000 uscrt | 269,100,000 SCRT · 269,100,000,000,000 uscrt | ~60 mo window | **PASS** |
| core_development | ContinuousVesting | 299,000,001 SCRT · 299,000,001,000,000 uscrt | 29,900,001 SCRT · 29,900,001,000,000 uscrt | 269,100,000 SCRT · 269,100,000,000,000 uscrt | ~60 mo window | **PASS** |
| advisors | PeriodicVesting | 72,000,001 SCRT · 72,000,001,000,000 uscrt | 1 SCRT · 1,000,000 uscrt | 72,000,000 SCRT · 72,000,000,000,000 uscrt | **16** × 4,500,000 SCRT | **PASS** |
| ecosystem_fund | BaseAccount | 178,000,001 SCRT · 178,000,001,000,000 uscrt | same (all liquid) | — | — | **PASS** |
| research_development | ContinuousVesting | 72,000,001 SCRT · 72,000,001,000,000 uscrt | ≥ 7,200,001 SCRT (live ~7,203,065 SCRT) | 64,800,000 SCRT · 64,800,000,000,000 uscrt | ~10% liquid + unlock | **PASS** |
| builder_relayer_support | BaseAccount | 43,000,001 SCRT · 43,000,001,000,000 uscrt | same (all liquid) | — | — | **PASS** |
| remediation | ContinuousVesting | 44,000,001 SCRT · 44,000,001,000,000 uscrt | 13,200,001 SCRT · 13,200,001,000,000 uscrt | 30,800,000 SCRT · 30,800,000,000,000 uscrt | 30% liquid | **PASS** |
| validator_program | PeriodicVesting | 71,040,001 SCRT · 71,040,001,000,000 uscrt | 6,240,001 SCRT · 6,240,001,000,000 uscrt | 64,800,000 SCRT · 64,800,000,000,000 uscrt | **20** × 3,240,000 SCRT; 71.04M = 72M − 4×240k | **PASS** |

---

## Seats (4 paid)

| Seat | Balance | Spendable | Status |
|------|--------:|----------:|--------|
| seat01–04 each | 240,000 SCRT · 240,000,000,000 uscrt | same | **PASS** |
| **Total paid** | **960,000 SCRT** · **960,000,000,000 uscrt** | | **PASS** |

---

## Check summary

| Area | Checks | Result |
|------|-------:|--------|
| Chain / policy / enclave / mint | 8 | all **PASS** |
| Buckets (type, balance, spendable, original_vesting, schedules) | 30 | all **PASS** |
| Seats | 4 | all **PASS** |
| Vesting windows (foundation/core) | 2 | all **PASS** |
| **Total** | **46** | **46 PASS · 0 FAIL** |

---

## Packages (public)

https://github.com/Secret3dev/SecretNetwork/releases/tag/v1.26.0-community-continuance

| File | SHA-256 |
|------|---------|
| `…_ubuntu-22.04.deb` | `672dd49b5b95587559e998f89e260142a4bcdb958a5f3009ece50e8ff4ce06d4` |
| `…_ubuntu-24.04.deb` | `d86def55a3c38c7de3dd91583bc66c29e05962e039e0b2ae47939b5f04faf925` |

Install: `scripts/install-testnet.sh` on that release (detects 22.04 / 24.04).  
Do not use `--unsafe-skip-upgrades`.

---

## Conclusion

On **pulsar-3**, Continuance executed at height **3,198,416**. Binary **1.26.0**, enclave unchanged, inflation **5%**, foundation tax **0**, mint **1,079,000,000 SCRT**, all eight buckets and four seats match design (including vesting types, original_vesting, spendable, and period schedules).

**Testnet Continuance: verified successful.**
