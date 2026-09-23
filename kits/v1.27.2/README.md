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

`sign` sends this node's signature to https://upgrade.secret3.dev for `secret-4-v1.27.2`. It reads `$HOME/.secretd`. `SECRETD_HOME` does not change that.

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

Doing this by hand takes longer. Emergency signers who still need to coordinate signatures should use `./autopilot.sh sign` and the install command above. Each block is a subshell. A failure stops that block and leaves the SSH session open.

Get the kit onto this machine. Run this from any directory. It writes `secret-4-v1.27.2/` with the scripts, the checksum file, and the mainnet binaries, then checks those three binaries. A failed download stops the block. The shell stays open. The node stays up.

```bash
(
set -e
kit=secret-4-v1.27.2
base=https://raw.githubusercontent.com/Secret3dev/SecretNetwork/secretcommunity-release-1/kits/v1.27.2
mkdir -p "$kit/mainnet/ubuntu-22.04" "$kit/mainnet/ubuntu-24.04" "$kit/mainnet/check-hw" "$kit/mainnet/catalog" "$kit/testnet"
fetch() { curl -fL --retry 3 -o "$kit/$1" "$base/$1"; }
fetch SHA256SUMS
fetch README.md
fetch mainnet/H.txt
fetch mainnet/SUBMIT.md
fetch mainnet/proposal.json
fetch mainnet/autopilot.sh
fetch mainnet/halt.sh
fetch mainnet/catalog/after.txt
fetch mainnet/catalog/allowlist.txt
fetch mainnet/catalog/meta.json
fetch mainnet/catalog/removed.txt
fetch mainnet/check-hw/check-hw
fetch mainnet/ubuntu-22.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-22.04.deb
fetch mainnet/ubuntu-24.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-24.04.deb
fetch testnet/H.txt
fetch testnet/halt.sh
fetch testnet/install.sh
chmod +x "$kit/mainnet/autopilot.sh" "$kit/mainnet/halt.sh" "$kit/mainnet/check-hw/check-hw" "$kit/testnet/halt.sh" "$kit/testnet/install.sh"
( cd "$kit" && set -o pipefail && grep '  mainnet/' SHA256SUMS | sha256sum -c - )
)
```

Sign from that kit. This does not stop the node.

```bash
cd secret-4-v1.27.2/mainnet && ./autopilot.sh sign
```

The node has halted. `secretd` is `1.26.0`. Do not delete `migration_consensus.json`. Do not install the package until after `check-hw --migrate_op 3`.

Pull the combined file. The collector serves it once 7 signatures are in, and keeps serving it until this upgrade is marked done. If this fails, it prints the collector reply. `have` is how many signatures are in. `need` is 7. A 200 body that is not the address map is not copied. If the unit sets `SCRT_SGX_STORAGE`, export that same path before this block and before the handover. Stop here. The node is still up.

```bash
(
set -e
export SCRT_SGX_STORAGE="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
rm -f /tmp/migration_consensus.json
code=$(curl -sS -o /tmp/migration_consensus.json -w '%{http_code}' --max-time 20 https://upgrade.secret3.dev/v1/upgrades/secret-4-v1.27.2/consensus) || true
if [ "$code" = 200 ]; then
  python3 -c 'import json,re,sys; d=json.load(open(sys.argv[1])); assert isinstance(d, dict) and d
for k,v in d.items():
    assert re.fullmatch(r"[0-9A-F]{40}", k)
    assert isinstance(v, list) and len(v)==2 and all(isinstance(x, str) and x for x in v)' /tmp/migration_consensus.json
  sudo mkdir -p "$SCRT_SGX_STORAGE"
  sudo cp /tmp/migration_consensus.json "$SCRT_SGX_STORAGE/migration_consensus.json"
  sudo chown "$(id -u):$(id -g)" "$SCRT_SGX_STORAGE/migration_consensus.json"
else
  echo "combined file is not ready (HTTP $code)"
  cat /tmp/migration_consensus.json
  echo
  false
fi
)
```

Run this from the `mainnet` directory, and only after that file is readable. `27286266` is the height in `upgrade-info.json`. The package follows this machine's Ubuntu version. `check-hw` has to be run from a directory that contains `check_hw_enclave.so`.

```bash
(
set -e
export SCRT_SGX_STORAGE="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
test -r "$SCRT_SGX_STORAGE/migration_consensus.json"

. /etc/os-release
case "$VERSION_ID" in 22.04|24.04) ;; *) false ;; esac
deb="ubuntu-${VERSION_ID}/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-${VERSION_ID}.deb"
test -s "$deb"

cd check-hw
sudo systemctl stop secret-node

export EXTRA_HEIGHT=27286266
# Deletes other migration_* files. Leaves migration_consensus.json.
sudo find "$SCRT_SGX_STORAGE" -maxdepth 1 -name 'migration_*' ! -name 'migration_consensus.json' -delete

secretd migrate_op 5
# Unpacks the package so the next line can copy the signed enclave. Does not install.
dpkg-deb -x "../$deb" /tmp/sn127
cp /tmp/sn127/usr/lib/librust_cosmwasm_enclave.signed.so ./check_hw_enclave.so
# Writes migration_report_local.bin. Stop if that file is missing. migrate_op 2 cannot export without it.
./check-hw --migrate_op 1 || sudo test -s "$SCRT_SGX_STORAGE/migration_report_local.bin"
secretd migrate_op 2
echo "$EXTRA_HEIGHT" > "$SCRT_SGX_STORAGE/halt_height"
./check-hw --migrate_op 3
# Installs the package. This line does not run when check-hw 3 fails.
sudo dpkg -i "../$deb"
)
```

If `dpkg` replaced a customized unit, copy that backup back and run `sudo systemctl daemon-reload` before start.

```bash
(
set -e
test "$(secretd version | head -1)" = 1.27.2
sudo systemctl start secret-node
)
```

The collector marks one upgrade `current` per network, by hand. `autopilot.sh` runs only when `secret-4-v1.27.2` is that current upgrade and the measurement matches this package. After the upgrade is marked `done`, the script exits and the collector stops serving the combined file. `testnet/halt.sh` exits the same way once `trinity-b-v1.27.0-r10` is marked done.

Proposal 377 is submitted. Plan `v1.27.2` at height `27286266`.
