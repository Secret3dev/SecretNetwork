# app/upgrades/v1.26 — Community Continuance

On-chain upgrade handler for plan name **`v1.26.0-community-continuance`**.

## What it does

- Mints **1,079,000,000 SCRT** once at the upgrade height
- Distributes to the eight proposal buckets with the configured vesting shapes
- Pays eligible validator seats from the program bucket (vacant seats keep funds at program)
- Pins inflation at **5%** and sets Secret foundation tax to **0**
- Selects addresses by **chain ID** at runtime (`pulsar-3`, `secret-4`, `secretdev-1`)

## Layout

| Path | Role |
|------|------|
| `upgrade.go` | Handler: mint, vest, seats, policy |
| `config/` | Allocations, address sets, `Validate()` release gate |
| `*_test.go` | Unit + handler tests (`-tags secretcli` for handler package) |

## Networks

- **Testnet (`pulsar-3`)** — filled addresses for Continuance testnet
- **Mainnet (`secret-4`)** — placeholders until mainnet cut is intentional
- **LocalSecret (`secretdev-1`)** — local rehearsal only

## Build note

This package is host-only Go. The enclave is **not** rebuilt for this upgrade;
release packages must ship the same enclave measurement the target network already runs.
