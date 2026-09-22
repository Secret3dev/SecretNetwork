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

That command waits until the collector is serving the combined file, writes it to `/opt/secret/.sgx_secrets/migration_consensus.json`, then runs the handover. The node stays up while it waits.

Track the upgrade at https://secretnodes.com/secret-4/upgrade/secret-4-v1.27.2

`sign` sends this node's signature to https://upgrade.secret3.dev for `secret-4-v1.27.2`.

### Optional

Leave these unset. The command above already does the upgrade.

**Node home**

`SECRETD_HOME` is the directory that contains `data/upgrade-info.json`. The script checks:

- `$HOME/.secretd`
- `/home/ubuntu/.secretd`
- `/root/.secretd`
- `/opt/secret/.secretd`

If more than one of these has the file, the script stops and lists them. Set `SECRETD_HOME` to the home you want.

```bash
cd mainnet && SECRETD_HOME=/home/secret/.secretd I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./autopilot.sh install
```

If the unit has `--home /home/secret`, set `SECRETD_HOME=/home/secret`.

**Systemd unit**

`dpkg` replaces `/etc/systemd/system/secret-node.service`. Save your unit first, then set `SERVICE_UNIT_FILE` to that copy. The script checks the copy before it stops the node, and puts it back before start.

The unit name is `secret-node`. Set `SERVICE` if the name is different.

### By hand

Doing this by hand takes longer. Emergency signers who still need to coordinate signatures should use `./autopilot.sh sign` and the install command above.

The node has halted. `secretd` is `1.26.0`. Do not delete `migration_consensus.json`. Do not install the package until after `check-hw --migrate_op 3`.

Pull the combined file. The collector serves it once 7 signatures are in, and keeps serving it until this upgrade is marked done. If this fails, it prints the collector reply. `have` is how many signatures are in. `need` is 7. Stop here. The node is still up.

```bash
rm -f /tmp/migration_consensus.json
code=$(curl -sS -o /tmp/migration_consensus.json -w '%{http_code}' --max-time 20 https://upgrade.secret3.dev/v1/upgrades/secret-4-v1.27.2/consensus) || true
if [ "$code" = 200 ]; then
  sudo cp /tmp/migration_consensus.json /opt/secret/.sgx_secrets/migration_consensus.json
else
  echo "combined file is not ready (HTTP $code)"
  cat /tmp/migration_consensus.json
  echo
  exit 1
fi
```

Run this from the `mainnet` directory, and only after that file is on disk. `27286266` is the height in `upgrade-info.json`. On Ubuntu 24.04 use the `ubuntu-24.04` package instead of the one below. `check-hw` has to be run from a directory that contains `check_hw_enclave.so`.

```bash
test -s /opt/secret/.sgx_secrets/migration_consensus.json || exit 1
cd check-hw || exit 1
sudo systemctl stop secret-node

export SCRT_SGX_STORAGE=/opt/secret/.sgx_secrets
export EXTRA_HEIGHT=27286266

find $SCRT_SGX_STORAGE -maxdepth 1 -name 'migration_*' ! -name 'migration_consensus.json' -delete

secretd migrate_op 5 || exit 1

dpkg-deb -x ../ubuntu-22.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-22.04.deb /tmp/sn127
cp /tmp/sn127/usr/lib/librust_cosmwasm_enclave.signed.so ./check_hw_enclave.so
./check-hw --migrate_op 1
secretd migrate_op 2 || exit 1
echo "$EXTRA_HEIGHT" > $SCRT_SGX_STORAGE/halt_height || exit 1
./check-hw --migrate_op 3 || exit 1

sudo dpkg -i ../ubuntu-22.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-22.04.deb || exit 1
```

If `dpkg` replaced a customized `/etc/systemd/system/secret-node.service`, copy your backup back and run `sudo systemctl daemon-reload` before start.

```bash
test "$(secretd version | head -1)" = 1.27.2 || exit 1
sudo systemctl start secret-node
```

The collector marks one upgrade `current` per network, by hand. `autopilot.sh` runs only when `secret-4-v1.27.2` is that current upgrade and the measurement matches this package. After the upgrade is marked `done`, the script exits and the collector stops serving the combined file. `testnet/halt.sh` exits the same way once `trinity-b-v1.27.0-r10` is marked done.

Proposal 377 is submitted. Plan `v1.27.2` at height `27286266`.
