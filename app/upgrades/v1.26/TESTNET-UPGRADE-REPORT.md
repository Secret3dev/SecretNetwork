# Continuance testnet upgrade — verification report

| | |
|--|--|
| **Network** | pulsar-3 |
| **Upgrade** | `v1.26.0-community-continuance` |
| **Proposal** | #7 |
| **Plan height** | 3,198,416 |
| **Result** | **PASS** (46/46 checks) |
| **Verified** | 2026-08-05 (live re-query + pre/post packs) |
| **Explorer** | [testnet.ping.pub/secret](https://testnet.ping.pub/secret) |

**1 uscrt = 0.000001 SCRT** · **1 SCRT = 1,000,000 uscrt**

Account links use: `https://testnet.ping.pub/secret/account/<address>`

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

### Mint (total supply)

| | uscrt | SCRT |
|--|------:|-----:|
| Pre | 152,523,386,800,242,937 | 152,523,386,800.242937 |
| Post (evidence) | 153,602,422,088,649,766 | 153,602,422,088.649766 |
| **Delta** | **+1,079,035,288,406,829** | **+1,079,035,288.406829** |
| Expected mint | +1,079,000,000,000,000 | **+1,079,000,000** |

Extra beyond mint ≈ post-upgrade block provision. **PASS**

---

## Buckets (live)

Balance = allocation + **1 SCRT** seed (1,000,000 uscrt), unless noted. Click address to open explorer.

| Bucket | Explorer | Type | Balance | Spendable | Original vesting | Vesting | Status |
|--------|----------|------|--------:|----------:|-----------------:|---------|--------|
| foundation | [secret1mce…cryk](https://testnet.ping.pub/secret/account/secret1mcevwvr8n3nkhv35dmwkm7zfqpqyypdn7hcryk) | ContinuousVesting | 299,000,001 SCRT · 299,000,001,000,000 uscrt | 29,900,001 SCRT · 29,900,001,000,000 uscrt | 269,100,000 SCRT · 269,100,000,000,000 uscrt | ~60 mo | **PASS** |
| core_development | [secret1p3l…c8w9](https://testnet.ping.pub/secret/account/secret1p3l02sqqddt8n4kg9vs8qeq0hvmx4g6mpdc8w9) | ContinuousVesting | 299,000,001 SCRT · 299,000,001,000,000 uscrt | 29,900,001 SCRT · 29,900,001,000,000 uscrt | 269,100,000 SCRT · 269,100,000,000,000 uscrt | ~60 mo | **PASS** |
| advisors | [secret1d3n…d6lvz](https://testnet.ping.pub/secret/account/secret1d3n3cjkpwemav70xt5wtpmpt0nuy0jl2md6lvz) | PeriodicVesting | 72,000,001 SCRT · 72,000,001,000,000 uscrt | 1 SCRT · 1,000,000 uscrt | 72,000,000 SCRT · 72,000,000,000,000 uscrt | **16** × 4,500,000 SCRT | **PASS** |
| ecosystem_fund | [secret167e…ed8h](https://testnet.ping.pub/secret/account/secret167ewv0c5cg5k8xwjtmsr8stsxfgxpyc32qed8h) | BaseAccount | 178,000,001 SCRT · 178,000,001,000,000 uscrt | all liquid | — | — | **PASS** |
| research_development | [secret1dpp…rmxe](https://testnet.ping.pub/secret/account/secret1dpplrnf75kw7sfyv72msq9kua0qycpq55wrmxe) | ContinuousVesting | 72,000,001 SCRT · 72,000,001,000,000 uscrt | ≥ 7,200,001 SCRT (live ~7,203,065 SCRT) | 64,800,000 SCRT · 64,800,000,000,000 uscrt | ~10% liquid | **PASS** |
| builder_relayer_support | [secret1j8w…jk6n](https://testnet.ping.pub/secret/account/secret1j8w9qym77v787lec09c7gxn7pvvugrwkjyjk6n) | BaseAccount | 43,000,001 SCRT · 43,000,001,000,000 uscrt | all liquid | — | — | **PASS** |
| remediation | [secret1q8k…zjp9](https://testnet.ping.pub/secret/account/secret1q8ksklhfjc9rsduy6cw2nl2xfjsuczht4lzjp9) | ContinuousVesting | 44,000,001 SCRT · 44,000,001,000,000 uscrt | 13,200,001 SCRT · 13,200,001,000,000 uscrt | 30,800,000 SCRT · 30,800,000,000,000 uscrt | 30% liquid | **PASS** |
| validator_program | [secret1kt6…8jr7u](https://testnet.ping.pub/secret/account/secret1kt67upwgqhjgeh6smh4ey9mhgzrtv4wle8jr7u) | PeriodicVesting | 71,040,001 SCRT · 71,040,001,000,000 uscrt | 6,240,001 SCRT · 6,240,001,000,000 uscrt | 64,800,000 SCRT · 64,800,000,000,000 uscrt | **20** × 3,240,000 SCRT; 72M − 4×240k | **PASS** |

### Full addresses (copy / explorer)

| Role | Address | Explorer |
|------|---------|----------|
| foundation | `secret1mcevwvr8n3nkhv35dmwkm7zfqpqyypdn7hcryk` | [open](https://testnet.ping.pub/secret/account/secret1mcevwvr8n3nkhv35dmwkm7zfqpqyypdn7hcryk) |
| core_development | `secret1p3l02sqqddt8n4kg9vs8qeq0hvmx4g6mpdc8w9` | [open](https://testnet.ping.pub/secret/account/secret1p3l02sqqddt8n4kg9vs8qeq0hvmx4g6mpdc8w9) |
| advisors | `secret1d3n3cjkpwemav70xt5wtpmpt0nuy0jl2md6lvz` | [open](https://testnet.ping.pub/secret/account/secret1d3n3cjkpwemav70xt5wtpmpt0nuy0jl2md6lvz) |
| ecosystem_fund | `secret167ewv0c5cg5k8xwjtmsr8stsxfgxpyc32qed8h` | [open](https://testnet.ping.pub/secret/account/secret167ewv0c5cg5k8xwjtmsr8stsxfgxpyc32qed8h) |
| research_development | `secret1dpplrnf75kw7sfyv72msq9kua0qycpq55wrmxe` | [open](https://testnet.ping.pub/secret/account/secret1dpplrnf75kw7sfyv72msq9kua0qycpq55wrmxe) |
| builder_relayer_support | `secret1j8w9qym77v787lec09c7gxn7pvvugrwkjyjk6n` | [open](https://testnet.ping.pub/secret/account/secret1j8w9qym77v787lec09c7gxn7pvvugrwkjyjk6n) |
| remediation | `secret1q8ksklhfjc9rsduy6cw2nl2xfjsuczht4lzjp9` | [open](https://testnet.ping.pub/secret/account/secret1q8ksklhfjc9rsduy6cw2nl2xfjsuczht4lzjp9) |
| validator_program | `secret1kt67upwgqhjgeh6smh4ey9mhgzrtv4wle8jr7u` | [open](https://testnet.ping.pub/secret/account/secret1kt67upwgqhjgeh6smh4ey9mhgzrtv4wle8jr7u) |

---

## Seats (4 paid · 240,000 SCRT each)

| Slot | Validator (moniker) | Operator | Payout (explorer) | Paid | Status |
|-----:|---------------------|----------|-------------------|-----:|--------|
| 1 | **SNF-B** | `secretvaloper17nl6x709q8wta9ja0qu0kduvfc70u8fskhkkve` | [secret1qrn…ayr4](https://testnet.ping.pub/secret/account/secret1qrn9yzzhea8g94r839q2dv8aq9wy6s9g99ayr4) | 240,000 SCRT · 240,000,000,000 uscrt | **PASS** |
| 2 | **SNF-D** | `secretvaloper1fj9zf2l6xlashn3sqcvuj7lythahg3f2v9mx2z` | [secret1sh5…vyuz](https://testnet.ping.pub/secret/account/secret1sh5zgy007t62upyd4vgegq69qeffqu5uznvyuz) | 240,000 SCRT · 240,000,000,000 uscrt | **PASS** |
| 3 | **SNF-A** | `secretvaloper1lgre6vtvntv75z6zvghkpwy4m953u4a7dc8e6e` | [secret1770…yxmu](https://testnet.ping.pub/secret/account/secret1770dqwlmmnt74588pvj8uqevhx9hjaalngyxmu) | 240,000 SCRT · 240,000,000,000 uscrt | **PASS** |
| 4 | **SNF-E** | `secretvaloper1ncfe2lwe0hm4fcjj3ma8nw9xxy95e7dvl4ppyj` | [secret19x0…rw5h](https://testnet.ping.pub/secret/account/secret19x0u83dfynl0sm8evt6l85cg668fttyf7frw5h) | 240,000 SCRT · 240,000,000,000 uscrt | **PASS** |
| | **Total paid** | | | **960,000 SCRT** · **960,000,000,000 uscrt** | **PASS** |

**Not seated (no Continuance seat payout):** **SNF-C** · `secretvaloper1pzltl7rn7fyl3grk24m5lsk3kvpeta0tm3k6m5`

**Ghost seats (slots 5–30):** 26 keyless placeholders — expected **SKIPPED** at upgrade (no 240k send). Program retains those reserves (spendable **6,240,001 SCRT** = 26 × 240k + 1 SCRT seed).

| Slot | Moniker | Operator | Payout address | Explorer |
|-----:|---------|----------|----------------|----------|
| 1 | SNF-B | `secretvaloper17nl6x709q8wta9ja0qu0kduvfc70u8fskhkkve` | `secret1qrn9yzzhea8g94r839q2dv8aq9wy6s9g99ayr4` | [open](https://testnet.ping.pub/secret/account/secret1qrn9yzzhea8g94r839q2dv8aq9wy6s9g99ayr4) |
| 2 | SNF-D | `secretvaloper1fj9zf2l6xlashn3sqcvuj7lythahg3f2v9mx2z` | `secret1sh5zgy007t62upyd4vgegq69qeffqu5uznvyuz` | [open](https://testnet.ping.pub/secret/account/secret1sh5zgy007t62upyd4vgegq69qeffqu5uznvyuz) |
| 3 | SNF-A | `secretvaloper1lgre6vtvntv75z6zvghkpwy4m953u4a7dc8e6e` | `secret1770dqwlmmnt74588pvj8uqevhx9hjaalngyxmu` | [open](https://testnet.ping.pub/secret/account/secret1770dqwlmmnt74588pvj8uqevhx9hjaalngyxmu) |
| 4 | SNF-E | `secretvaloper1ncfe2lwe0hm4fcjj3ma8nw9xxy95e7dvl4ppyj` | `secret19x0u83dfynl0sm8evt6l85cg668fttyf7frw5h` | [open](https://testnet.ping.pub/secret/account/secret19x0u83dfynl0sm8evt6l85cg668fttyf7frw5h) |

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

```bash
curl -fsSL -o install.sh \
  https://raw.githubusercontent.com/Secret3dev/SecretNetwork/v1.26.0-community-continuance/scripts/install-continuance-testnet.sh
chmod +x install.sh
sudo ./install.sh
```

Do not use `--unsafe-skip-upgrades`.

---

## Conclusion

On **pulsar-3**, Continuance executed at height **3,198,416**. Binary **1.26.0**, enclave unchanged, inflation **5%**, foundation tax **0**, mint **1,079,000,000 SCRT**, all eight buckets and four seats match design (including vesting types, original_vesting, spendable, and period schedules).

**Testnet Continuance: verified successful.**
