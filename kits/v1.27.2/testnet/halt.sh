#!/bin/bash
# trinity-b host upgrade. Same enclave measurement. Plan v1.27.2.
# The running binary must be 1.27.0. Installs the package. No handover.
#
#   I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./halt.sh --install
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
DO_INSTALL=0
for arg in "$@"; do
  case "$arg" in
    --install) DO_INSTALL=1 ;;
    -h|--help) sed -n '2,8p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; exit 1 ;;
  esac
done

die() { echo "FATAL: $*" >&2; exit 1; }
ok() { echo "OK  $*"; }

API="${COLLECTOR_API:-https://upgrade.secret3.dev}"
UPGRADE_ID="${UPGRADE_ID:-trinity-b-v1.27.0-r10}"
rec="$(curl -fsS --max-time 20 "$API/v1/upgrades/$UPGRADE_ID")" \
  || die "cannot read $API/v1/upgrades/$UPGRADE_ID"
state="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("state") or "")' "$rec")"
if [[ "$state" == "done" ]]; then
  die "upgrade $UPGRADE_ID is done. This script will not run."
fi
truthy_yes() {
  case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
    yes|true|1|y|on) return 0 ;;
    *) return 1 ;;
  esac
}

[[ "${CHAIN_ID:-trinity-b}" == "trinity-b" ]] || die "CHAIN_ID must be trinity-b"
os_id="$(. /etc/os-release; printf '%s' "${VERSION_ID:-}")"
case "$os_id" in
  22.04|24.04) ;;
  *) die "need Ubuntu 22.04 or 24.04" ;;
esac
DEB="$ROOT/ubuntu-${os_id}/secretnetwork_1.27.2_TRINITY_goleveldb_amd64_ubuntu-${os_id}.deb"
SERVICE="${SERVICE:-trinity-node}"
[[ -s "$DEB" ]] || die "missing $DEB"

if [[ "$DO_INSTALL" -ne 1 ]]; then
  ok "dry-run. Stops $SERVICE, installs the 1.27.2 package, starts $SERVICE. No handover."
  exit 0
fi
truthy_yes "${I_UNDERSTAND:-}" || die "set I_UNDERSTAND=yes"
truthy_yes "${I_CONFIRM_PRECHECK:-}" || die "set I_CONFIRM_PRECHECK=yes"
oldv="$(secretd version 2>/dev/null | head -1 || true)"
[[ "$oldv" == "1.27.0" ]] || die "secretd is ${oldv:-empty} (want 1.27.0)"
sudo systemctl stop "$SERVICE" || true
sudo dpkg -i "$DEB"
newv="$(secretd version 2>/dev/null | head -1 || true)"
[[ "$newv" == "1.27.2" ]] || die "secretd is ${newv:-empty} after install"
sudo systemctl enable "$SERVICE"
sudo systemctl start "$SERVICE"
ok "started $SERVICE on 1.27.2"
