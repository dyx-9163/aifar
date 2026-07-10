#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
warn() { echo "[WARN] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

[ "${AIFAR_NODE_ROLE:-}" = "coordinator" ] || fail "MySQL Router must be installed on the coordinator"

WORK_DIR="${AIFAR_WORK_DIR:?}"
DEPLOY_ROOT="$(dirname "$(dirname "$WORK_DIR")")"
INSTALL_ROOT="$DEPLOY_ROOT/mysql-innodb-cluster"
ROUTER_BASE="$INSTALL_ROOT/mysql-router-bin"
ROUTER_DIR="$INSTALL_ROOT/router"
ROUTER_USER="aifar-mysql-router"
SERVICE_NAME="aifar-mysql-router"
ADMIN_USER="aifar_cluster_admin"
ADMIN_PASSWORD="${AIFAR_MYSQL_ADMIN_PASSWORD:?}"
SEED_HOST="${AIFAR_CLUSTER_COORDINATOR_HOST:?}"
CLUSTER_NAME="${AIFAR_CLUSTER_NAME:?}"
BASE_PORT=6446
ARCHIVE="$WORK_DIR/files/mysql-aifar-8.0.36-official-bundle.tar"
SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo -n"; fi

diagnostics() {
  $SUDO systemctl --no-pager --full status "$SERVICE_NAME" || true
  $SUDO journalctl -u "$SERVICE_NAME" -n 120 --no-pager || true
  if [ -f "$ROUTER_DIR/log/mysqlrouter.log" ]; then
    $SUDO tail -n 160 "$ROUTER_DIR/log/mysqlrouter.log" || true
  fi
}

[ -f "$ARCHIVE" ] || fail "MySQL offline bundle is missing"
mkdir -p "$WORK_DIR/router-bundle" "$WORK_DIR/unpacked-router"
if ! find "$WORK_DIR/router-bundle" -maxdepth 1 -type f -name 'mysql-router-*.tar.*' | grep -q .; then
  tar -xf "$ARCHIVE" -C "$WORK_DIR/router-bundle"
fi
ROUTER_ARCHIVE="$(find "$WORK_DIR/router-bundle" -maxdepth 1 -type f \( -name 'mysql-router-*-linux*.tar.xz' -o -name 'mysql-router-*-linux*.tar.gz' \) | sort | head -n 1)"
[ -f "$ROUTER_ARCHIVE" ] || fail "MySQL Router archive was not found in the bundle"
rm -rf "$WORK_DIR/unpacked-router"
mkdir -p "$WORK_DIR/unpacked-router"
tar -xf "$ROUTER_ARCHIVE" -C "$WORK_DIR/unpacked-router"
ROUTER_SOURCE="$(find "$WORK_DIR/unpacked-router" -maxdepth 1 -type d -name 'mysql-router-*' | sort | head -n 1)"
[ -x "$ROUTER_SOURCE/bin/mysqlrouter" ] || fail "mysqlrouter was not found after extraction"

NOLOGIN="/sbin/nologin"
[ -x /usr/sbin/nologin ] && NOLOGIN="/usr/sbin/nologin"
if ! id "$ROUTER_USER" >/dev/null 2>&1; then
  $SUDO useradd --system --home-dir "$ROUTER_DIR" --shell "$NOLOGIN" "$ROUTER_USER"
fi

$SUDO systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
$SUDO rm -rf "$ROUTER_BASE.new"
$SUDO mkdir -p "$ROUTER_BASE.new"
$SUDO cp -a "$ROUTER_SOURCE"/. "$ROUTER_BASE.new"/
$SUDO rm -rf "$ROUTER_BASE"
$SUDO mv "$ROUTER_BASE.new" "$ROUTER_BASE"
$SUDO chmod 0755 "$ROUTER_BASE" "$ROUTER_BASE/bin" "$ROUTER_BASE/bin/mysqlrouter"
$SUDO rm -rf "$ROUTER_DIR"
$SUDO mkdir -p "$(dirname "$ROUTER_DIR")"

if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null || echo Disabled)" != "Disabled" ]; then
  if command -v semanage >/dev/null 2>&1; then
    if ! $SUDO semanage fcontext -a -t bin_t "$ROUTER_BASE/bin(/.*)?" >/dev/null 2>&1; then
      $SUDO semanage fcontext -m -t bin_t "$ROUTER_BASE/bin(/.*)?" >/dev/null 2>&1 || fail "failed to register Router binary context"
    fi
    if ! $SUDO semanage fcontext -a -t lib_t "$ROUTER_BASE/lib(/.*)?" >/dev/null 2>&1; then
      $SUDO semanage fcontext -m -t lib_t "$ROUTER_BASE/lib(/.*)?" >/dev/null 2>&1 || fail "failed to register Router library context"
    fi
    $SUDO restorecon -RF "$ROUTER_BASE" || fail "failed to restore Router SELinux contexts"
  else
    warn "semanage is unavailable; applying non-persistent Router labels"
    $SUDO chcon -R -t bin_t "$ROUTER_BASE/bin" || fail "failed to label Router binaries"
    $SUDO chcon -R -t lib_t "$ROUTER_BASE/lib" || fail "failed to label Router libraries"
  fi
fi

info "bootstrapping MySQL Router against $SEED_HOST:3306"
if ! printf '%s\n' "$ADMIN_PASSWORD" | $SUDO "$ROUTER_BASE/bin/mysqlrouter" \
  --bootstrap "$ADMIN_USER@$SEED_HOST:3306" \
  --directory "$ROUTER_DIR" \
  --user "$ROUTER_USER" \
  --name "aifar-$CLUSTER_NAME" \
  --report-host "${AIFAR_NODE_HOST:?}" \
  --conf-base-port "$BASE_PORT" \
  --conf-bind-address 0.0.0.0 \
  --strict \
  --force; then
  fail "MySQL Router bootstrap failed"
fi
[ -f "$ROUTER_DIR/mysqlrouter.conf" ] || fail "Router bootstrap did not create mysqlrouter.conf"
$SUDO chown -R "$ROUTER_USER:$ROUTER_USER" "$ROUTER_DIR"
$SUDO chmod 0700 "$ROUTER_DIR" "$ROUTER_DIR/run" "$ROUTER_DIR/data" 2>/dev/null || true

cat > "$WORK_DIR/$SERVICE_NAME.service" <<SERVICE
[Unit]
Description=AIFAR MySQL Router for InnoDB Cluster
After=network-online.target aifar-mysql-innodb.service
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=$ROUTER_USER
Group=$ROUTER_USER
WorkingDirectory=$ROUTER_DIR
Environment=LD_LIBRARY_PATH=$ROUTER_BASE/lib:$ROUTER_BASE/lib/mysqlrouter
ExecStart=$ROUTER_BASE/bin/mysqlrouter -c $ROUTER_DIR/mysqlrouter.conf
Restart=on-failure
RestartSec=3
TimeoutStartSec=60
LimitNOFILE=65536
PrivateTmp=true
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
SERVICE
$SUDO install -m 0644 "$WORK_DIR/$SERVICE_NAME.service" "/etc/systemd/system/$SERVICE_NAME.service"
if command -v restorecon >/dev/null 2>&1; then
  $SUDO restorecon "/etc/systemd/system/$SERVICE_NAME.service" >/dev/null 2>&1 || true
fi
$SUDO systemctl daemon-reload
$SUDO systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
if ! $SUDO systemctl enable --now "$SERVICE_NAME" 2>&1; then
  diagnostics
  fail "MySQL Router systemd service failed to start"
fi

ready=0
attempt=1
while [ "$attempt" -le 60 ]; do
  if command -v ss >/dev/null 2>&1 && ss -lnt | awk '{print $4}' | grep -Eq "(:|\\.)${BASE_PORT}$"; then
    ready=1
    break
  fi
  sleep 1
  attempt=$((attempt + 1))
done
if [ "$ready" -ne 1 ]; then
  diagnostics
  fail "MySQL Router did not listen on port $BASE_PORT"
fi

$SUDO "$ROUTER_BASE/bin/mysqlrouter" --version
info "MySQL Router is ready: read-write=${AIFAR_NODE_HOST}:6446 read-only=${AIFAR_NODE_HOST}:6447"
