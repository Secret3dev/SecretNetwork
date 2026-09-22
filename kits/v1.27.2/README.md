# 1.27.2 packages

Two networks. Each has an Ubuntu 22.04 package and an Ubuntu 24.04 package. The signed enclave in the 22.04 package is the same measurement as the 24.04 package for that network.

## Testnet

Chain `trinity-b`. Plan `v1.27.2`. Measurement is `testnet/H.txt`.

A node that is already on the upgraded chain installs the package for its Ubuntu version:

```bash
cd testnet
sudo ./install.sh
```

A node still running `1.27.0` that has halted on plan `v1.27.2` installs without a handover. The enclave measurement does not change.

```bash
cd testnet
I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./halt.sh --install
```

## Mainnet

Chain `secret-4`. Plan `v1.27.2`. The node must be running `1.26.0`. Measurement is `mainnet/H.txt`.

Do not install the package before the node halts. The handover runs first, then the package is installed.

```bash
cd mainnet && I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./autopilot.sh install
```

Track the upgrade at https://secretnodes.com/secret-4/upgrade/secret-4-v1.27.2

`sign` sends this node's signature to https://upgrade.secret3.dev for `secret-4-v1.27.2`.

### Optional

Leave these unset. The command above already does the upgrade.

**Node home**

`SECRETD_HOME` is the directory that contains `data/upgrade-info.json`. The script already checks:

- `$HOME/.secretd`
- `/home/ubuntu/.secretd`
- `/root/.secretd`
- `/opt/secret/.secretd`

```bash
cd mainnet && SECRETD_HOME=/home/secret/.secretd I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./autopilot.sh install
```

If the unit has `--home /home/secret`, set `SECRETD_HOME=/home/secret`.

**Systemd unit**

`dpkg` replaces `/etc/systemd/system/secret-node.service`. Save your unit first, then set `SERVICE_UNIT_FILE` to that copy. The script checks the copy before it stops the node, and puts it back before start.

The unit name is `secret-node`. Set `SERVICE` if the name is different.

### Manual steps

The handover runs on `1.26.0`. The package is installed after the new sealed file exists.

1. Confirm Ubuntu 22.04 or 24.04, and that the package enclave matches `H.txt`.
2. Read `data/upgrade-info.json`. The plan name must be `v1.27.2`. That height is what the handover stamps.
3. Confirm the systemd unit exists and has no `--bootstrap`.
4. Confirm `/opt/secret/.sgx_secrets/migration_consensus.json` is already there. The script does not download it.
5. Confirm `secretd` is `1.26.0`.
6. Stop the node.
7. Keep `migration_consensus.json`. Remove every other `migration_*` file.
8. On `1.26.0`: `migrate_op 5`, `check-hw 1`, `migrate_op 2`, write `halt_height`, `check-hw 3`.
9. Confirm the new sealed file exists, then `dpkg -i` the package.
10. Confirm `secretd` is `1.27.2` and the installed enclave matches `H.txt`.
11. If a unit backup was set, put it back. It must not contain `--bootstrap` or `--unsafe-skip-upgrades`.
12. Start the node.

The collector marks one upgrade `current` per network, by hand. `autopilot.sh` runs only when `secret-4-v1.27.2` is that current upgrade and the measurement matches this package. After the upgrade is marked `done`, the script exits and the collector stops serving the combined file. `testnet/halt.sh` exits the same way once `trinity-b-v1.27.0-r10` is marked done.

Proposal 377 is submitted. Plan `v1.27.2` at height `27286266`.
