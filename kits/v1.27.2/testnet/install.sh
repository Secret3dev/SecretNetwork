#!/bin/bash
# Install the trinity-b 1.27.2 package for this machine's Ubuntu version.
# The chain already applied plan v1.27.2. This only installs the package.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
die() { echo "FATAL: $*" >&2; exit 1; }

os_id="$(. /etc/os-release; printf '%s' "${VERSION_ID:-}")"
case "$os_id" in
  22.04|24.04) ;;
  *) die "need Ubuntu 22.04 or 24.04 (got ${os_id:-unknown})" ;;
esac
DEB="$ROOT/ubuntu-${os_id}/secretnetwork_1.27.2_TRINITY_goleveldb_amd64_ubuntu-${os_id}.deb"
[[ -s "$DEB" ]] || die "missing $DEB"
expect="$(tr -d '[:space:]' < "$ROOT/H.txt")"
echo "package      $DEB"
echo "measurement  $expect"
sudo dpkg -i "$DEB"
got="$(secretd version 2>/dev/null | head -1 || true)"
[[ "$got" == "1.27.2" ]] || die "secretd version is ${got:-empty} (want 1.27.2)"
echo "OK  secretd 1.27.2"
