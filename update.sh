#!/bin/bash
set -euo pipefail
# systemd-run 유닛은 PATH가 비어 있을 수 있음. config 읽기(grep/sed) 전에 보강.
export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"

# 스크립트는 ${deploy_base}/current/ 아래에 두고 실행한다 (에이전트 바이너리가 내장 스크립트를 이 경로에 풀어 씀).
# SCRIPT_DIR = versions/<버전>/ 또는 current가 가리키는 디렉터리, BASE = 그 부모 = 배포 루트.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE="$(cd "$SCRIPT_DIR/.." && pwd)"
HISTORY_LOG="$BASE/update_history.log"

cleanup_scripts() {
	rm -f "$SCRIPT_DIR/update.sh" "$SCRIPT_DIR/rollback.sh"
}

# 맨 앞줄에 한 줄 추가 (새 기록이 최상단)
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

VERSIONS="$BASE/versions"
NEW_VERSION="${1:?usage: update.sh <version>}"
NEW_DIR="$VERSIONS/$NEW_VERSION"
# 실행 파일명은 appmeta.BinaryName(contrabass-moleU)와 동일해야 함
NEW_BIN="$NEW_DIR/contrabass-moleU"

# 1. 사전 체크
[ -x "$NEW_BIN" ] || {
    prepend_history "update $NEW_VERSION failed: new binary not found"
    echo "new binary not found: $NEW_BIN"
    cleanup_scripts
    exit 1
}

# 2. 적용할 버전의 config.yaml에서 설정 읽기. 실패해도 기본값 유지.
# HTTP_PORT = Maintenance.MaintenancePort (에이전트가 maintenance HTTP를 여는 포트, 예: 8889).
# Server.HTTPPort(예: 8888)는 Gin이 아닌 별도 바인딩이며, 브라우저는 보통 8888→maintenance로 리버스 프록시한다.
# GET /version 은 maintenance 리스너에서만 제공되므로 헬스체크는 MaintenancePort로 해야 한다(8888만 쓰면 /version 이 프록시되지 않아 404 등 잘못된 응답이 올 수 있음).
SERVICE=contrabass-mole.service
HTTP_PORT=
if [ -f "$NEW_DIR/config.yaml" ]; then
    v=$(grep -E '^[[:space:]]*MaintenancePort:[[:space:]]*[0-9]+' "$NEW_DIR/config.yaml" 2>/dev/null | head -1 | sed 's/.*:[[:space:]]*//' 2>/dev/null) || true
    [ -n "$v" ] && HTTP_PORT=$v
    v=$(grep -E '^[[:space:]]*SystemctlServiceName:' "$NEW_DIR/config.yaml" 2>/dev/null | head -1 | sed 's/.*:[[:space:]]*//' | sed 's/^["'\''"]//;s/["'\''"]$//' 2>/dev/null) || true
    [ -n "$v" ] && SERVICE=$v
fi
if [ -z "${HTTP_PORT:-}" ]; then
    prepend_history "update $NEW_VERSION failed: MaintenancePort not found in config.yaml"
    echo "MaintenancePort not found in config.yaml"
    cleanup_scripts
    exit 1
fi

prepend_history "update $NEW_VERSION started"

# 2. 서비스 중지
systemctl stop $SERVICE

systemctl is-active --quiet $SERVICE && {
    prepend_history "update $NEW_VERSION failed: service did not stop"
    echo "service did not stop"
    cleanup_scripts
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

# 6. 헬스 체크 (Restart= 시 재시작 루프에서도 is-active는 성공하므로, 실제 HTTP 응답으로 검사)
sleep 3
if ! systemctl is-active --quiet $SERVICE; then
    prepend_history "update $NEW_VERSION failed, rollback"
    echo "start failed, rollback"
    "$SCRIPT_DIR/rollback.sh"
    prepend_history "rollback completed"
    exit 1
fi
# GET /version 본문은 정확히 "<BinaryName> <버전 키>" 한 줄(예: contrabass-moleU 0.4.4-11). HTTP 200만으로는 부족함.
# curl 은 파이프 없이 단독 실행해 -f 실패(404 등)가 반드시 감지되게 한다.
HEALTH_RAW=""
if ! HEALTH_RAW=$(curl -sSf --connect-timeout 5 --max-time 10 "http://127.0.0.1:${HTTP_PORT}/version" 2>/dev/null); then
    prepend_history "update $NEW_VERSION failed (health check: curl /version), rollback"
    echo "health check failed (curl http://127.0.0.1:${HTTP_PORT}/version), rollback"
    "$SCRIPT_DIR/rollback.sh"
    prepend_history "rollback completed"
    exit 1
fi
HEALTH_LINE=$(printf '%s' "$HEALTH_RAW" | tr -d '\r' | head -n 1 | sed 's/[[:space:]]*$//')
EXPECTED_LINE="$(basename "$NEW_BIN") ${NEW_VERSION}"
if [ "$HEALTH_LINE" != "$EXPECTED_LINE" ]; then
    prepend_history "update $NEW_VERSION failed (health check: bad /version body), rollback"
    echo "health check failed: expected '${EXPECTED_LINE}', got '${HEALTH_LINE}' (MaintenancePort=${HTTP_PORT})"
    "$SCRIPT_DIR/rollback.sh"
    prepend_history "rollback completed"
    exit 1
fi

prepend_history "update $NEW_VERSION success"
echo "update to $NEW_VERSION successful"
cleanup_scripts
