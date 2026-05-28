#!/bin/bash
set -euo pipefail
# systemd-run 유닛은 PATH가 비어 있을 수 있음. config 읽기(grep/sed) 전에 보강.
export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"

# 스크립트는 ${deploy_base}/current/ 아래에 두고 실행한다 (에이전트 바이너리가 내장 스크립트를 이 경로에 풀어 씀).
# SCRIPT_DIR = versions/<버전>/ 또는 current가 가리키는 디렉터리, BASE = 그 부모 = 배포 루트.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE="$(cd "$SCRIPT_DIR/.." && pwd)"
HISTORY_LOG="$BASE/update_history.log"

# 헬스체크: 느린 기동(의존 라이브러리 로드·병합 바이너리)에서 3초 단일 curl 로 오탐 실패하지 않도록 재시도.
HEALTH_INITIAL_SLEEP="${HEALTH_INITIAL_SLEEP:-2}"
HEALTH_RETRY_INTERVAL="${HEALTH_RETRY_INTERVAL:-3}"
HEALTH_MAX_ATTEMPTS="${HEALTH_MAX_ATTEMPTS:-20}"
SERVICE_ACTIVE_MAX_ATTEMPTS="${SERVICE_ACTIVE_MAX_ATTEMPTS:-15}"
SERVICE_ACTIVE_INTERVAL="${SERVICE_ACTIVE_INTERVAL:-2}"

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

# 업데이트 실패 시 rollback.sh 실행; 종료 코드에 따라 로그를 구분(성공 시에만 completed).
invoke_rollback() {
    local reason="$1"
    prepend_history "update $NEW_VERSION failed ($reason), invoking rollback"
    echo "update failed ($reason), invoking rollback.sh" >&2
    set +e
    "$SCRIPT_DIR/rollback.sh"
    local rc=$?
    set -e
    if [ "$rc" -eq 0 ]; then
        prepend_history "rollback completed after update failure"
    else
        prepend_history "rollback failed (exit $rc) after update failure; check current symlink and journal"
    fi
    cleanup_scripts
    exit 1
}

wait_for_service_active() {
    local attempt=1
    while [ "$attempt" -le "$SERVICE_ACTIVE_MAX_ATTEMPTS" ]; do
        if systemctl is-active --quiet "$SERVICE"; then
            return 0
        fi
        sleep "$SERVICE_ACTIVE_INTERVAL"
        attempt=$((attempt + 1))
    done
    return 1
}

# GET /version (MaintenancePort). 성공 시 HEALTH_RAW 설정. 0=ok, 1=not ready, 2=service down.
wait_for_version_http() {
    local attempt=1
    local url="http://127.0.0.1:${HTTP_PORT}/version"
    HEALTH_RAW=""
    sleep "$HEALTH_INITIAL_SLEEP"
    while [ "$attempt" -le "$HEALTH_MAX_ATTEMPTS" ]; do
        if ! systemctl is-active --quiet "$SERVICE"; then
            HEALTH_RAW=""
            return 2
        fi
        if HEALTH_RAW=$(curl -sSf --connect-timeout 3 --max-time 8 "$url" 2>/dev/null); then
            return 0
        fi
        HEALTH_RAW=""
        sleep "$HEALTH_RETRY_INTERVAL"
        attempt=$((attempt + 1))
    done
    return 1
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

# 2. 적용할 버전의 agent.local.yml에서 설정 읽기. 실패해도 기본값 유지.
# HTTP_PORT = Maintenance.MaintenancePort (에이전트가 maintenance HTTP를 여는 포트, 예: 8889).
# Server.HTTPPort(예: 8888)는 Gin이 아닌 별도 바인딩이며, 브라우저는 보통 8888→maintenance로 리버스 프록시한다.
# GET /version 은 maintenance 리스너에서만 제공되므로 헬스체크는 MaintenancePort로 해야 한다(8888만 쓰면 /version 이 프록시되지 않아 404 등 잘못된 응답이 올 수 있음).
SERVICE=contrabass-mole.service
HTTP_PORT=
if [ -f "$NEW_DIR/agent.local.yml" ]; then
    v=$(grep -E '^[[:space:]]*MaintenancePort:[[:space:]]*[0-9]+' "$NEW_DIR/agent.local.yml" 2>/dev/null | head -1 | sed 's/.*:[[:space:]]*//' 2>/dev/null) || true
    [ -n "$v" ] && HTTP_PORT=$v
    v=$(grep -E '^[[:space:]]*SystemctlServiceName:' "$NEW_DIR/agent.local.yml" 2>/dev/null | head -1 | sed 's/.*:[[:space:]]*//' | sed 's/^["'\''"]//;s/["'\''"]$//' 2>/dev/null) || true
    [ -n "$v" ] && SERVICE=$v
fi
if [ -z "${HTTP_PORT:-}" ]; then
    prepend_history "update $NEW_VERSION failed: MaintenancePort not found in agent.local.yml"
    echo "MaintenancePort not found in agent.local.yml"
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

# 6. 헬스 체크 (Restart= 시 재시작 루프에서도 is-active는 성공할 수 있으므로, 기동 대기 후 HTTP /version 재시도)
if ! wait_for_service_active; then
    invoke_rollback "service did not become active within $((SERVICE_ACTIVE_MAX_ATTEMPTS * SERVICE_ACTIVE_INTERVAL))s"
fi

HEALTH_RAW=""
wait_rc=0
wait_for_version_http || wait_rc=$?
if [ "$wait_rc" -eq 2 ]; then
    invoke_rollback "service stopped during health check"
fi
if [ "$wait_rc" -ne 0 ]; then
    max_wait=$((HEALTH_INITIAL_SLEEP + HEALTH_MAX_ATTEMPTS * HEALTH_RETRY_INTERVAL))
    invoke_rollback "health check: GET /version not ready after ~${max_wait}s (port ${HTTP_PORT})"
fi

# GET /version 본문은 정확히 "<BinaryName> <버전 키>" 한 줄(예: contrabass-moleU 0.4.4-11). HTTP 200만으로는 부족함.
HEALTH_LINE=$(printf '%s' "$HEALTH_RAW" | tr -d '\r' | head -n 1 | sed 's/[[:space:]]*$//')
EXPECTED_LINE="$(basename "$NEW_BIN") ${NEW_VERSION}"
if [ "$HEALTH_LINE" != "$EXPECTED_LINE" ]; then
    invoke_rollback "health check: bad /version body (expected '${EXPECTED_LINE}', got '${HEALTH_LINE}')"
fi

prepend_history "update $NEW_VERSION success"
echo "update to $NEW_VERSION successful"
cleanup_scripts
