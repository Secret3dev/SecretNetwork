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

Leave the optional variables unset and that command is unchanged.

`SECRETD_HOME` is the node home, the directory that contains `data/upgrade-info.json`. Unset, the script reads the first file it finds under `$HOME/.secretd`, `/home/ubuntu/.secretd`, `/root/.secretd`, or `/opt/secret/.secretd`. Set it when the data is somewhere else. If the unit's `ExecStart` has `--home /home/secret`, the file is `/home/secret/data/upgrade-info.json` and `SECRETD_HOME=/home/secret`. The usual home for the `secret` user is `/home/secret/.secretd`.

```bash
cd mainnet && SECRETD_HOME=/home/secret/.secretd I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./autopilot.sh install
```

`SERVICE_UNIT_FILE` is a backup of the systemd unit, saved before `dpkg`. The package postinst overwrites `/etc/systemd/system/secret-node.service`. When this is set, the script checks the backup before it stops the node. After the package install it copies that backup to `/etc/systemd/system/$SERVICE.service`, reloads systemd, and then starts the node. The unit name defaults to `secret-node`. Set `SERVICE` if the unit name is different.

Track the upgrade at https://secretnodes.com/secret-4/upgrade/secret-4-v1.27.2

`sign` sends this node's signature of the measurement to `https://upgrade.secret3.dev` for upgrade id `secret-4-v1.27.2`.

### Manual steps

The script does these in order. The handover runs on the installed `1.26.0` binary. The new package is installed only after the sealed file for the new measurement exists.

1. Confirm Ubuntu 22.04 or 24.04 and that the matching `.deb` is in this directory. The script extracts the enclave from that package and checks it against `H.txt`, so the package is the measurement the signers approved.
2. Read `data/upgrade-info.json`. The node writes it when it halts. The plan name has to be `v1.27.2`, and the height in the file is the height the handover stamps. With `SECRETD_HOME` set, only `$SECRETD_HOME/data/upgrade-info.json` is read, so an older file in another home is ignored.
3. Confirm the systemd unit exists and `ExecStart` has no `--bootstrap`. `--bootstrap` is for genesis and skips loading the seed.
4. Confirm `/opt/secret/.sgx_secrets/migration_consensus.json` is already on disk. That file is the signed handover. The script does not download it.
5. Set `I_UNDERSTAND=yes` and `I_CONFIRM_PRECHECK=yes`. If `SERVICE_UNIT_FILE` is set, that backup has to exist before the node is stopped. `secretd` must be `1.26.0`.
6. Stop the service.
7. Copy `migration_consensus.json` aside, delete every other `migration_*` file, and put the consensus file back. The handover then sees only the signed file.
8. Run `secretd migrate_op 5`, `check-hw --migrate_op 1`, `secretd migrate_op 2`, write `halt_height` to the plan height, then `check-hw --migrate_op 3`. This reseals the enclave for the new measurement and records the halt height. The new package is not installed yet.
9. Confirm `data-<measurement>.bin` exists under `/opt/secret/.sgx_secrets`.
10. `dpkg -i` the package. Confirm `secretd` is `1.27.2` and the installed enclave matches `H.txt`.
11. If `SERVICE_UNIT_FILE` is set, copy that backup onto `/etc/systemd/system/$SERVICE.service` and reload systemd. The restored unit must not contain `--bootstrap` or `--unsafe-skip-upgrades`.
12. Enable and start the service.

The collector marks one upgrade `current` per network, by hand. `autopilot.sh` runs only when `secret-4-v1.27.2` is that current upgrade and the measurement matches this package. After the upgrade is marked `done`, the script exits and the collector stops serving the combined file. `testnet/halt.sh` exits the same way once `trinity-b-v1.27.0-r10` is marked done.

Proposal 377 is submitted. Plan `v1.27.2` at height `27286266`.
