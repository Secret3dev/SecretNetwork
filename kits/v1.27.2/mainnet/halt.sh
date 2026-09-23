#!/bin/bash
# secret-4 halt. Plan name v1.27.2. The running binary must be 1.26.0.
# Order: handover while the current package is still installed, then install
# the 1.27.2 package and start the node.
#
#   ./halt.sh
#   I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./halt.sh --install
#
# SERVICE defaults to secret-node. Set SERVICE if the unit has another name.
#
# SECRETD_HOME, when set, is the node home: the directory that contains
# data/upgrade-info.json. Unset, the four homes are checked. More than one
# distinct file stops the script so you can set SECRETD_HOME.
# SERVICE_UNIT_FILE, when set, is a backup of the systemd unit taken before
# this script runs. The package postinst overwrites
# /etc/systemd/system/secret-node.service. After dpkg that backup is copied to
# /etc/systemd/system/$SERVICE.service, systemd is reloaded, and then the node
# starts. Leave both unset and those steps do not run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
DO_INSTALL=0
for arg in "$@"; do
  case "$arg" in
    --install) DO_INSTALL=1 ;;
    --dry-run) DO_INSTALL=0 ;;
    -h|--help) sed -n '2,18p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *unsafe-skip-upgrades*|upgrade-proposal-passed)
      echo "FATAL: refusing $arg" >&2; exit 1 ;;
    *) echo "unknown arg: $arg" >&2; exit 1 ;;
  esac
done

CHAIN_ID="${CHAIN_ID:-secret-4}"
UPGRADE_NAME="${UPGRADE_NAME:-v1.27.2}"
FROM_VER="${FROM_VER:-1.26.0}"
SERVICE="${SERVICE:-secret-node}"
SCRT_SGX_STORAGE="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
ENCLAVE="${ENCLAVE:-/usr/lib/librust_cosmwasm_enclave.signed.so}"
CHECK_HW="${CHECK_HW:-$ROOT/check-hw/check-hw}"

die() { echo "FATAL: $*" >&2; exit 1; }
ok() { echo "OK  $*"; }

truthy_yes() {
  local v
  v="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  case "$v" in
    yes|true|1|y|on) return 0 ;;
    *) return 1 ;;
  esac
}

# secret-4 plan v1.27.2 only. SKIP_DEST3 would skip the handover.
if truthy_yes "${SKIP_DEST3:-}"; then
  die "this halt must run the handover. Do not set SKIP_DEST3"
fi
[[ "$CHAIN_ID" == "secret-4" ]] || die "CHAIN_ID=$CHAIN_ID (want secret-4)"
[[ "$UPGRADE_NAME" == "v1.27.2" ]] || die "UPGRADE_NAME=$UPGRADE_NAME (want v1.27.2)"

# Package must match this Ubuntu release, and the enclave inside it must match H.txt.
os_id="$(. /etc/os-release; printf '%s' "${VERSION_ID:-}")"
case "$os_id" in
  22.04|24.04) ;;
  *) die "need Ubuntu 22.04 or 24.04 (got ${os_id:-unknown})" ;;
esac
DEB="${DEB:-$ROOT/ubuntu-${os_id}/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-${os_id}.deb}"
[[ -s "$DEB" ]] || die "missing package $DEB"
[[ -x "$CHECK_HW" ]] || die "missing $CHECK_HW"

# SHA256SUMS is one directory above mainnet. Check the package this host will
# install, and the check-hw binary that will run. Another Ubuntu package is
# not required. A missing line or a different hash exits before the node stops.
sum_line_hash() {
  local rel="$1" sums="$ROOT/../SHA256SUMS" line count
  [[ -s "$sums" ]] || die "missing $sums"
  line="$(grep -F "  ${rel}" "$sums")" || die "SHA256SUMS has no line for ${rel}"
  count="$(printf '%s\n' "$line" | wc -l | tr -d ' ')"
  [[ "$count" == "1" ]] || die "SHA256SUMS has ${count} lines for ${rel}"
  printf '%s' "${line%% *}"
}
got="$(sha256sum "$DEB" | awk '{print $1}')"
want="$(sum_line_hash "mainnet/${DEB#"$ROOT"/}")"
[[ "$got" == "$want" ]] || die "package sha256 ${got} != SHA256SUMS ${want}"
got="$(sha256sum "$CHECK_HW" | awk '{print $1}')"
want="$(sum_line_hash "mainnet/check-hw/check-hw")"
[[ "$got" == "$want" ]] || die "check-hw sha256 ${got} != SHA256SUMS ${want}"

# Measurement is the 32 bytes at the SGX sigstruct offset. It must equal H.txt.
mrenclave_of() {
  python3 - "$1" <<'PY'
import binascii, sys
HDR = bytes.fromhex("06000000e10000000000010000000000")
d = open(sys.argv[1], "rb").read()
i = d.find(HDR)
if i < 0 or i + 992 > len(d):
    sys.exit(2)
print(binascii.hexlify(d[i + 960:i + 992]).decode())
PY
}

extract_so() {
  local dest="$1"
  dpkg-deb -x "$DEB" "$dest"
  [[ -s "$dest/usr/lib/librust_cosmwasm_enclave.signed.so" ]] || die "package has no signed enclave"
}

expect_h="$(tr -d '[:space:]' < "$ROOT/H.txt")"
[[ "$expect_h" =~ ^[0-9a-f]{64}$ ]] || die "H.txt is not 64 hex"
pkgdir="$(mktemp -d)"
trap 'rm -rf "$pkgdir"' EXIT
extract_so "$pkgdir"
pkg_h="$(mrenclave_of "$pkgdir/usr/lib/librust_cosmwasm_enclave.signed.so")"
[[ "$pkg_h" == "$expect_h" ]] || die "package measurement $pkg_h != H.txt $expect_h"
ENCLAVE_EXPECT_SHA256="$(sha256sum "$pkgdir/usr/lib/librust_cosmwasm_enclave.signed.so" | awk '{print $1}')"
MRENCLAVE_EXPECT="$expect_h"
HW_SO="$pkgdir/usr/lib/librust_cosmwasm_enclave.signed.so"

# upgrade-info.json is written when the node halts. One home is used.
# Two distinct files stops here, before the node is stopped.
read_upgrade_info() {
  local f real seen
  local -a files found
  if [[ -n "${SECRETD_HOME:-}" ]]; then
    files=("$SECRETD_HOME/data/upgrade-info.json")
  else
    files=(
      "$HOME/.secretd/data/upgrade-info.json"
      /home/ubuntu/.secretd/data/upgrade-info.json
      /root/.secretd/data/upgrade-info.json
      /opt/secret/.secretd/data/upgrade-info.json
    )
  fi
  seen=" "
  for f in "${files[@]}"; do
    [[ -s "$f" ]] || continue
    real="$(readlink -f -- "$f" 2>/dev/null || printf '%s' "$f")"
    case " $seen " in
      *" $real "*) continue ;;
    esac
    seen="$seen$real "
    found+=("$f")
  done
  if [[ "${#found[@]}" -gt 1 ]]; then
    echo "FATAL: upgrade-info.json is in more than one home. Set SECRETD_HOME to one:" >&2
    for f in "${found[@]}"; do
      printf '  %s\n' "${f%/data/upgrade-info.json}" >&2
    done
    return 2
  fi
  [[ "${#found[@]}" -eq 1 ]] || return 1
  python3 - "${found[0]}" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
plan = d.get("plan") or d
name = plan.get("name") or d.get("name") or ""
height = plan.get("height") or d.get("height") or ""
print(f"{name} {height}")
PY
}

# Plan name must be v1.27.2. The height in the file is what the handover stamps.
info="$(read_upgrade_info)" && rc=0 || rc=$?
if [[ "$rc" -eq 2 ]]; then
  exit 1
fi
[[ "$rc" -eq 0 ]] || die "node has no upgrade-info.json. Wait until it halts on the plan."
info_name="${info%% *}"
info_height="${info##* }"
[[ "$info_name" == "v1.27.2" ]] || die "upgrade-info name is $info_name (want v1.27.2)"
case "$info_height" in
  *[!0-9]*|"") die "upgrade-info height is not an integer ($info_height)" ;;
esac
[[ "$info_height" -gt 0 ]] || die "upgrade-info height must be positive"
PLAN_HEIGHT="$info_height"
EXTRA_HEIGHT="$info_height"

# check-hw 3 reads $HOME/.secretd, /root/.secretd, and /opt/secret/.secretd.
# It does not read SECRETD_HOME. A different height in one of those exits here,
# before the node is stopped. Unset SECRETD_HOME skips this.
if [[ -n "${SECRETD_HOME:-}" ]]; then
  hw_height="$(python3 - "$HOME" <<'PY'
import json, os, sys
home = sys.argv[1]
paths = []
if home:
    paths.append(os.path.join(home, ".secretd/data/upgrade-info.json"))
paths.extend([
    "/root/.secretd/data/upgrade-info.json",
    "/opt/secret/.secretd/data/upgrade-info.json",
])
seen = []
for path in paths:
    if path in seen:
        continue
    seen.append(path)
    try:
        data = json.load(open(path))
    except Exception:
        continue
    height = data.get("height")
    if isinstance(height, bool):
        continue
    if isinstance(height, int) and height > 0:
        print(height)
        raise SystemExit(0)
    if isinstance(height, str) and height.isdigit() and int(height) > 0:
        print(int(height))
        raise SystemExit(0)
PY
)" || die "could not read the upgrade-info files check-hw uses"
  if [[ -n "$hw_height" && "$hw_height" != "$PLAN_HEIGHT" ]]; then
    die "check-hw would use upgrade-info height $hw_height, not $PLAN_HEIGHT from SECRETD_HOME"
  fi
fi

# Unit that will be restarted. --bootstrap is genesis-only and must not be here.
if systemctl cat "$SERVICE" >/dev/null 2>&1; then
  if systemctl show -p ExecStart "$SERVICE" | grep -q -- '--bootstrap'; then
    die "$SERVICE ExecStart still has --bootstrap"
  fi
else
  die "systemd unit $SERVICE not found. Set SERVICE to this node's unit name."
fi

# The unit's SCRT_SGX_STORAGE is the directory the node reads. A different
# path here writes the handover where that node will not look. Unset in the
# unit means the binary default, which is the script default.
unit_store="$(systemctl show -p Environment --value "$SERVICE" | tr ' ' '\n' | sed -n 's/^SCRT_SGX_STORAGE=//p' | tail -1)"
if [[ -n "$unit_store" && "$unit_store" != "$SCRT_SGX_STORAGE" ]]; then
  die "systemd $SERVICE SCRT_SGX_STORAGE=$unit_store but this script uses $SCRT_SGX_STORAGE. Set SCRT_SGX_STORAGE to the unit path."
fi

# Signed handover file. This script does not download it.
[[ -s "$SCRT_SGX_STORAGE/migration_consensus.json" ]] \
  || die "missing $SCRT_SGX_STORAGE/migration_consensus.json"

# check-hw 3 reads halt_height. The value is the plan height from upgrade-info.json.
stamp_dest3() {
  mkdir -p "$SCRT_SGX_STORAGE"
  printf '%s\n' "$PLAN_HEIGHT" > "$SCRT_SGX_STORAGE/halt_height" \
    || die "could not write halt_height"
  export EXTRA_HEIGHT="$PLAN_HEIGHT"
  ok "halt_height=$PLAN_HEIGHT"
}

# check-hw loads ./check_hw_enclave.so from its working directory.
# Op 1 can fail the Intel quote and still pass if this run wrote migration_report_local.bin.
# Cleanup already removed any older report. A report that survives cleanup is fatal.
# Op 3 must stamp random_proof_hstar to the plan height.
run_check_hw() {
  local op="$1"
  [[ -x "$CHECK_HW" ]] || die "missing $CHECK_HW"
  local cwd sha mr hwabs out
  cwd="$(mktemp -d /tmp/checkhw-XXXXXX)"
  cp -fL "$HW_SO" "$cwd/check_hw_enclave.so"
  sha="$(sha256sum "$cwd/check_hw_enclave.so" | awk '{print $1}')"
  mr="$(mrenclave_of "$cwd/check_hw_enclave.so")"
  [[ "$sha" == "$ENCLAVE_EXPECT_SHA256" ]] || { rm -rf "$cwd"; die "enclave sha mismatch"; }
  [[ "$mr" == "$MRENCLAVE_EXPECT" ]] || { rm -rf "$cwd"; die "enclave measurement mismatch"; }
  hwabs="$(readlink -f "$CHECK_HW")"
  out="$(mktemp)"
  if ! ( cd "$cwd" && "$hwabs" --migrate_op "$op" >"$out" 2>&1 ); then
    cat "$out" >&2
    if [[ "$op" == "1" ]] && sudo test -s "$SCRT_SGX_STORAGE/migration_report_local.bin"; then
      ok "check-hw 1 wrote the local report"
      rm -rf "$cwd" "$out"
      return 0
    fi
    rm -rf "$cwd" "$out"
    die "check-hw --migrate_op $op failed"
  fi
  cat "$out"
  if [[ "$op" == "3" ]]; then
    if grep -q "stamped random_proof_hstar=${PLAN_HEIGHT}" "$out"; then
      :
    elif grep -q "already set" "$out" && grep -q "random_proof_hstar=${PLAN_HEIGHT}" "$out"; then
      ok "handover kept random_proof_hstar=${PLAN_HEIGHT}"
    else
      rm -rf "$cwd" "$out"
      die "handover did not stamp random_proof_hstar=${PLAN_HEIGHT}"
    fi
  fi
  rm -rf "$cwd" "$out"
  ok "check-hw --migrate_op $op"
}

echo "secret-4 v1.27.2 halt  install=$DO_INSTALL"
echo "  service=$SERVICE package=$DEB"
echo "  measurement=$expect_h"

# No --install: print the plan and exit. The node stays up.
if [[ "$DO_INSTALL" -ne 1 ]]; then
  ok "dry-run. At halt this stops $SERVICE, runs the handover, installs the package, then starts $SERVICE."
  exit 0
fi

# Install stops the node. Both flags are required. Handover runs on 1.26.0.
truthy_yes "${I_UNDERSTAND:-}" || die "set I_UNDERSTAND=yes for --install"
truthy_yes "${I_CONFIRM_PRECHECK:-}" || die "set I_CONFIRM_PRECHECK=yes for --install"
# A bad backup path stops here, before the node is stopped.
if [[ -n "${SERVICE_UNIT_FILE:-}" ]]; then
  [[ -s "$SERVICE_UNIT_FILE" ]] || die "SERVICE_UNIT_FILE is missing or empty: $SERVICE_UNIT_FILE"
fi
command -v secretd >/dev/null || die "secretd is not on PATH"
oldv="$(secretd version 2>/dev/null | head -1 || true)"
[[ "$oldv" == "$FROM_VER" ]] || die "installed secretd is ${oldv:-empty} (want $FROM_VER)"

# Copy the combined file before the stop. An unreadable file exits here.
json_bak="$(mktemp)"
cp -a "$SCRT_SGX_STORAGE/migration_consensus.json" "$json_bak"

# Stop, then handover on the 1.26.0 binary, then install the package.
# A failed stop exits before migrate_op. An already stopped unit still returns 0.
sudo systemctl stop "$SERVICE"

# Keep migration_consensus.json. Remove every other migration_* file.
# sudo: those files are often owned by the service user. A failed delete exits.
# An old migration_report_local.bin must not remain. check-hw 1 treats a
# nonempty report as success when the Intel quote fails.
sudo find "$SCRT_SGX_STORAGE" -maxdepth 1 -name 'migration_*' ! -name 'migration_consensus.json' -delete
if sudo test -e "$SCRT_SGX_STORAGE/migration_report_local.bin"; then
  die "migration_report_local.bin is still present after cleanup"
fi
cp -a "$json_bak" "$SCRT_SGX_STORAGE/migration_consensus.json"

# Handover on the installed binary, then import after the new package is installed.
export SCRT_SGX_STORAGE EXTRA_HEIGHT PLAN_HEIGHT
secretd migrate_op 5
run_check_hw 1
op2_out="$(mktemp)"
set +e
secretd migrate_op 2 >"$op2_out" 2>&1
op2_rc=$?
set -e
cat "$op2_out"
[[ "$op2_rc" -eq 0 ]] || die "migrate_op 2 failed"
if grep -F -q "Migration is authorized by on-chain consensus" "$op2_out"; then
  die "migrate_op 2 authorized by on-chain consensus. This halt uses the emergency file."
fi
grep -F -q "Migration is authorized by off-chain (emergency) consensus" "$op2_out" \
  || die "migrate_op 2 did not print off-chain (emergency) consensus"
grep -F -q "Emergency threshold reached: true" "$op2_out" \
  || die "migrate_op 2 emergency threshold was not reached"
stamp_dest3
run_check_hw 3

# Do not install the package unless the new sealed file exists.
[[ -s "$SCRT_SGX_STORAGE/data-${MRENCLAVE_EXPECT}.bin" ]] \
  || die "missing sealed data-${MRENCLAVE_EXPECT}.bin after the handover"

# Install 1.27.2. secretd must report 1.27.2 and the installed enclave must match H.txt.
sudo dpkg -i "$DEB"
newv="$(secretd version 2>/dev/null | head -1 || true)"
[[ "$newv" == "1.27.2" ]] || die "package installed but secretd version is ${newv:-empty}"
newmr="$(mrenclave_of "$ENCLAVE")"
[[ "$newmr" == "$MRENCLAVE_EXPECT" ]] || die "installed enclave measurement $newmr != $MRENCLAVE_EXPECT"

# postinst rewrites the unit. Put the operator backup back before start.
if [[ -n "${SERVICE_UNIT_FILE:-}" ]]; then
  sudo cp -a "$SERVICE_UNIT_FILE" "/etc/systemd/system/${SERVICE}.service"
  sudo systemctl daemon-reload
  if systemctl show -p ExecStart "$SERVICE" | grep -q -- '--bootstrap'; then
    die "$SERVICE ExecStart still has --bootstrap"
  fi
  if systemctl show -p ExecStart "$SERVICE" | grep -q -- 'unsafe-skip-upgrades'; then
    die "$SERVICE ExecStart has --unsafe-skip-upgrades"
  fi
fi

# Start the node on 1.27.2.
sudo systemctl enable "$SERVICE"
sudo systemctl start "$SERVICE"
ok "started $SERVICE on 1.27.2"
