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

Doing this by hand takes longer. Emergency signers who still need to coordinate signatures should use `./autopilot.sh sign` and the install command above. This section is not for them.

Stay in one shell, in the `mainnet` directory. If a command fails, stop. Do not start the node. Do not install the package before the handover finishes. Do not pass `--unsafe-skip-upgrades`.

The unit name below is `secret-node`. Use your unit name if it is different.

1. Confirm Ubuntu 22.04 or 24.04 and that this directory has the matching package.

```bash
set -euo pipefail
. /etc/os-release
case "$VERSION_ID" in 22.04|24.04) ;; *) echo "need Ubuntu 22.04 or 24.04"; exit 1 ;; esac
DEB="ubuntu-${VERSION_ID}/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-${VERSION_ID}.deb"
test -s "$DEB"
```

2. The enclave inside that package must match `H.txt`.

```bash
pkg=$(mktemp -d)
dpkg-deb -x "$DEB" "$pkg"
SO="$pkg/usr/lib/librust_cosmwasm_enclave.signed.so"
got=$(python3 - "$SO" <<'PY'
import binascii, sys
HDR = bytes.fromhex("06000000e10000000000010000000000")
d = open(sys.argv[1], "rb").read()
i = d.find(HDR)
if i < 0 or i + 992 > len(d):
    sys.exit(2)
print(binascii.hexlify(d[i + 960:i + 992]).decode())
PY
)
expect=$(tr -d '[:space:]' < H.txt)
echo "$got"
echo "$expect"
test "$got" = "$expect"
```

3. Read the halt file from the node home. The name must be `v1.27.2`. That height is what the handover stamps. If more than one home has this file, set `INFO` to the one for this node and do not use the other.

```bash
INFO="${SECRETD_HOME:-$HOME/.secretd}/data/upgrade-info.json"
PLAN_HEIGHT=$(python3 - "$INFO" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
plan = d.get("plan") or d
name = plan.get("name") or d.get("name") or ""
height = plan.get("height") or d.get("height") or ""
if name != "v1.27.2":
    sys.exit("plan name is %s" % name)
print(height)
PY
)
export PLAN_HEIGHT EXTRA_HEIGHT="$PLAN_HEIGHT"
export SCRT_SGX_STORAGE="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
echo "$PLAN_HEIGHT"
```

A unit with `--home /home/secret` uses `INFO=/home/secret/data/upgrade-info.json`. The usual `secret` user home uses `INFO=/home/secret/.secretd/data/upgrade-info.json`.

4. The unit must exist and must not use `--bootstrap`. The signed handover file must already be on disk. This does not download it.

```bash
systemctl cat secret-node >/dev/null
if systemctl show -p ExecStart secret-node | grep -q -- '--bootstrap'; then
  echo "unit has --bootstrap"
  exit 1
fi
test -s "$SCRT_SGX_STORAGE/migration_consensus.json"
```

5. If this unit was customized, save it now. `dpkg` replaces `/etc/systemd/system/secret-node.service`.

```bash
sudo cp -a /etc/systemd/system/secret-node.service /root/secret-node.service.pre-v1.27.2
```

6. Confirm the installed binary is still `1.26.0`, then stop the node.

```bash
secretd version | head -1
sudo systemctl stop secret-node
```

The first line must be `1.26.0`.

7. Keep `migration_consensus.json`. Remove every other `migration_*` file.

```bash
bak=$(mktemp)
cp -a "$SCRT_SGX_STORAGE/migration_consensus.json" "$bak"
find "$SCRT_SGX_STORAGE" -maxdepth 1 -name 'migration_*' ! -name 'migration_consensus.json' -delete
cp -a "$bak" "$SCRT_SGX_STORAGE/migration_consensus.json"
```

8. Handover, still on `1.26.0`. `check-hw` loads `check_hw_enclave.so` from the directory it is run in. That file is the enclave from the new package.

```bash
work=$(mktemp -d /tmp/checkhw-XXXXXX)
cp -fL "$SO" "$work/check_hw_enclave.so"
hw=$(readlink -f check-hw/check-hw)
secretd migrate_op 5
( cd "$work" && "$hw" --migrate_op 1 ) || test -s "$SCRT_SGX_STORAGE/migration_report_local.bin"
secretd migrate_op 2
printf '%s\n' "$PLAN_HEIGHT" > "$SCRT_SGX_STORAGE/halt_height"
( cd "$work" && "$hw" --migrate_op 3 )
```

Opcode 1 can exit non-zero and still be complete when `$SCRT_SGX_STORAGE/migration_report_local.bin` exists. Opcode 3 must print `stamped random_proof_hstar=$PLAN_HEIGHT`, or both `already set` and `random_proof_hstar=$PLAN_HEIGHT`.

`check-hw` also opens `upgrade-info.json` under `$HOME/.secretd`, `/root/.secretd`, and `/opt/secret/.secretd`. When one of those files exists, its height must equal `EXTRA_HEIGHT`.

9. The new sealed file must exist. Then install the package.

```bash
test -s "$SCRT_SGX_STORAGE/data-${expect}.bin"
sudo dpkg -i "$DEB"
secretd version | head -1
```

The first line must be `1.27.2`. The installed enclave must match `H.txt`.

```bash
inst=$(python3 - /usr/lib/librust_cosmwasm_enclave.signed.so <<'PY'
import binascii, sys
HDR = bytes.fromhex("06000000e10000000000010000000000")
d = open(sys.argv[1], "rb").read()
i = d.find(HDR)
if i < 0 or i + 992 > len(d):
    sys.exit(2)
print(binascii.hexlify(d[i + 960:i + 992]).decode())
PY
)
echo "$inst"
test "$inst" = "$expect"
```

10. If you saved the unit in step 5, put that file back before start. It must not contain `--bootstrap` or `--unsafe-skip-upgrades`.

```bash
sudo cp -a /root/secret-node.service.pre-v1.27.2 /etc/systemd/system/secret-node.service
sudo systemctl daemon-reload
systemctl show -p ExecStart secret-node
```

11. Start the node.

```bash
sudo systemctl enable secret-node
sudo systemctl start secret-node
```

The collector marks one upgrade `current` per network, by hand. `autopilot.sh` runs only when `secret-4-v1.27.2` is that current upgrade and the measurement matches this package. After the upgrade is marked `done`, the script exits and the collector stops serving the combined file. `testnet/halt.sh` exits the same way once `trinity-b-v1.27.0-r10` is marked done.

Proposal 377 is submitted. Plan `v1.27.2` at height `27286266`.
