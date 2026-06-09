#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
make "$@"
exec "$ROOT/maintenance/scripts/pack-agent-tarball.sh"
