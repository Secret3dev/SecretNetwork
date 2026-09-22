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

Doing this by hand takes longer. Emergency signers who still need to coordinate signatures should use `./autopilot.sh sign` and the install command above.

Get the binaries into this directory before the halt. Run this from `mainnet`. If a download fails, stop. The node is still up. These commands do not install the package. The last line checks the three mainnet files in `SHA256SUMS`. A missing file fails.

```bash
mkdir -p ubuntu-22.04 ubuntu-24.04 check-hw
base=https://raw.githubusercontent.com/Secret3dev/SecretNetwork/secretcommunity-release-1/kits/v1.27.2/mainnet
curl -fL --retry 3 -o ubuntu-22.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-22.04.deb "$base/ubuntu-22.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-22.04.deb" || exit 1
curl -fL --retry 3 -o ubuntu-24.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-24.04.deb "$base/ubuntu-24.04/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-24.04.deb" || exit 1
curl -fL --retry 3 -o check-hw/check-hw "$base/check-hw/check-hw" || exit 1
chmod +x check-hw/check-hw
( cd .. && set -o pipefail && grep '  mainnet/' SHA256SUMS | sha256sum -c - ) || exit 1
```

The node has halted. `secretd` is `1.26.0`. Do not delete `migration_consensus.json`. Do not install the package until after `check-hw --migrate_op 3`.

Pull the combined file. The collector serves it once 7 signatures are in, and keeps serving it until this upgrade is marked done. If this fails, it prints the collector reply. `have` is how many signatures are in. `need` is 7. A 200 body that is not the address map is not copied. If the unit sets `SCRT_SGX_STORAGE`, export that same path before this block and before the handover. Stop here. The node is still up.

```bash
export SCRT_SGX_STORAGE="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
rm -f /tmp/migration_consensus.json
code=$(curl -sS -o /tmp/migration_consensus.json -w '%{http_code}' --max-time 20 https://upgrade.secret3.dev/v1/upgrades/secret-4-v1.27.2/consensus) || true
if [ "$code" = 200 ]; then
  python3 -c 'import json,re,sys; d=json.load(open(sys.argv[1])); assert isinstance(d, dict) and d
for k,v in d.items():
    assert re.fullmatch(r"[0-9A-F]{40}", k)
    assert isinstance(v, list) and len(v)==2 and all(isinstance(x, str) and x for x in v)' /tmp/migration_consensus.json || exit 1
  sudo mkdir -p "$SCRT_SGX_STORAGE"
  sudo cp /tmp/migration_consensus.json "$SCRT_SGX_STORAGE/migration_consensus.json" || exit 1
  sudo chown "$(id -u):$(id -g)" "$SCRT_SGX_STORAGE/migration_consensus.json" || exit 1
else
  echo "combined file is not ready (HTTP $code)"
  cat /tmp/migration_consensus.json
  echo
  exit 1
fi
```

Run this from the `mainnet` directory, and only after that file is readable. The height comes from `upgrade-info.json`. The node is not stopped until that file is the only one, the plan name is `v1.27.2`, and `secretd` is `1.26.0`. The package is the one for this Ubuntu version. `check-hw` has to be run from a directory that contains `check_hw_enclave.so`. Save a customized unit first and set `SERVICE_UNIT_FILE` to that copy. The unit name is `secret-node`. Set `SERVICE` if the name is different.

```bash
export SERVICE="${SERVICE:-secret-node}"
export SCRT_SGX_STORAGE="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
test -r "$SCRT_SGX_STORAGE/migration_consensus.json" || exit 1
test "$(secretd version | head -1)" = 1.26.0 || exit 1
if systemctl show -p ExecStart "$SERVICE" | grep -q -- '--bootstrap'; then exit 1; fi
if systemctl show -p ExecStart "$SERVICE" | grep -q -- 'unsafe-skip-upgrades'; then exit 1; fi

unit_store="$(systemctl show -p Environment --value "$SERVICE" | tr ' ' '\n' | sed -n 's/^SCRT_SGX_STORAGE=//p' | tail -1)"
if [ -n "$unit_store" ] && [ "$unit_store" != "$SCRT_SGX_STORAGE" ]; then
  echo "unit SCRT_SGX_STORAGE=$unit_store"
  exit 1
fi

EXTRA_HEIGHT="$(python3 - <<'PY'
import json, os, sys
home = os.environ.get("HOME", "")
secretd_home = os.environ.get("SECRETD_HOME", "")
def load(path):
    try:
        return json.load(open(path))
    except Exception:
        return None
def plan_of(data):
    plan = data.get("plan") or data
    name = plan.get("name") or data.get("name") or ""
    height = plan.get("height")
    if height in (None, ""):
        height = data.get("height") or ""
    return str(name), str(height)
if secretd_home:
    paths = [os.path.join(secretd_home, "data/upgrade-info.json")]
else:
    paths = [
        os.path.join(home, ".secretd/data/upgrade-info.json") if home else "",
        "/home/ubuntu/.secretd/data/upgrade-info.json",
        "/root/.secretd/data/upgrade-info.json",
        "/opt/secret/.secretd/data/upgrade-info.json",
    ]
seen, found = [], []
for path in paths:
    if not path or not os.path.isfile(path) or os.path.getsize(path) == 0:
        continue
    real = os.path.realpath(path)
    if real in seen:
        continue
    seen.append(real)
    found.append(path)
if len(found) > 1:
    sys.stderr.write("upgrade-info.json is in more than one home. Set SECRETD_HOME to one:\n")
    for path in found:
        sys.stderr.write("  %s\n" % path[:-len("/data/upgrade-info.json")])
    sys.exit(1)
if len(found) != 1:
    sys.stderr.write("node has no upgrade-info.json\n")
    sys.exit(1)
name, height = plan_of(load(found[0]))
if name != "v1.27.2" or not height.isdigit() or int(height) <= 0:
    sys.stderr.write("upgrade-info is %s %s\n" % (name, height))
    sys.exit(1)
if secretd_home:
    hw_paths = []
    if home:
        hw_paths.append(os.path.join(home, ".secretd/data/upgrade-info.json"))
    hw_paths.extend(["/root/.secretd/data/upgrade-info.json", "/opt/secret/.secretd/data/upgrade-info.json"])
    seen_hw, hw_height = [], ""
    for path in hw_paths:
        if path in seen_hw:
            continue
        seen_hw.append(path)
        data = load(path)
        if not isinstance(data, dict):
            continue
        h = data.get("height")
        if isinstance(h, bool):
            continue
        if isinstance(h, int) and h > 0:
            hw_height = str(h)
            break
        if isinstance(h, str) and h.isdigit() and int(h) > 0:
            hw_height = str(int(h))
            break
    if hw_height and hw_height != height:
        sys.stderr.write("check-hw would use upgrade-info height %s, not %s\n" % (hw_height, height))
        sys.exit(1)
print(height)
PY
)" || exit 1

. /etc/os-release
case "$VERSION_ID" in 22.04|24.04) ;; *) exit 1 ;; esac
deb="ubuntu-${VERSION_ID}/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-${VERSION_ID}.deb"
test -s "$deb" || exit 1

cd check-hw || exit 1
sudo systemctl stop "$SERVICE" || exit 1

sudo find "$SCRT_SGX_STORAGE" -maxdepth 1 -name 'migration_*' ! -name 'migration_consensus.json' -delete || exit 1
if sudo test -e "$SCRT_SGX_STORAGE/migration_report_local.bin"; then exit 1; fi

secretd migrate_op 5 || exit 1

rm -rf /tmp/sn127
dpkg-deb -x "../$deb" /tmp/sn127 || exit 1
cp /tmp/sn127/usr/lib/librust_cosmwasm_enclave.signed.so ./check_hw_enclave.so || exit 1
./check-hw --migrate_op 1 || sudo test -s "$SCRT_SGX_STORAGE/migration_report_local.bin" || exit 1

op2="$(mktemp)"
secretd migrate_op 2 >"$op2" 2>&1 || { cat "$op2"; exit 1; }
cat "$op2"
if grep -F -q "Migration is authorized by on-chain consensus" "$op2"; then exit 1; fi
grep -F -q "Migration is authorized by off-chain (emergency) consensus" "$op2" || exit 1
grep -F -q "Emergency threshold reached: true" "$op2" || exit 1

echo "$EXTRA_HEIGHT" > "$SCRT_SGX_STORAGE/halt_height" || exit 1
op3="$(mktemp)"
./check-hw --migrate_op 3 >"$op3" 2>&1 || { cat "$op3"; exit 1; }
cat "$op3"
if grep -q "stamped random_proof_hstar=${EXTRA_HEIGHT}" "$op3"; then
  :
elif grep -q "already set" "$op3" && grep -q "random_proof_hstar=${EXTRA_HEIGHT}" "$op3"; then
  :
else
  exit 1
fi
h="$(tr -d '[:space:]' < ../H.txt)" || exit 1
test -s "$SCRT_SGX_STORAGE/data-${h}.bin" || exit 1

sudo dpkg -i "../$deb" || exit 1
```

Set `SERVICE_UNIT_FILE` to the unit copy you saved before `dpkg`. Leave it unset and the package unit is what starts. Start runs only when `secretd` is `1.27.2` and the unit has no `--bootstrap` or `--unsafe-skip-upgrades`.

```bash
export SERVICE="${SERVICE:-secret-node}"
if [ -n "${SERVICE_UNIT_FILE:-}" ]; then
  test -s "$SERVICE_UNIT_FILE" || exit 1
  sudo cp -a "$SERVICE_UNIT_FILE" "/etc/systemd/system/${SERVICE}.service" || exit 1
  sudo systemctl daemon-reload || exit 1
fi
if systemctl show -p ExecStart "$SERVICE" | grep -q -- '--bootstrap'; then exit 1; fi
if systemctl show -p ExecStart "$SERVICE" | grep -q -- 'unsafe-skip-upgrades'; then exit 1; fi
test "$(secretd version | head -1)" = 1.27.2 || exit 1
sudo systemctl start "$SERVICE" || exit 1
```

The collector marks one upgrade `current` per network, by hand. `autopilot.sh` runs only when `secret-4-v1.27.2` is that current upgrade and the measurement matches this package. After the upgrade is marked `done`, the script exits and the collector stops serving the combined file. `testnet/halt.sh` exits the same way once `trinity-b-v1.27.0-r10` is marked done.

Proposal 377 is submitted. Plan `v1.27.2` at height `27286266`.
