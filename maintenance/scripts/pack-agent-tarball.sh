#!/usr/bin/env bash
# Build a deployment tar.gz containing contrabass.manifest.yaml, contrabass-moleU, config.yaml.
# Manifest paths are ./contrabass-moleU and ./config.yaml (see maintenance/packaging/contrabass.manifest.yaml.template).
#
# Usage:
#   ./maintenance/scripts/pack-agent-tarball.sh [binary-path] [config-path] [output.tar.gz]
#
# Defaults:
#   binary:   ./contrabass-moleU
#   config:   ./config.yaml
#   output:   ./dist/contrabass-agent-<git-describe>.tar.gz  (slashes in version → '-')
#
# Requires: sha256sum, tar

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

BINARY="${1:-./contrabass-moleU}"
CONFIG="${2:-./config.yaml}"
OUT_ARG="${3:-}"

TEMPLATE="$ROOT/maintenance/packaging/contrabass.manifest.yaml.template"
MANIFEST_NAME="contrabass.manifest.yaml"

for cmd in sha256sum tar; do
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "pack-agent-tarball: required command not found: $cmd" >&2
		exit 1
	fi
done

if [[ ! -f "$TEMPLATE" ]]; then
	echo "pack-agent-tarball: template not found: $TEMPLATE" >&2
	exit 1
fi
if [[ ! -f "$BINARY" ]]; then
	echo "pack-agent-tarball: binary not found: $BINARY" >&2
	exit 1
fi
if [[ ! -f "$CONFIG" ]]; then
	echo "pack-agent-tarball: config not found: $CONFIG" >&2
	exit 1
fi

AGENT_SHA=$(sha256sum "$BINARY" | awk '{print $1}')
CONFIG_SHA=$(sha256sum "$CONFIG" | awk '{print $1}')

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

cp -f "$BINARY" "$TMP/contrabass-moleU"
chmod +x "$TMP/contrabass-moleU"
cp -f "$CONFIG" "$TMP/config.yaml"

sed -e "s/__AGENT_SHA256__/${AGENT_SHA}/g" \
	-e "s/__CONFIG_SHA256__/${CONFIG_SHA}/g" \
	"$TEMPLATE" >"$TMP/$MANIFEST_NAME"

if [[ -n "$OUT_ARG" ]]; then
	if [[ "$OUT_ARG" = /* ]]; then
		OUT="$OUT_ARG"
	else
		OUT="$ROOT/$OUT_ARG"
	fi
else
	VERSION_KEY=$("$ROOT/maintenance/scripts/build-version.sh" 2>/dev/null || echo "0.0.0-0")
	SAFE_VER=${VERSION_KEY//\//-}
	mkdir -p "$ROOT/dist"
	OUT="$ROOT/dist/contrabass-agent-${SAFE_VER}.tar.gz"
fi

mkdir -p "$(dirname "$OUT")"
tar -C "$TMP" -czf "$OUT" .

echo "pack-agent-tarball: wrote $OUT"
echo "pack-agent-tarball: members:"
tar -tzf "$OUT" | sed 's/^/  /'
