#!/usr/bin/env bash
# Build a deployment tar.gz containing contrabass.manifest.yaml, contrabass-moleU-control,
# contrabass-moleU-compute, and agent.local.yml (see maintenance/packaging/contrabass.manifest.yaml.template).
#
# Usage:
#   ./maintenance/scripts/pack-agent-tarball.sh [control-binary] [compute-binary] [config-path] [output.tar.gz]
#
# Defaults:
#   control:  ./build/image/contrabass-moleU-control
#   compute:  ./build/image/contrabass-moleU-compute
#   config:   ./cfg/agent.local.yml
#   output:   ./dist/contrabass-agent-<version-key>.tar.gz  (from binary `agent --version`; slashes → '-')
#
# Default output name uses the version key from **`agent --version`** on both control and
# compute binaries (must match). Does not call build-version.sh / git describe.
#
# Requires: sha256sum, tar

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

BINARY_NAME="contrabass-moleU"
BINARY_CONTROL="${1:-./build/image/contrabass-moleU-control}"
BINARY_COMPUTE="${2:-./build/image/contrabass-moleU-compute}"
CONFIG="${3:-./cfg/agent.local.yml}"
OUT_ARG="${4:-}"

resolve_path() {
	local p="$1"
	if [[ "$p" = /* ]]; then
		printf '%s' "$p"
	else
		printf '%s' "$ROOT/$p"
	fi
}

version_key_from_binary() {
	local bin="$1"
	local line key
	line=$("$bin" agent --version 2>/dev/null) || return 1
	line=$(printf '%s' "$line" | tr -d '\r' | head -n 1)
	case "$line" in
		"${BINARY_NAME}"*)
			key=${line#${BINARY_NAME} }
			key=${key%% (*}
			key=$(printf '%s' "$key" | sed 's/[[:space:]]*$//')
			[[ -n "$key" ]] || return 1
			printf '%s' "$key"
			;;
		*) return 1 ;;
	esac
}

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
for f in "$BINARY_CONTROL" "$BINARY_COMPUTE" "$CONFIG"; do
	if [[ ! -f "$f" ]]; then
		echo "pack-agent-tarball: file not found: $f" >&2
		exit 1
	fi
done

CONTROL_ABS=$(resolve_path "$BINARY_CONTROL")
COMPUTE_ABS=$(resolve_path "$BINARY_COMPUTE")

CONTROL_VER=$(version_key_from_binary "$CONTROL_ABS") || {
	echo "pack-agent-tarball: cannot read version key from control binary (agent --version): $BINARY_CONTROL" >&2
	exit 1
}
COMPUTE_VER=$(version_key_from_binary "$COMPUTE_ABS") || {
	echo "pack-agent-tarball: cannot read version key from compute binary (agent --version): $BINARY_COMPUTE" >&2
	exit 1
}
if [[ "$CONTROL_VER" != "$COMPUTE_VER" ]]; then
	echo "pack-agent-tarball: control/compute version keys differ: $CONTROL_VER vs $COMPUTE_VER" >&2
	exit 1
fi
VERSION_KEY="$CONTROL_VER"

CONTROL_SHA=$(sha256sum "$BINARY_CONTROL" | awk '{print $1}')
COMPUTE_SHA=$(sha256sum "$BINARY_COMPUTE" | awk '{print $1}')
CONFIG_SHA=$(sha256sum "$CONFIG" | awk '{print $1}')

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

cp -f "$BINARY_CONTROL" "$TMP/contrabass-moleU-control"
cp -f "$BINARY_COMPUTE" "$TMP/contrabass-moleU-compute"
chmod +x "$TMP/contrabass-moleU-control" "$TMP/contrabass-moleU-compute"
cp -f "$CONFIG" "$TMP/agent.local.yml"

sed -e "s/__CONTROL_SHA256__/${CONTROL_SHA}/g" \
	-e "s/__COMPUTE_SHA256__/${COMPUTE_SHA}/g" \
	-e "s/__CONFIG_SHA256__/${CONFIG_SHA}/g" \
	"$TEMPLATE" >"$TMP/$MANIFEST_NAME"

if [[ -n "$OUT_ARG" ]]; then
	if [[ "$OUT_ARG" = /* ]]; then
		OUT="$OUT_ARG"
	else
		OUT="$ROOT/$OUT_ARG"
	fi
else
	SAFE_VER=${VERSION_KEY//\//-}
	mkdir -p "$ROOT/dist"
	OUT="$ROOT/dist/contrabass-agent-${SAFE_VER}.tar.gz"
fi

mkdir -p "$(dirname "$OUT")"
tar -C "$TMP" -czf "$OUT" .

echo "pack-agent-tarball: wrote $OUT"
echo "pack-agent-tarball: members:"
tar -tzf "$OUT" | sed 's/^/  /'
