#!/usr/bin/env bash
# Install Continuance host upgrade from the GitHub Release.
#
# Fail-closed: this machine's chain-id picks TESTNET vs MAINNET.
# pulsar-3 → TESTNET package. secret-4 → MAINNET package.
# Anything else, or a disagreement between sources, is FATAL.
# Ubuntu 22.04 / 24.04 only. Never uses the shell variable VERSION
# after sourcing /etc/os-release.
#
# Usage (at upgrade halt):
#   curl -fsSL -o install.sh \
#     https://raw.githubusercontent.com/Secret3dev/SecretNetwork/v1.26.0-community-continuance/scripts/install-continuance.sh
#   chmod +x install.sh
#   sudo ./install.sh
#
# Detect only (does not stop the node or install):
#   DRY_RUN=1 ./scripts/install-continuance.sh
#
# Optional env:
#   CHAIN_ID   force chain-id (still refused if the node disagrees)
#   SERVICE    systemd unit (default: secret-node)
#   TAG        release tag (default: v1.26.0-community-continuance)
#   REPO       owner/name (default: Secret3dev/SecretNetwork)
#   SECRETD_HOME
#   NODE       RPC for `secretd status` (default: tcp://127.0.0.1:26657)
#   DRY_RUN=1  print the package that would be installed and exit
set -euo pipefail

REPO="${REPO:-Secret3dev/SecretNetwork}"
TAG="${TAG:-v1.26.0-community-continuance}"
SERVICE="${SERVICE:-secret-node}"
WORKDIR="${WORKDIR:-/tmp/continuance-install}"
NODE="${NODE:-tcp://127.0.0.1:26657}"
# Do NOT name this VERSION — /etc/os-release overwrites VERSION.
PKG_VER="1.26.0"

# deb sha256 — must match the Release assets for this tag.
declare -A DEB_SHA=(
  [MAINNET|ubuntu-22.04]=bb9baf0ee8d32b7800845771de1224edb42876245f1ae730323481353600a6f6
  [MAINNET|ubuntu-24.04]=b38ebe3dfc54e236519751fe23962edd25eb2d3ba9bfbd6eab86ad3315534a8b
  [TESTNET|ubuntu-22.04]=672dd49b5b95587559e998f89e260142a4bcdb958a5f3009ece50e8ff4ce06d4
  [TESTNET|ubuntu-24.04]=d86def55a3c38c7de3dd91583bc66c29e05962e039e0b2ae47939b5f04faf925
)

# secretd inside each .deb — checked after install.
declare -A SECRETD_SHA=(
  [MAINNET|ubuntu-22.04]=f6d6cb40230ec96d9a1cd6b5de65d217a679ba2db034696f5d513e6feef17a6c
  [MAINNET|ubuntu-24.04]=0cb3866f2f4f2ab3e8cf0f5d2c0766054694b712149514111c06e5dd7ab0358d
  [TESTNET|ubuntu-22.04]=24d5ff98aaf16bf467b800060cb091a19e8a6b77255b397b8796e172d32c92af
  [TESTNET|ubuntu-24.04]=8558b701c099f7e8f2e03d7525e1a36cfb58fd22523f37d2b12d729c74f4a42f
)

die() { echo "FATAL: $*" >&2; exit 1; }
info() { echo "    ..  $*"; }
ok() { echo "    OK  $*"; }

# ---------------------------------------------------------------------------
# Chain-id: collect every source we can see. They must all agree.
# ---------------------------------------------------------------------------

FOUND_SRC=()
FOUND_ID=()

add_id() {
  local src="$1" id="${2:-}"
  id="$(printf '%s' "$id" | tr -d '[:space:]')"
  [ -n "$id" ] || return 0
  FOUND_SRC+=("$src")
  FOUND_ID+=("$id")
}

chain_id_from_genesis_file() {
  local f="$1"
  [ -f "$f" ] || return 0
  # genesis.json can be >1GB. chain_id is a top-level field; on secret-4 it sits
  # AFTER app_state, so read the tail (and a small header) — never the whole file.
  python3 - "$f" <<'PY' 2>/dev/null || true
import re, sys
path = sys.argv[1]
f = open(path, "rb")
head = f.read(65536)
f.seek(0, 2)
n = f.tell()
f.seek(max(0, n - 262144))
tail = f.read()
blob = (head + b"\n" + tail).decode("utf-8", "replace")
ids = re.findall(r'"chain_id"\s*:\s*"([^"]+)"', blob)
# Prefer a top-level-looking value: secret-4 / pulsar-3 / secretdev-1
prefer = [x for x in ids if x in ("secret-4", "pulsar-3", "secretdev-1")]
print((prefer[-1] if prefer else (ids[-1] if ids else "")))
PY
}

chain_id_from_status_json() {
  local raw="${1:-}"
  [ -n "$raw" ] || return 0
  printf '%s' "$raw" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
info = d.get("node_info") or d.get("NodeInfo") or {}
print(info.get("network") or "")
' 2>/dev/null || true
}

collect_ids() {
  if [ -n "${CHAIN_ID:-}" ]; then
    add_id "CHAIN_ID env" "$CHAIN_ID"
  fi

  local st raw
  raw="$(timeout 5 secretd status --node "$NODE" --output json 2>/dev/null || true)"
  st="$(chain_id_from_status_json "$raw")"
  add_id "secretd status ${NODE}" "$st"

  local homes=()
  [ -n "${SECRETD_HOME:-}" ] && homes+=("$SECRETD_HOME")
  if [ -n "${SUDO_USER:-}" ] && [ "${SUDO_USER}" != "root" ]; then
    homes+=("$(getent passwd "$SUDO_USER" | cut -d: -f6)/.secretd")
  fi
  homes+=("${HOME}/.secretd")

  local unit_user unit_home
  unit_user="$(systemctl show "$SERVICE" -p User --value 2>/dev/null || true)"
  if [ -n "$unit_user" ] && [ "$unit_user" != "root" ]; then
    homes+=("$(getent passwd "$unit_user" | cut -d: -f6)/.secretd")
  fi
  unit_home="$(systemctl show "$SERVICE" -p ExecStart --value 2>/dev/null | tr ' ' '\n' | awk '/--home/{getline; print}' || true)"
  [ -n "$unit_home" ] && homes+=("$unit_home")

  local h g id seen=""
  for h in "${homes[@]}"; do
    [ -n "$h" ] || continue
    g="${h}/config/genesis.json"
    case " $seen " in *" $g "*) continue ;; esac
    seen="$seen $g"
    id="$(chain_id_from_genesis_file "$g")"
    add_id "genesis $g" "$id"
  done
}

map_net() {
  case "$1" in
    secret-4) echo MAINNET ;;
    pulsar-3) echo TESTNET ;;
    secretdev-1) die "chain-id secretdev-1 is local rehearsal — refusing to install a Release package" ;;
    "") die "empty chain-id" ;;
    *) die "unknown chain-id '$1' (need secret-4 or pulsar-3). Set CHAIN_ID= if this node has no genesis." ;;
  esac
}

# ---------------------------------------------------------------------------
# OS
# ---------------------------------------------------------------------------

[ -r /etc/os-release ] || die "no /etc/os-release"
# shellcheck source=/dev/null
. /etc/os-release
PKG_VER="1.26.0"
[ "${ID:-}" = "ubuntu" ] || die "only Ubuntu is supported (got ID=${ID:-unknown})"
case "${VERSION_ID:-}" in
  22.04) OS_TAG=ubuntu-22.04 ;;
  24.04) OS_TAG=ubuntu-24.04 ;;
  *) die "unsupported Ubuntu ${VERSION_ID:-unknown} (need 22.04 or 24.04)" ;;
esac

# ---------------------------------------------------------------------------
# Resolve network from this machine
# ---------------------------------------------------------------------------

collect_ids

if [ "${#FOUND_ID[@]}" -eq 0 ]; then
  die "could not determine chain-id. Set CHAIN_ID=secret-4 or CHAIN_ID=pulsar-3 (must match this machine)."
fi

CHAIN_RESOLVED="${FOUND_ID[0]}"
i=0
while [ "$i" -lt "${#FOUND_ID[@]}" ]; do
  if [ "${FOUND_ID[$i]}" != "$CHAIN_RESOLVED" ]; then
    die "chain-id disagreement: ${FOUND_SRC[0]}=$CHAIN_RESOLVED vs ${FOUND_SRC[$i]}=${FOUND_ID[$i]} — refusing to guess"
  fi
  i=$((i + 1))
done

NET_TAG="$(map_net "$CHAIN_RESOLVED")"

# Cross-install is unrepresentable: the package name is derived from NET_TAG.
DEB_NAME="secretnetwork_${PKG_VER}_${NET_TAG}_goleveldb_amd64_${OS_TAG}.deb"
KEY="${NET_TAG}|${OS_TAG}"
WANT_SHA="${DEB_SHA[$KEY]:-}"
WANT_BIN="${SECRETD_SHA[$KEY]:-}"
[ -n "$WANT_SHA" ] || die "no pinned sha256 for $KEY"
[ -n "$WANT_BIN" ] || die "no pinned secretd sha256 for $KEY"

case "$DEB_NAME" in
  *MAINNET*) [ "$NET_TAG" = MAINNET ] || die "internal: MAINNET in name but NET_TAG=$NET_TAG" ;;
  *TESTNET*) [ "$NET_TAG" = TESTNET ] || die "internal: TESTNET in name but NET_TAG=$NET_TAG" ;;
  *) die "internal: package name has no network tag: $DEB_NAME" ;;
esac
case "$DEB_NAME" in
  *MAINNET*TESTNET*|*TESTNET*MAINNET*) die "internal: package name has both network tags" ;;
esac
if [ "$CHAIN_RESOLVED" = "secret-4" ] && [ "$NET_TAG" != "MAINNET" ]; then
  die "secret-4 must map to MAINNET (got $NET_TAG)"
fi
if [ "$CHAIN_RESOLVED" = "pulsar-3" ] && [ "$NET_TAG" != "TESTNET" ]; then
  die "pulsar-3 must map to TESTNET (got $NET_TAG)"
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${DEB_NAME}"

echo "============================================================"
echo " Continuance install"
echo "============================================================"
echo "    chain-id  ${CHAIN_RESOLVED}"
echo "    network   ${NET_TAG}"
echo "    os        Ubuntu ${VERSION_ID} -> ${OS_TAG}"
echo "    package   ${DEB_NAME}"
echo "    release   ${REPO} @ ${TAG}"
echo "    service   ${SERVICE}"
i=0
while [ "$i" -lt "${#FOUND_ID[@]}" ]; do
  echo "    source    ${FOUND_SRC[$i]} = ${FOUND_ID[$i]}"
  i=$((i + 1))
done
echo

if [ "${DRY_RUN:-}" = "1" ]; then
  ok "DRY_RUN=1 — would install ${DEB_NAME} (sha256 ${WANT_SHA})"
  echo "    not stopping ${SERVICE}, not downloading, not installing"
  exit 0
fi

[ "$(id -u)" -eq 0 ] || die "run as root (sudo $0) — or DRY_RUN=1 to detect only"

command -v curl >/dev/null || die "need curl"
command -v sha256sum >/dev/null || die "need sha256sum"
command -v systemctl >/dev/null || die "need systemctl"

mkdir -p "$WORKDIR"
cd "$WORKDIR"
info "downloading ${URL}"
curl -fL --retry 3 --retry-delay 2 -o "$DEB_NAME" "$URL" \
  || die "download failed — is the ${NET_TAG} asset on Release ${TAG}?"

[ "$(basename "$DEB_NAME")" = "$DEB_NAME" ] || die "unexpected downloaded name"
case "$DEB_NAME" in
  *MAINNET*) [ "$NET_TAG" = MAINNET ] || die "downloaded MAINNET package on a $NET_TAG machine" ;;
  *TESTNET*) [ "$NET_TAG" = TESTNET ] || die "downloaded TESTNET package on a $NET_TAG machine" ;;
esac

GOT_SHA="$(sha256sum "$DEB_NAME" | awk '{print $1}')"
info "sha256  $GOT_SHA"
[ "$GOT_SHA" = "$WANT_SHA" ] || die "sha256 MISMATCH (want $WANT_SHA)"
ok "package checksum matches ${NET_TAG} ${OS_TAG}"

info "stopping ${SERVICE}"
systemctl stop "$SERVICE" || die "failed to stop ${SERVICE}"
ok "stopped"

info "installing ${DEB_NAME}"
apt-get install -y "./${DEB_NAME}"
ok "installed"

GOT_BIN="$(sha256sum /usr/local/bin/secretd | awk '{print $1}')"
[ "$GOT_BIN" = "$WANT_BIN" ] || die "installed /usr/local/bin/secretd sha256 $GOT_BIN != $WANT_BIN (${NET_TAG} ${OS_TAG})"
ok "secretd checksum matches ${NET_TAG} ${OS_TAG}"

info "starting ${SERVICE}"
systemctl start "$SERVICE" || die "failed to start ${SERVICE}"
systemctl is-active --quiet "$SERVICE" && ok "service active" \
  || die "service not active — check journalctl -u ${SERVICE}"

echo
echo "Done. ${CHAIN_RESOLVED} -> ${NET_TAG} ${OS_TAG}"
echo "  secretd version"
echo "  Never use --unsafe-skip-upgrades."
echo "============================================================"
