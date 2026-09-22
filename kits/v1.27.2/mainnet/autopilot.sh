#!/bin/bash
# Operator script for the secret-4 v1.27.2 packages in this directory.
# Picks the Ubuntu 22.04 or 24.04 package. Does not install it until halt.
#
#   ./autopilot.sh                  # show the package and measurement
#   ./autopilot.sh sign             # sign the measurement and send it
#   I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./autopilot.sh install
#
# Signatures go to https://upgrade.secret3.dev upgrade id secret-4-v1.27.2.
#
# install waits until the collector is serving the combined file, writes it, then
# execs halt.sh. SECRETD_HOME and SERVICE_UNIT_FILE are read there.
# Leave them unset and halt.sh scans the default homes and does not restore a unit.
# sign reads $HOME/.secretd. The binary ignores --home. SECRETD_HOME does not change sign.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
API="${COLLECTOR_API:-https://upgrade.secret3.dev}"
UPGRADE_ID="${UPGRADE_ID:-secret-4-v1.27.2}"
CMD="${1:-print}"

die() { echo "FATAL: $*" >&2; exit 1; }
ok() { echo "OK  $*"; }

# Refuse unless this upgrade is the one current secret-4 row and H matches the package.
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
  # The current list for secret-4 must be this id and nothing else.
  ids="$(curl -fsS --max-time 20 "$API/v1/upgrades?chain_id=secret-4&state=current" \
    | python3 -c 'import json,sys; rows=json.load(sys.stdin).get("upgrades") or []; print(" ".join(r.get("id") or "" for r in rows))')"
  [[ "$ids" == "$UPGRADE_ID" ]] || die "secret-4 current upgrade is [$ids], not $UPGRADE_ID"
  local api_h
  api_h="$(python3 -c 'import json,sys; print((json.loads(sys.argv[1]).get("H") or "").lower())' "$rec")"
  [[ "$api_h" == "$H" ]] || die "collector measurement $api_h does not match this package"
}

# Package for this Ubuntu release. H.txt must be 64 hex. Then require_current.
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
    # Show the package and measurement. Does not stop the node.
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
    # Sign H with this node's validator key and send that signature to the collector.
    # emergency_approve_upgrade reads $HOME/.secretd. The 1.26 binary ignores --home.
    if [[ -n "${SECRETD_HOME:-}" ]]; then
      sign_home="$(readlink -f "${HOME}/.secretd")"
      intent_home="$(readlink -f "$SECRETD_HOME")"
      [[ "$sign_home" == "$intent_home" ]] || die "emergency_approve_upgrade reads ${HOME}/.secretd and ignores --home. SECRETD_HOME is $SECRETD_HOME."
    fi
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
      200) ok "signature accepted" ;;
      409)
        if grep -q "frozen" /tmp/upgrade-sig.out; then
          ok "combined file is already frozen"
        else
          die "collector returned HTTP 409 $(cat /tmp/upgrade-sig.out)"
        fi
        ;;
      *) die "collector returned HTTP $code" ;;
    esac
    ;;
  install)
    # The collector serves the combined file once 7 signatures are in.
    # Write that file before halt.sh. The node stays up while this waits.
    dest="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}/migration_consensus.json"
    url="$API/v1/upgrades/$UPGRADE_ID/consensus"
    while true; do
      code="$(curl -sS -o /tmp/migration_consensus.json -w '%{http_code}' --max-time 20 "$url" || true)"
      if [[ "$code" == "200" ]]; then
        python3 -c 'import json,re,sys; d=json.load(open(sys.argv[1])); assert isinstance(d, dict) and d
for k,v in d.items():
    assert re.fullmatch(r"[0-9A-F]{40}", k)
    assert isinstance(v, list) and len(v)==2 and all(isinstance(x, str) and x for x in v)' /tmp/migration_consensus.json \
          || die "collector returned a body that is not the combined file"
        break
      fi
      if [[ "$code" == "410" ]]; then
        die "upgrade $UPGRADE_ID is done. Combined file is no longer served."
      fi
      # 409 body is have/need. Do not copy it. The node stays up.
      counts=""
      if [[ "$code" == "409" ]]; then
        counts="$(python3 -c 'import json; d=json.load(open("/tmp/migration_consensus.json")); print("have %s, need %s" % (d.get("have"), d.get("need")))' 2>/dev/null || true)"
      fi
      if [[ -n "$counts" ]]; then
        echo "combined file is not ready ($counts). node is still up."
      else
        echo "waiting for combined file (HTTP ${code:-none}). node is still up."
      fi
      sleep 15
    done
    sudo mkdir -p "$(dirname "$dest")"
    if [[ -w "$(dirname "$dest")" ]]; then
      cp /tmp/migration_consensus.json "$dest"
    else
      sudo cp /tmp/migration_consensus.json "$dest"
      sudo chown "$(id -u):$(id -g)" "$dest"
    fi
    [[ -s "$dest" ]] || die "could not write $dest"
    ok "wrote $dest"
    # halt.sh stops the node, runs the handover on 1.26.0, then installs the package.
    exec "$ROOT/halt.sh" --install
    ;;
  *)
    die "usage: $0 [print|sign|install]"
    ;;
esac
