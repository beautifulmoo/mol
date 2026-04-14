#!/usr/bin/env bash
# Prints the agent version key "<semver>-<patch>" for go build -ldflags -X main.VersionKey=...
# Uses: git describe --tags --long --always
#   - tag + distance: 0.4.4-1-gabc1234 -> 0.4.4-1
#   - no tag (hash only): -> 0.0.0_dev-0 (underscore avoids hyphen/prerelease confusion)
#   - unparseable: -> 0.0.0-0
set -euo pipefail
cd "$(dirname "$0")/.."
d=$(git describe --tags --long --always 2>/dev/null || true)
d=$(echo "$d" | tr -d '\r\n')
if [[ -z "$d" ]]; then
	echo "0.0.0-0"
	exit 0
fi
# Pure abbreviated commit id (no tag)
if [[ "$d" =~ ^[0-9a-f]{7,40}$ ]]; then
	echo "0.0.0_dev-0"
	exit 0
fi
# tag-N-g<hash> (describe --long)
if [[ "$d" =~ ^(.+)-([0-9]+)-g[0-9a-f]+$ ]]; then
	tag="${BASH_REMATCH[1]}"
	n="${BASH_REMATCH[2]}"
	tag="${tag#v}"
	echo "${tag}-${n}"
	exit 0
fi
echo "0.0.0-0"
