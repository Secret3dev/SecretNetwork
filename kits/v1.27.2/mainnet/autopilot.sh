#!/bin/bash
# Operator script for the secret-4 v1.27.2 packages in this directory.
# Picks the Ubuntu 22.04 or 24.04 package. Does not install it until halt.
#
#   ./autopilot.sh                  # show the package and measurement
#   ./autopilot.sh sign             # sign the measurement and send it
#   I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./autopilot.sh install
#
# Signatures go to https://upgrade.secret3.dev upgrade id secret-4-v1.27.2.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
API="${COLLECTOR_API:-https://upgrade.secret3.dev}"
UPGRADE_ID="${UPGRADE_ID:-secret-4-v1.27.2}"
CMD="${1:-print}"

die() { echo "FATAL: $*" >&2; exit 1; }
ok() { echo "OK  $*"; }

require_current() {
  local rec state ids
  rec="$(curl -fsS --max-time 20 "$API/v1/upgrades/$UPGRADE_ID")" \
    || die "cannot read $API/v1/upgrades/$UPGRADE_ID"
  state="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("state") or "")' "$rec")"
  if [[ "$state" == "done" ]]; then
    die "upgrade $UPGRADE_ID is done. This script will not run."
  fi
  if [[ "$state" != "current" ]]; then
    die "upgrade $UPGRADE_ID is not the current secret-4 upgrade (state=${state:-unset})."
  fi
  ids="$(curl -fsS --max-time 20 "$API/v1/upgrades?chain_id=secret-4&state=current" \
    | python3 -c 'import json,sys; rows=json.load(sys.stdin).get("upgrades") or []; print(" ".join(r.get("id") or "" for r in rows))')"
  [[ "$ids" == "$UPGRADE_ID" ]] || die "secret-4 current upgrade is [$ids], not $UPGRADE_ID"
  local api_h
  api_h="$(python3 -c 'import json,sys; print((json.loads(sys.argv[1]).get("H") or "").lower())' "$rec")"
  [[ "$api_h" == "$H" ]] || die "collector measurement $api_h does not match this package"
}

os_id="$(. /etc/os-release; printf '%s' "${VERSION_ID:-}")"
case "$os_id" in
  22.04|24.04) ;;
  *) die "need Ubuntu 22.04 or 24.04 (got ${os_id:-unknown})" ;;
esac
DEB="$ROOT/ubuntu-${os_id}/secretnetwork_1.27.2_MAINNET_goleveldb_amd64_ubuntu-${os_id}.deb"
H="$(tr -d '[:space:]' < "$ROOT/H.txt")"
[[ -s "$DEB" ]] || die "missing $DEB"
[[ "$H" =~ ^[0-9a-f]{64}$ ]] || die "H.txt is not 64 hex"
require_current

case "$CMD" in
  print|"")
    echo "chain        secret-4"
    echo "plan         v1.27.2"
    echo "from         1.26.0"
    echo "package      $DEB"
    echo "measurement  $H"
    echo "collector    $API/v1/upgrades/$UPGRADE_ID"
    echo
    echo "Do not install the package before the node halts."
    echo "At the halt:  I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes $0 install"
    ;;
  sign)
    command -v secretd >/dev/null || die "secretd is not on PATH"
    out="$(secretd emergency_approve_upgrade "$H" 2>&1)" || {
      printf '%s\n' "$out" >&2
      die "emergency_approve_upgrade failed"
    }
    printf '%s\n' "$out"
    line="$(printf '%s\n' "$out" | grep -F 'Signature:' | tail -1)"
    [[ -n "$line" ]] || die "no Signature line"
    json="${line#*Signature:}"
    json="${json#"${json%%[![:space:]]*}"}"
    code="$(curl -sS -o /tmp/upgrade-sig.out -w '%{http_code}' \
      -H 'Content-Type: application/json' \
      -d "$json" "$API/v1/upgrades/$UPGRADE_ID/sigs")"
    echo "HTTP $code"
    cat /tmp/upgrade-sig.out
    echo
    case "$code" in
      200|409) ok "signature accepted or the list is already frozen" ;;
      *) die "collector returned HTTP $code" ;;
    esac
    ;;
  install)
    exec "$ROOT/halt.sh" --install
    ;;
  *)
    die "usage: $0 [print|sign|install]"
    ;;
esac
