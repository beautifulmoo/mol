#!/bin/bash
set -euo pipefail
# systemd-run 유닛은 PATH가 비어 있을 수 있음. config 읽기(grep/sed) 전에 보강.
export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"

# 스크립트는 ${deploy_base}/current/ 아래에 두고 실행한다 (에이전트가 내장 스크립트를 이 경로에 풀어 씀).
# SCRIPT_DIR = versions/<버전>/ 또는 current가 가리키는 디렉터리, BASE = 그 부모 = 배포 루트.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE="$(cd "$SCRIPT_DIR/.." && pwd)"
HISTORY_LOG="$BASE/update_history.log"

# 헬스체크: 병합·추가 라이브러리 링크 등으로 기동이 느릴 수 있음. 짧은 대기는 오탐 실패(롤백)를 유발한다.
HEALTH_INITIAL_SLEEP="${HEALTH_INITIAL_SLEEP:-5}"
HEALTH_RETRY_INTERVAL="${HEALTH_RETRY_INTERVAL:-5}"
HEALTH_MAX_ATTEMPTS="${HEALTH_MAX_ATTEMPTS:-72}"
SERVICE_ACTIVE_MAX_ATTEMPTS="${SERVICE_ACTIVE_MAX_ATTEMPTS:-45}"
SERVICE_ACTIVE_INTERVAL="${SERVICE_ACTIVE_INTERVAL:-2}"

cleanup_scripts() {
	rm -f "$SCRIPT_DIR/update.sh" "$SCRIPT_DIR/rollback.sh"
}

append_history() {
    local line="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    local lockfile="${HISTORY_LOG}.lock"
    mkdir -p "$(dirname "$HISTORY_LOG")" 2>/dev/null || true
    (
        flock -w 30 9 || exit 1
        printf '%s\n' "$line" >> "$HISTORY_LOG"
    ) 9>"$lockfile"
}

invoke_rollback() {
    local reason="$1"
    append_history "update $NEW_VERSION failed ($reason), invoking rollback"
    echo "update failed ($reason), invoking rollback.sh" >&2
    set +e
    /bin/bash "$SCRIPT_DIR/rollback.sh"
    local rc=$?
    set -e
    if [ "$rc" -eq 0 ]; then
        append_history "rollback completed after update failure"
    else
        append_history "rollback failed (exit $rc) after update failure; check current symlink and journal"
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
        if HEALTH_RAW=$(curl -sSf --connect-timeout 5 --max-time 15 "$url" 2>/dev/null); then
            return 0
        fi
        HEALTH_RAW=""
        sleep "$HEALTH_RETRY_INTERVAL"
        attempt=$((attempt + 1))
    done
    return 1
}

# agent.local.yml scalar (InstallPrefix / DeployBase / MaintenancePort 등)
yaml_scalar() {
    local key="$1" file="$2"
    [ -f "$file" ] || return 0
    grep -E "^[[:space:]]*${key}:[[:space:]]*" "$file" 2>/dev/null | head -1 | \
        sed -E 's/^[[:space:]]*[^:]+:[[:space:]]*//' | sed -E 's/^["'\''"]//;s/["'\''"]$//' | tr -d '\r' || true
}

# Go versionsapi.VersionsBaseFromParts: InstallPrefix, else DeployBase, else BASE.
install_root_from_cfg() {
    local cfg="$1"
    local inst dep root
    inst=$(yaml_scalar InstallPrefix "$cfg")
    dep=$(yaml_scalar DeployBase "$cfg")
    if [ -n "$inst" ]; then
        root="$inst"
    elif [ -n "$dep" ]; then
        root="$dep"
    else
        root="$BASE"
    fi
    root="${root%/}"
    printf '%s' "$root"
}

# versions/<키> 디렉터리 (InstallPrefix·DeployBase·staging 순으로 탐색). Go가 기동 전 contrabass-moleU를 준비한다.
find_new_version_dir() {
    local ver="$1"
    local cfg="${2:-}"
    local roots=() r
    [ -n "$cfg" ] && [ -f "$cfg" ] && roots+=("$(install_root_from_cfg "$cfg")")
    roots+=("$BASE")
    local i
    for i in "${!roots[@]}"; do
        r="${roots[$i]}"
        [ -z "$r" ] && continue
        if [ -x "$r/versions/$ver/contrabass-moleU" ]; then
            printf '%s' "$r/versions/$ver"
            return 0
        fi
    done
    if [ -x "$BASE/staging/$ver/contrabass-moleU" ]; then
        printf '%s' "$BASE/staging/$ver"
        return 0
    fi
    return 1
}

set_current_link() {
    local ver="$1" dir="$2"
    if [ -d "$BASE/versions/$ver" ] && [ "$(cd "$BASE/versions/$ver" && pwd)" = "$(cd "$dir" && pwd)" ]; then
        ln -sfn "versions/$ver" "$BASE/current"
    else
        ln -sfn "$(cd "$dir" && pwd)" "$BASE/current"
    fi
}

NEW_VERSION="${1:?usage: update.sh <version>}"
_cfg_paths="$SCRIPT_DIR/agent.local.yml"
NEW_DIR=$(find_new_version_dir "$NEW_VERSION" "$_cfg_paths") || {
    append_history "update $NEW_VERSION failed: new binary not found under versions/ or staging"
    echo "new binary not found for version $NEW_VERSION (checked InstallPrefix/DeployBase from $_cfg_paths and $BASE)" >&2
    cleanup_scripts
    exit 1
}
VERSIONS="$(dirname "$NEW_DIR")"
NEW_BIN="$NEW_DIR/contrabass-moleU"

SERVICE=contrabass-mole.service
HTTP_PORT=
if [ -f "$NEW_DIR/agent.local.yml" ]; then
    v=$(yaml_scalar MaintenancePort "$NEW_DIR/agent.local.yml")
    [ -n "$v" ] && HTTP_PORT=$v
    v=$(yaml_scalar SystemctlServiceName "$NEW_DIR/agent.local.yml")
    [ -n "$v" ] && SERVICE=$v
fi
if [ -z "${HTTP_PORT:-}" ] && [ -f "$_cfg_paths" ]; then
    v=$(yaml_scalar MaintenancePort "$_cfg_paths")
    [ -n "$v" ] && HTTP_PORT=$v
    v=$(yaml_scalar SystemctlServiceName "$_cfg_paths")
    [ -n "$v" ] && SERVICE=$v
fi
if [ -z "${HTTP_PORT:-}" ]; then
    append_history "update $NEW_VERSION failed: MaintenancePort not found in agent.local.yml"
    echo "MaintenancePort not found in agent.local.yml" >&2
    cleanup_scripts
    exit 1
fi

append_history "update $NEW_VERSION started"

systemctl stop $SERVICE

systemctl is-active --quiet $SERVICE && {
    append_history "update $NEW_VERSION failed: service did not stop"
    echo "service did not stop" >&2
    cleanup_scripts
    exit 1
}

if [ -L "$BASE/current" ]; then
    ln -sfn "$(readlink "$BASE/current")" "$BASE/previous"
fi

set_current_link "$NEW_VERSION" "$NEW_DIR"

systemctl start $SERVICE

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

HEALTH_LINE=$(printf '%s' "$HEALTH_RAW" | tr -d '\r' | head -n 1 | sed 's/[[:space:]]*$//')
EXPECTED_LINE="contrabass-moleU ${NEW_VERSION}"
if [ "$HEALTH_LINE" != "$EXPECTED_LINE" ]; then
    invoke_rollback "health check: bad /version body (expected '${EXPECTED_LINE}', got '${HEALTH_LINE}')"
fi

append_history "update $NEW_VERSION success"
echo "update to $NEW_VERSION successful"
cleanup_scripts
