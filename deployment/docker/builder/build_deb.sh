#!/bin/bash
set -euo pipefail
VERSION="${VERSION:-1.27.0}"
DEB_NET="${DEB_NET:-TRINITY}"
DEB_OS="${DEB_OS:-ubuntu-24.04}"
DB_BACKEND="${DB_BACKEND:-goleveldb}"
case "$DEB_NET" in
  TRINITY|TESTNET|MAINNET) ;;
  *) echo "FATAL: DEB_NET must be TRINITY|TESTNET|MAINNET (got $DEB_NET)" >&2; exit 1 ;;
esac
case "$DEB_OS" in
  ubuntu-22.04|ubuntu-24.04) ;;
  *) echo "FATAL: DEB_OS must be ubuntu-22.04 or ubuntu-24.04 (got $DEB_OS)" >&2; exit 1 ;;
esac
make deb-no-compile
NAME="secretnetwork_${VERSION}_${DEB_NET}_${DB_BACKEND}_amd64_${DEB_OS}.deb"
test -f "./$NAME" || { echo "FATAL: missing $NAME after deb-no-compile" >&2; exit 1; }
rm -f "./secretnetwork_${VERSION}_amd64.deb"
cp "./$NAME" /build/
rm -f "/build/secretnetwork_${VERSION}_amd64.deb"
echo "wrote /build/$NAME"
