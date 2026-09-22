# Submit the secret-4 v1.27.2 proposal

This has not been broadcast.

`proposal.json` is an expedited software-upgrade proposal.

- Plan name: `v1.27.2`
- Height: `27286266`
- That height is 8:30am Eastern on Wednesday 23 September 2026 (12:30 UTC).
- It was projected from mainnet height 27260562 at 2026-09-21 19:46 UTC, using 5.703 seconds per block over the previous 10,000 blocks.
- Deposit: `2500000000uscrt` (2,500 SCRT). That is the full expedited minimum, so voting starts immediately.
- Expedited voting lasts 24 hours. Submit before 8:30am Eastern on Tuesday 22 September 2026 so voting finishes before the halt.

From a machine with a mainnet key that can pay the deposit:

```bash
secretd tx gov submit-proposal proposal.json \
  --from <key-name> \
  --chain-id secret-4 \
  --node https://rpc.secret.mainnet.secret3.dev \
  --gas 400000 \
  --gas-prices 0.1uscrt \
  --yes
```

Do not add a second `--deposit`. The amount is inside `proposal.json`.
