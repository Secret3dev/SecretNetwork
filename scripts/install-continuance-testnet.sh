#!/usr/bin/env bash
# Install Continuance TESTNET host upgrade from the GitHub Release.
#
# Detects Ubuntu 22.04 vs 24.04, downloads the matching .deb, verifies sha256,
# stops the node service, installs the package, starts the service.
#
# Usage (at upgrade halt):
#   curl -fsSL -o install.sh \
#     https://raw.githubusercontent.com/Secret3dev/SecretNetwork/v1.26.0-community-continuance/scripts/install-continuance-testnet.sh
#   chmod +x install.sh
#   sudo ./install.sh
#
# Or after cloning this repo:
#   sudo ./scripts/install-continuance-testnet.sh
#
# Env (optional):
#   SERVICE   systemd unit (default: secret-node)
#   TAG       release tag (default: v1.26.0-community-continuance)
#   REPO      owner/name  (default: Secret3dev/SecretNetwork)
#   WORKDIR   download dir (default: /tmp/continuance-install)
set -euo pipefail

REPO="${REPO:-Secret3dev/SecretNetwork}"
TAG="${TAG:-v1.26.0-community-continuance}"
SERVICE="${SERVICE:-secret-node}"
WORKDIR="${WORKDIR:-/tmp/continuance-install}"
VERSION="1.26.0"

# Package digests (TESTNET Continuance cut). Must match the Release assets.
declare -A EXPECT_SHA=(
  [ubuntu-22.04]=672dd49b5b95587559e998f89e260142a4bcdb958a5f3009ece50e8ff4ce06d4
  [ubuntu-24.04]=d86def55a3c38c7de3dd91583bc66c29e05962e039e0b2ae47939b5f04faf925
)

die() { echo "FATAL: $*" >&2; exit 1; }
info() { echo "    ..  $*"; }
ok() { echo "    OK  $*"; }

[ "$(id -u)" -eq 0 ] || die "run as root (sudo $0)"

# --- detect OS ---
[ -r /etc/os-release ] || die "no /etc/os-release"
# shellcheck source=/dev/null
. /etc/os-release
[ "${ID:-}" = "ubuntu" ] || die "only Ubuntu is supported (got ID=${ID:-unknown})"

case "${VERSION_ID:-}" in
  22.04) OS_TAG=ubuntu-22.04 ;;
  24.04) OS_TAG=ubuntu-24.04 ;;
  *) die "unsupported Ubuntu ${VERSION_ID:-unknown} (need 22.04 or 24.04)" ;;
esac

DEB_NAME="secretnetwork_${VERSION}_TESTNET_goleveldb_amd64_${OS_TAG}.deb"
WANT_SHA="${EXPECT_SHA[$OS_TAG]}"
URL="https://github.com/${REPO}/releases/download/${TAG}/${DEB_NAME}"

echo "============================================================"
echo " Continuance TESTNET install"
echo "============================================================"
echo "    os       Ubuntu ${VERSION_ID} -> ${OS_TAG}"
echo "    package  ${DEB_NAME}"
echo "    release  ${REPO} @ ${TAG}"
echo "    service  ${SERVICE}"
echo

command -v curl >/dev/null || die "need curl"
command -v sha256sum >/dev/null || die "need sha256sum"
command -v systemctl >/dev/null || die "need systemctl"

mkdir -p "$WORKDIR"
cd "$WORKDIR"
info "downloading ${URL}"
curl -fL --retry 3 --retry-delay 2 -o "$DEB_NAME" "$URL" \
  || die "download failed — is the Release published with this asset?"

GOT_SHA=$(sha256sum "$DEB_NAME" | awk '{print $1}')
info "sha256  $GOT_SHA"
[ "$GOT_SHA" = "$WANT_SHA" ] || die "sha256 MISMATCH (want $WANT_SHA)"
ok "checksum matches"

info "stopping ${SERVICE}"
systemctl stop "$SERVICE" || die "failed to stop ${SERVICE}"
ok "stopped"

info "installing package"
apt-get install -y "./${DEB_NAME}"
ok "installed"

info "starting ${SERVICE}"
systemctl start "$SERVICE" || die "failed to start ${SERVICE}"
systemctl is-active --quiet "$SERVICE" && ok "service active" || die "service not active — check journalctl -u ${SERVICE}"

echo
echo "Done. Expected: secretd 1.26.0, upgrade name v1.26.0-community-continuance"
echo "  secretd version"
echo "  journalctl -u ${SERVICE} -n 50 --no-pager | grep -i upgrade"
echo
echo "Never use --unsafe-skip-upgrades."
echo "============================================================"
