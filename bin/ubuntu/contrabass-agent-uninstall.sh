#!/bin/sh
set -eu

die() {
    echo "error: $*" >&2
    exit 1
}

usage() {
    echo "Usage: $0"
    echo "       Must be run as root (stops contrabass-mole.service and removes install paths)."
}

usage_exit() {
    usage
    if [ -n "${1:-}" ]; then
        echo "error: $1" >&2
    fi
    exit 1
}

if [ $# -ne 0 ]; then
    usage_exit "no arguments expected"
fi

if [ "$(id -u)" -ne 0 ]; then
    usage_exit "must be run as root (e.g. sudo $0)"
fi

for cmd in systemctl rm; do
    command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
done

INSTALL_DIR="/var/lib/contrabass/mole"
LOG_DIR="/var/log/contrabass/mole"
SERVICE_NAME="contrabass-mole.service"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}"

echo "----------------------------------------------------------------------------------------"
echo "Uninstalling Contrabass Mole"
echo "----------------------------------------------------------------------------------------"

if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "Stopping $SERVICE_NAME"
    systemctl stop "$SERVICE_NAME"
fi

if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "Disabling $SERVICE_NAME"
    systemctl disable "$SERVICE_NAME"
fi

if [ -f "$UNIT_FILE" ]; then
    echo "Removing $UNIT_FILE"
    rm -f "$UNIT_FILE"
fi

echo "Reloading systemd daemon"
systemctl daemon-reload
systemctl reset-failed "$SERVICE_NAME" 2>/dev/null || true

if [ -d "$INSTALL_DIR" ]; then
    echo "Removing $INSTALL_DIR"
    rm -rf "$INSTALL_DIR"
else
    echo "note: $INSTALL_DIR not found — skipping"
fi

if [ -d "$LOG_DIR" ]; then
    echo "Removing $LOG_DIR"
    rm -rf "$LOG_DIR"
else
    echo "note: $LOG_DIR not found — skipping"
fi

echo "Uninstall complete"
