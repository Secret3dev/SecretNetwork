#!/usr/bin/env bash
# Kept so older curl URLs do not 404.
# This is not a second installer. It runs scripts/install-continuance.sh,
# which selects TESTNET vs MAINNET from this machine's chain-id.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$HERE/install-continuance.sh" ]; then
  exec "$HERE/install-continuance.sh" "$@"
fi
TAG="${TAG:-v1.26.0-community-continuance}"
REPO="${REPO:-Secret3dev/SecretNetwork}"
URL="https://raw.githubusercontent.com/${REPO}/${TAG}/scripts/install-continuance.sh"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
curl -fsSL "$URL" -o "$tmp" \
  || { echo "FATAL: could not fetch $URL" >&2; exit 1; }
chmod +x "$tmp"
exec "$tmp" "$@"
