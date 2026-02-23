#!/bin/bash
set -euo pipefail

SERVICE=mol.service
BASE=/opt/mol
VERSIONS=$BASE/versions
NEW_VERSION="$1"   # 예: 1.2.6

NEW_DIR="$VERSIONS/$NEW_VERSION"
NEW_BIN="$NEW_DIR/mol"

# 1. 사전 체크
[ -x "$NEW_BIN" ] || {
    echo "new binary not found: $NEW_BIN"
    exit 1
}

# 2. 서비스 중지
systemctl stop $SERVICE

systemctl is-active --quiet $SERVICE && {
    echo "service did not stop"
    exit 1
}

# 3. previous 갱신
if [ -L "$BASE/current" ]; then
    ln -sfn "$(readlink $BASE/current)" "$BASE/previous"
fi

# 4. current 교체 (원자적)
ln -sfn "versions/$NEW_VERSION" "$BASE/current"

# 5. 서비스 시작
systemctl start $SERVICE

# 6. 헬스 체크
sleep 3
if ! systemctl is-active --quiet $SERVICE; then
    echo "start failed, rollback"
    "$BASE/rollback.sh"
    exit 1
fi

# (선택) 바이너리 헬스 체크
#if ! "$BASE/current/aaa" --healthcheck; then
#    echo "healthcheck failed, rollback"
#    "$BASE/rollback.sh"
#    exit 1
#fi

echo "update to $NEW_VERSION successful"

