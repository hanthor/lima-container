#!/bin/bash
# Download the IronRDP WASM package from npm and install to web/static/rdp/pkg/
set -euo pipefail

VERSION="${1:-1.0.1}"
DEST="web/static/rdp/pkg"

# Resolve to repo root (script may be called from any directory)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEST="$REPO_ROOT/$DEST"

mkdir -p "$DEST"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo "Downloading ironrdp-wasm@${VERSION}…"
cd "$WORK"
npm pack "ironrdp-wasm@${VERSION}" --quiet 2>/dev/null

TAR=$(ls ironrdp-wasm-*.tgz 2>/dev/null | head -1)
if [ -z "$TAR" ]; then
  echo "Error: npm pack failed — no tarball produced" >&2
  exit 1
fi

tar xzf "$TAR"

cp package/pkg/rdp_client.js "$DEST/"
cp package/pkg/rdp_client_bg.wasm "$DEST/"
cp package/pkg/rdp_client.d.ts "$DEST/" 2>/dev/null || true

echo "IronRDP WASM ${VERSION} installed to ${DEST}"
ls -lh "$DEST"/rdp_client*
