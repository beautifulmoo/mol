#!/bin/bash
set -euo pipefail
export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"

# 스크립트가 있는 디렉터리 = mol 배포 루트
BASE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HISTORY_LOG="$BASE/update_history.log"
SERVICE=mol.service
if [ -L "$BASE/previous" ] && [ -f "$BASE/previous/config.yaml" ]; then
    v=$(grep -E '^systemctl_service_name:' "$BASE/previous/config.yaml" 2>/dev/null | head -1 | sed 's/.*:[[:space:]]*//' | sed 's/^["'\''"]//;s/["'\''"]$//' 2>/dev/null) || true
    [ -n "$v" ] && SERVICE=$v
fi

prepend_history() {
    local line="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    if [ -f "$HISTORY_LOG" ]; then
        echo "$line" > "${HISTORY_LOG}.tmp"
        cat "$HISTORY_LOG" >> "${HISTORY_LOG}.tmp"
        mv "${HISTORY_LOG}.tmp" "$HISTORY_LOG"
    else
        echo "$line" > "$HISTORY_LOG"
    fi
}

prepend_history "rollback started"

[ -L "$BASE/previous" ] || {
    prepend_history "rollback failed: no previous version"
    echo "no previous version"
    exit 1
}

systemctl stop $SERVICE || {
    prepend_history "rollback failed: service did not stop"
    exit 1
}

ln -sfn "$(readlink $BASE/previous)" "$BASE/current"

systemctl start $SERVICE || {
    prepend_history "rollback failed: service did not start"
    exit 1
}

prepend_history "rollback success"
echo "rollback completed"

