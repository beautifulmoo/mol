#!/bin/sh
set -eu

die() {
    echo "error: $*" >&2
    exit 1
}

usage() {
    echo "Usage: $0 <tar.gz> <control|compute>"
    echo "       Must be run as root (writes under /var/lib/contrabass/mole and /etc/systemd/system)."
}

usage_exit() {
    usage
    if [ -n "${1:-}" ]; then
        echo "error: $1" >&2
    fi
    exit 1
}

if [ $# -lt 2 ]; then
    usage_exit "exactly two arguments required: tar.gz bundle path and control|compute"
fi

if [ "$(id -u)" -ne 0 ]; then
    usage_exit "must be run as root (e.g. sudo $0 <tar.gz> <control|compute>)"
fi

echo "----------------------------------------------------------------------------------------"
echo "Installing Contrabass Mole"
echo "----------------------------------------------------------------------------------------"

for cmd in tar cp ln chmod systemctl; do
    command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
done

HAS_SHA256SUM=0
if command -v sha256sum >/dev/null 2>&1; then
    HAS_SHA256SUM=1
else
    echo "warning: sha256sum not found — skipping manifest hash verification"
fi

TAR_FILE="$1"
ROLE="$2"

case "$ROLE" in
    control|compute) ;;
    *) die "ROLE must be control or compute: $ROLE" ;;
esac

[ -f "$TAR_FILE" ] || die "file not found: $TAR_FILE"
if ! tar -tzf "$TAR_FILE" > /dev/null 2>&1; then
    die "not a valid tar.gz archive: $TAR_FILE"
fi

INSTALL_DIR="/var/lib/contrabass/mole"
LOG_DIR="/var/log/contrabass/mole"
SERVICE_NAME="contrabass-mole.service"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}"
MANIFEST_NAME="contrabass.manifest.yaml"
BINARY_NAME="contrabass-moleU"
CONTROL_BIN="contrabass-moleU-control"
COMPUTE_BIN="contrabass-moleU-compute"
CONFIG_NAME="agent.local.yml"

file_sha256() {
    sha256sum "$1" | awk '{print tolower($1)}'
}

manifest_sha256() {
    section="$1"
    manifest="$2"
    awk -v s="$section" '
        $0 ~ "^" s ":" { insec=1; next }
        insec && /^[^[:space:]]/ && $0 !~ "^[[:space:]]" { insec=0 }
        insec && /sha256:/ {
            sub(/.*sha256:[[:space:]]*/, "")
            gsub(/["'\''"]/, "")
            print
            exit
        }
    ' "$manifest"
}

verify_manifest_hash() {
    section="$1"
    manifest="$2"
    target="$3"
    if [ "$HAS_SHA256SUM" -eq 0 ]; then
        return 0
    fi
    expected=$(manifest_sha256 "$section" "$manifest")
    [ -n "$expected" ] || die "manifest missing ${section}.sha256"
    actual=$(file_sha256 "$target")
    if [ "$actual" != "$(echo "$expected" | tr 'A-F' 'a-f')" ]; then
        die "${section} sha256 mismatch (manifest vs file)"
    fi
}

version_key_from_binary() {
    bin="$1"
    line=""
    line=$("$bin" agent --version 2>/dev/null) || return 1
    line=$(printf '%s' "$line" | tr -d '\r' | head -n 1)
    case "$line" in
        "${BINARY_NAME}"*)
            key=${line#${BINARY_NAME} }
            key=${key%% (*}
            key=$(printf '%s' "$key" | sed 's/[[:space:]]*$//')
            [ -n "$key" ] || return 1
            printf '%s' "$key"
            ;;
        *) return 1 ;;
    esac
}

TMP=$(mktemp -d)
cleanup() {
    rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

echo "Extracting $TAR_FILE to temporary directory"
tar -xzf "$TAR_FILE" -C "$TMP"

MANIFEST="$TMP/$MANIFEST_NAME"
[ -f "$MANIFEST" ] || die "bundle missing $MANIFEST_NAME"
grep -q '^manifestVersion:[[:space:]]*2' "$MANIFEST" || die "only manifestVersion 2 bundles are supported"

[ -f "$TMP/$CONTROL_BIN" ] || die "bundle missing $CONTROL_BIN"
[ -f "$TMP/$COMPUTE_BIN" ] || die "bundle missing $COMPUTE_BIN"
[ -f "$TMP/$CONFIG_NAME" ] || die "bundle missing $CONFIG_NAME"

verify_manifest_hash "agent_control" "$MANIFEST" "$TMP/$CONTROL_BIN"
verify_manifest_hash "agent_compute" "$MANIFEST" "$TMP/$COMPUTE_BIN"
verify_manifest_hash "config" "$MANIFEST" "$TMP/$CONFIG_NAME"

VERSION=$(version_key_from_binary "$TMP/$CONTROL_BIN") || die "cannot read version key from control binary"
compute_ver=$(version_key_from_binary "$TMP/$COMPUTE_BIN") || die "cannot read version key from compute binary"
if [ "$VERSION" != "$compute_ver" ]; then
    die "control/compute version keys differ: $VERSION vs $compute_ver"
fi

VERSION_DIR="$INSTALL_DIR/versions/$VERSION"
echo "Installing version $VERSION to $VERSION_DIR"

mkdir -p "$INSTALL_DIR/versions" "$INSTALL_DIR/staging"
rm -rf "$VERSION_DIR"
mkdir -p "$VERSION_DIR"

# flat bundle members → versions/<version-key>/
for item in "$TMP"/*; do
    [ -e "$item" ] || continue
    cp -a "$item" "$VERSION_DIR/"
done

if [ "$ROLE" = "control" ]; then
    echo "Materializing $CONTROL_BIN → $BINARY_NAME (control)"
    cp -f "$VERSION_DIR/$CONTROL_BIN" "$VERSION_DIR/$BINARY_NAME"
else
    echo "Materializing $COMPUTE_BIN → $BINARY_NAME (compute)"
    cp -f "$VERSION_DIR/$COMPUTE_BIN" "$VERSION_DIR/$BINARY_NAME"
fi
chmod 755 "$VERSION_DIR/$BINARY_NAME" "$VERSION_DIR/$CONTROL_BIN" "$VERSION_DIR/$COMPUTE_BIN"

echo "Creating current → versions/$VERSION"
ln -sfn "versions/$VERSION" "$INSTALL_DIR/current"

echo "Creating $LOG_DIR if it doesn't exist"
mkdir -p "$LOG_DIR"

echo "Creating systemd unit at $UNIT_FILE"
cat <<EOL > "$UNIT_FILE"
[Unit]
Description=Contrabass Mole Service
After=network.target

[Service]
Environment='LANG=C'
ExecStart=${INSTALL_DIR}/current/${BINARY_NAME} -cfg ${INSTALL_DIR}/current/${CONFIG_NAME}
Restart=on-failure
User=root
Group=root
Type=simple

[Install]
WantedBy=multi-user.target
EOL

chmod 644 "$UNIT_FILE"

echo "Reloading systemd daemon"
systemctl daemon-reload

echo "Starting and enabling $SERVICE_NAME"
systemctl enable "$SERVICE_NAME"
systemctl start "$SERVICE_NAME"

echo "Installation complete (version $VERSION, role $ROLE)"
