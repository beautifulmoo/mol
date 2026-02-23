#!/bin/bash
set -euo pipefail

SERVICE=mol.service
BASE=/opt/mol

[ -L "$BASE/previous" ] || {
    echo "no previous version"
    exit 1
}

systemctl stop $SERVICE

ln -sfn "$(readlink $BASE/previous)" "$BASE/current"

systemctl start $SERVICE

echo "rollback completed"

