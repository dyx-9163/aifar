#!/usr/bin/env sh
set -eu

info() {
  echo "[INFO] $1"
}

warn() {
  echo "[WARN] $1"
}

fail() {
  echo "[ERROR] $1"
  exit 1
}

WORK_DIR="${AIFAR_WORK_DIR:?}"
DEPLOY_ROOT="$(dirname "$(dirname "$WORK_DIR")")"
INSTALL_ROOT="$DEPLOY_ROOT/mysql-offline-standalone"
MYSQL_BASE="$INSTALL_ROOT/mysql"
CONFIG_DIR="$INSTALL_ROOT/conf"
DATA_DIR="$INSTALL_ROOT/data"
LOG_DIR="$INSTALL_ROOT/logs"
RUN_DIR="$INSTALL_ROOT/run"
IMPORT_DIR="$INSTALL_ROOT/import"
SECRETS_DIR="$INSTALL_ROOT/secrets"
CONFIG_FILE="$CONFIG_DIR/my.cnf"
SOCKET_FILE="$RUN_DIR/mysql.sock"
SECRET_FILE="$SECRETS_DIR/mysql-admin.env"
SERVICE_NAME="aifar-orchestration-mysql"
MYSQL_USER="aifar-mysql-orch"
ADMIN_USER="aifar_admin"
PORT=3306
ARCHIVE="$WORK_DIR/files/mysql-aifar-8.0.36-official-bundle.tar"
RPM_GPG_KEY="$WORK_DIR/files/RPM-GPG-KEY-openEuler"
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  SUDO="sudo -n"
fi

cleanup_sensitive() {
  rm -f "$WORK_DIR/mysql-admin.env" "$WORK_DIR/secure-mysql.sql"
}
trap cleanup_sensitive EXIT

diagnostics() {
  $SUDO systemctl --no-pager --full status "$SERVICE_NAME" || true
  $SUDO journalctl -u "$SERVICE_NAME" -n 120 --no-pager || true
  if [ -f "$LOG_DIR/mysql-error.log" ]; then
    $SUDO tail -n 160 "$LOG_DIR/mysql-error.log" || true
  fi
  if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null || echo Disabled)" != "Disabled" ]; then
    echo "SELinux mode: $(getenforce 2>/dev/null || echo unknown)"
    $SUDO ls -ldZ "$INSTALL_ROOT" "$MYSQL_BASE" "$MYSQL_BASE/bin/mysqld" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" || true
    if command -v ausearch >/dev/null 2>&1; then
      echo "recent SELinux AVC denials:"
      $SUDO ausearch -m AVC,USER_AVC -ts recent 2>/dev/null | tail -n 80 || true
    fi
  fi
  $SUDO systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
}

selinux_is_enabled() {
  command -v getenforce >/dev/null 2>&1 || return 1
  [ "$(getenforce 2>/dev/null || echo Disabled)" != "Disabled" ]
}

set_persistent_fcontext() {
  context_type="$1"
  path_pattern="$2"
  if $SUDO semanage fcontext -a -t "$context_type" "$path_pattern" >/dev/null 2>&1; then
    return 0
  fi
  $SUDO semanage fcontext -m -t "$context_type" "$path_pattern" >/dev/null 2>&1
}

apply_selinux_contexts() {
  if ! selinux_is_enabled; then
    info "SELinux is disabled; file context configuration skipped"
    return 0
  fi

  info "configuring SELinux contexts for the custom MySQL installation"
  if command -v semanage >/dev/null 2>&1; then
    set_persistent_fcontext usr_t "$INSTALL_ROOT(/.*)?" || fail "failed to register SELinux usr_t context"
    set_persistent_fcontext bin_t "$MYSQL_BASE/bin(/.*)?" || fail "failed to register SELinux bin_t context"
    set_persistent_fcontext lib_t "$MYSQL_BASE/lib(/.*)?" || fail "failed to register SELinux lib_t context"
    set_persistent_fcontext mysqld_exec_t "$MYSQL_BASE/bin/mysqld" || fail "failed to register SELinux mysqld_exec_t context"
    set_persistent_fcontext mysqld_etc_t "$CONFIG_DIR(/.*)?" || fail "failed to register SELinux mysqld_etc_t context"
    set_persistent_fcontext mysqld_db_t "$DATA_DIR(/.*)?" || fail "failed to register SELinux mysqld_db_t context"
    set_persistent_fcontext mysqld_log_t "$LOG_DIR(/.*)?" || fail "failed to register SELinux mysqld_log_t context"
    set_persistent_fcontext mysqld_var_run_t "$RUN_DIR(/.*)?" || fail "failed to register SELinux mysqld_var_run_t context"
    set_persistent_fcontext mysqld_db_t "$IMPORT_DIR(/.*)?" || fail "failed to register SELinux import context"
    $SUDO restorecon -RF "$INSTALL_ROOT" || fail "failed to restore persistent SELinux contexts"
    info "persistent SELinux contexts applied"
  else
    warn "semanage is unavailable; applying non-persistent SELinux labels with chcon"
    $SUDO chcon -R -t usr_t "$INSTALL_ROOT" || fail "failed to label MySQL installation root"
    $SUDO chcon -R -t bin_t "$MYSQL_BASE/bin" || fail "failed to label MySQL binaries"
    $SUDO chcon -R -t lib_t "$MYSQL_BASE/lib" || fail "failed to label MySQL libraries"
    $SUDO chcon -t mysqld_exec_t "$MYSQL_BASE/bin/mysqld" || fail "failed to label mysqld executable"
    $SUDO chcon -R -t mysqld_etc_t "$CONFIG_DIR" || fail "failed to label MySQL configuration"
    $SUDO chcon -R -t mysqld_db_t "$DATA_DIR" "$IMPORT_DIR" || fail "failed to label MySQL data directories"
    $SUDO chcon -R -t mysqld_log_t "$LOG_DIR" || fail "failed to label MySQL log directory"
    $SUDO chcon -R -t mysqld_var_run_t "$RUN_DIR" || fail "failed to label MySQL runtime directory"
    warn "SELinux labels are active but not registered for a future full filesystem relabel"
  fi
}

[ -f "$ARCHIVE" ] || fail "MySQL offline bundle is missing: $ARCHIVE"
[ -f "$RPM_GPG_KEY" ] || fail "openEuler RPM signing key is missing: $RPM_GPG_KEY"
command -v rpm >/dev/null 2>&1 || fail "rpm command is required"

set -- "$WORK_DIR"/rpms/*.rpm
[ -f "$1" ] || fail "offline RPM dependencies are missing"
info "importing the official openEuler RPM signing key"
$SUDO rpm --import "$RPM_GPG_KEY" 2>&1 || fail "openEuler RPM signing key import failed"
info "verifying offline RPM signatures"
for package_file in "$@"; do
  $SUDO rpm --checksig "$package_file" 2>&1 || fail "RPM signature verification failed: $package_file"
done
info "installing offline RPM dependencies"
$SUDO rpm -Uvh --replacepkgs "$@" 2>&1 || fail "offline RPM dependency installation failed"
command -v tar >/dev/null 2>&1 || fail "tar is still unavailable after installing offline RPMs"

info "extracting MySQL official bundle"
rm -rf "$WORK_DIR/bundle" "$WORK_DIR/unpacked"
mkdir -p "$WORK_DIR/bundle" "$WORK_DIR/unpacked"
tar -xf "$ARCHIVE" -C "$WORK_DIR/bundle"
MYSQL_ARCHIVE="$(find "$WORK_DIR/bundle" -maxdepth 1 -type f \( -name 'mysql-*-minimal.tar.xz' -o -name 'mysql-*-linux*.tar.xz' -o -name 'mysql-*-linux*.tar.gz' \) | sort | head -n 1)"
[ -n "$MYSQL_ARCHIVE" ] && [ -f "$MYSQL_ARCHIVE" ] || fail "MySQL server archive was not found in the official bundle"
tar -xf "$MYSQL_ARCHIVE" -C "$WORK_DIR/unpacked"
MYSQL_SOURCE="$(find "$WORK_DIR/unpacked" -maxdepth 1 -type d -name 'mysql-*' | sort | head -n 1)"
[ -n "$MYSQL_SOURCE" ] && [ -x "$MYSQL_SOURCE/bin/mysqld" ] || fail "mysqld was not found after extraction"

NOLOGIN="/sbin/nologin"
[ -x /usr/sbin/nologin ] && NOLOGIN="/usr/sbin/nologin"
if ! id "$MYSQL_USER" >/dev/null 2>&1; then
  info "creating MySQL runtime user"
  $SUDO useradd --system --home-dir "$INSTALL_ROOT" --shell "$NOLOGIN" "$MYSQL_USER"
fi

info "preparing installation directories"
$SUDO mkdir -p "$INSTALL_ROOT" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" "$IMPORT_DIR" "$SECRETS_DIR" /etc/systemd/system
$SUDO chmod 0700 "$SECRETS_DIR"
$SUDO systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
$SUDO rm -rf "$MYSQL_BASE.new"
$SUDO mkdir -p "$MYSQL_BASE.new"
$SUDO cp -a "$MYSQL_SOURCE"/. "$MYSQL_BASE.new"/
$SUDO rm -rf "$MYSQL_BASE"
$SUDO mv "$MYSQL_BASE.new" "$MYSQL_BASE"
$SUDO chmod 0755 "$INSTALL_ROOT" "$MYSQL_BASE" "$MYSQL_BASE/bin" "$MYSQL_BASE/bin/mysqld"

cat > "$WORK_DIR/my.cnf" <<CONF
[mysqld]
basedir=$MYSQL_BASE
datadir=$DATA_DIR
port=$PORT
bind-address=0.0.0.0
socket=$SOCKET_FILE
pid-file=$RUN_DIR/mysqld.pid
log-error=$LOG_DIR/mysql-error.log
mysqlx=0
skip_name_resolve=ON
character-set-server=utf8mb4
collation-server=utf8mb4_0900_ai_ci
innodb_buffer_pool_size=256M
max_connections=100
secure-file-priv=$IMPORT_DIR

[client]
port=$PORT
socket=$SOCKET_FILE
default-character-set=utf8mb4
CONF
$SUDO install -m 0644 "$WORK_DIR/my.cnf" "$CONFIG_FILE"
$SUDO chown -R "$MYSQL_USER:$MYSQL_USER" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" "$IMPORT_DIR"

NEED_SECURE=0
if [ ! -d "$DATA_DIR/mysql" ]; then
  info "initializing a new MySQL data directory"
  NEED_SECURE=1
  MYSQL_PASSWORD="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
  [ -n "$MYSQL_PASSWORD" ] || fail "failed to generate MySQL administrator password"
  umask 077
  cat > "$WORK_DIR/mysql-admin.env" <<SECRET
MYSQL_ADMIN_USER=$ADMIN_USER
MYSQL_ADMIN_PASSWORD=$MYSQL_PASSWORD
SECRET
  $SUDO install -m 0600 "$WORK_DIR/mysql-admin.env" "$SECRET_FILE"
  rm -f "$WORK_DIR/mysql-admin.env"
  $SUDO rm -rf "$DATA_DIR"/*
  $SUDO "$MYSQL_BASE/bin/mysqld" --defaults-file="$CONFIG_FILE" --initialize-insecure --user="$MYSQL_USER"
  $SUDO chown -R "$MYSQL_USER:$MYSQL_USER" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" "$IMPORT_DIR"
else
  info "existing MySQL data directory detected"
  [ -s "$SECRET_FILE" ] || fail "existing data directory has no managed credential file: $SECRET_FILE"
  SECRET_CONTENT="$($SUDO cat "$SECRET_FILE")"
  MYSQL_PASSWORD="$(printf '%s\n' "$SECRET_CONTENT" | sed -n 's/^MYSQL_ADMIN_PASSWORD=//p')"
  [ -n "$MYSQL_PASSWORD" ] || fail "managed credential file does not contain MYSQL_ADMIN_PASSWORD"
fi

apply_selinux_contexts

cat > "$WORK_DIR/$SERVICE_NAME.service" <<SERVICE
[Unit]
Description=AIFAR Orchestration MySQL 8.0.36
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=$MYSQL_USER
Group=$MYSQL_USER
WorkingDirectory=$INSTALL_ROOT
Environment=LD_LIBRARY_PATH=$MYSQL_BASE/lib:$MYSQL_BASE/lib/private
ExecStart=$MYSQL_BASE/bin/mysqld --defaults-file=$CONFIG_FILE
Restart=on-failure
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
SERVICE
$SUDO install -m 0644 "$WORK_DIR/$SERVICE_NAME.service" "/etc/systemd/system/$SERVICE_NAME.service"
if selinux_is_enabled; then
  $SUDO restorecon "/etc/systemd/system/$SERVICE_NAME.service" || fail "failed to label MySQL systemd unit"
fi
$SUDO systemctl daemon-reload
$SUDO systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
if ! $SUDO systemctl enable --now "$SERVICE_NAME" 2>&1; then
  diagnostics
  fail "MySQL systemd service failed to start"
fi

info "waiting for MySQL readiness"
READY=0
i=1
while [ "$i" -le 120 ]; do
  if MYSQL_PWD="$MYSQL_PASSWORD" "$MYSQL_BASE/bin/mysqladmin" --protocol=tcp -h 127.0.0.1 -P "$PORT" -u "$ADMIN_USER" ping >/dev/null 2>&1; then
    READY=1
    NEED_SECURE=0
    break
  fi
  if "$MYSQL_BASE/bin/mysqladmin" --protocol=socket --socket="$SOCKET_FILE" -uroot ping >/dev/null 2>&1; then
    READY=1
    NEED_SECURE=1
    break
  fi
  i=$((i + 1))
  sleep 1
done
if [ "$READY" -ne 1 ]; then
  diagnostics
  fail "MySQL did not become ready within 120 seconds"
fi

if [ "$NEED_SECURE" -eq 1 ]; then
  info "configuring local root and remote administration accounts"
  umask 077
  cat > "$WORK_DIR/secure-mysql.sql" <<SQL
CREATE USER IF NOT EXISTS '$ADMIN_USER'@'%' IDENTIFIED BY '$MYSQL_PASSWORD';
ALTER USER '$ADMIN_USER'@'%' IDENTIFIED BY '$MYSQL_PASSWORD';
GRANT ALL PRIVILEGES ON *.* TO '$ADMIN_USER'@'%' WITH GRANT OPTION;
ALTER USER 'root'@'localhost' IDENTIFIED BY '$MYSQL_PASSWORD';
FLUSH PRIVILEGES;
SQL
  "$MYSQL_BASE/bin/mysql" --protocol=socket --socket="$SOCKET_FILE" -uroot < "$WORK_DIR/secure-mysql.sql"
  rm -f "$WORK_DIR/secure-mysql.sql"
fi

if ! MYSQL_PWD="$MYSQL_PASSWORD" "$MYSQL_BASE/bin/mysqladmin" --protocol=tcp -h 127.0.0.1 -P "$PORT" -u "$ADMIN_USER" ping >/dev/null 2>&1; then
  diagnostics
  fail "MySQL administrator verification failed"
fi

$SUDO "$MYSQL_BASE/bin/mysqld" --version
info "MySQL service installed: $SERVICE_NAME"
info "MySQL endpoint: 0.0.0.0:$PORT"
info "MySQL administrator: $ADMIN_USER"
info "credential file: $SECRET_FILE"
info "MySQL installation completed"
