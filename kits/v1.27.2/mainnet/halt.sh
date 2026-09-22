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
# data/upgrade-info.json. Unset, the four homes below are scanned in order.
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
    -h|--help) sed -n '2,17p' "$0" | sed 's/^# \?//'; exit 0 ;;
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

# upgrade-info.json is written when the node halts. SECRETD_HOME replaces the scan.
read_upgrade_info() {
  local f
  local -a files
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
  for f in "${files[@]}"; do
    [[ -s "$f" ]] || continue
    python3 - "$f" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
plan = d.get("plan") or d
name = plan.get("name") or d.get("name") or ""
height = plan.get("height") or d.get("height") or ""
print(f"{name} {height}")
PY
    return 0
  done
  return 1
}

# Plan name must be v1.27.2. The height in the file is what the handover stamps.
info="$(read_upgrade_info)" || die "node has no upgrade-info.json. Wait until it halts on the plan."
info_name="${info%% *}"
info_height="${info##* }"
[[ "$info_name" == "v1.27.2" ]] || die "upgrade-info name is $info_name (want v1.27.2)"
case "$info_height" in
  *[!0-9]*|"") die "upgrade-info height is not an integer ($info_height)" ;;
esac
[[ "$info_height" -gt 0 ]] || die "upgrade-info height must be positive"
PLAN_HEIGHT="$info_height"
EXTRA_HEIGHT="$info_height"

# Unit that will be restarted. --bootstrap is genesis-only and must not be here.
if systemctl cat "$SERVICE" >/dev/null 2>&1; then
  if systemctl show -p ExecStart "$SERVICE" | grep -q -- '--bootstrap'; then
    die "$SERVICE ExecStart still has --bootstrap"
  fi
else
  die "systemd unit $SERVICE not found. Set SERVICE to this node's unit name."
fi

# Signed handover file. This script does not download it.
[[ -s "$SCRT_SGX_STORAGE/migration_consensus.json" ]] \
  || die "missing $SCRT_SGX_STORAGE/migration_consensus.json"

stamp_dest3() {
  mkdir -p "$SCRT_SGX_STORAGE"
  printf '%s\n' "$PLAN_HEIGHT" > "$SCRT_SGX_STORAGE/halt_height" \
    || die "could not write halt_height"
  export EXTRA_HEIGHT="$PLAN_HEIGHT"
  ok "halt_height=$PLAN_HEIGHT"
}

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
    if [[ "$op" == "1" && -s "$SCRT_SGX_STORAGE/migration_report_local.bin" ]]; then
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

# Stop, then handover on the 1.26.0 binary, then install the package.
sudo systemctl stop "$SERVICE" || true

# Keep migration_consensus.json. Remove every other migration_* file.
json_bak="$(mktemp)"
cp -a "$SCRT_SGX_STORAGE/migration_consensus.json" "$json_bak"
find "$SCRT_SGX_STORAGE" -maxdepth 1 -name 'migration_*' ! -name 'migration_consensus.json' -delete || true
cp -a "$json_bak" "$SCRT_SGX_STORAGE/migration_consensus.json"

# 5 self target info, check-hw 1 report, 2 export, stamp halt height, check-hw 3 import.
export SCRT_SGX_STORAGE EXTRA_HEIGHT PLAN_HEIGHT
secretd migrate_op 5
run_check_hw 1
secretd migrate_op 2
stamp_dest3
run_check_hw 3

# Do not install the package unless the new sealed file exists.
[[ -s "$SCRT_SGX_STORAGE/data-${MRENCLAVE_EXPECT}.bin" ]] \
  || die "missing sealed data-${MRENCLAVE_EXPECT}.bin after the handover"

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

sudo systemctl enable "$SERVICE"
sudo systemctl start "$SERVICE"
ok "started $SERVICE on 1.27.2"
