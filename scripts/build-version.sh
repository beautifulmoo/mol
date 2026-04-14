#!/usr/bin/env bash
# Prints the full git describe string for go build -ldflags -X main.VersionKey=...
# Example: 0.4.4-4-gc44d420 (see git describe --tags --long --always).
# Version comparison in Go strips a trailing -g<hex> (git describe hash) and uses semver + patch like before.
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
echo "$d"
