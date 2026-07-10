#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
warn() { echo "[WARN] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

WORK_DIR="${AIFAR_WORK_DIR:?}"
DEPLOY_ROOT="$(dirname "$(dirname "$WORK_DIR")")"
INSTALL_ROOT="$DEPLOY_ROOT/mysql-innodb-cluster"
MYSQL_BASE="$INSTALL_ROOT/mysql"
MYSQL_SHELL_BASE="$INSTALL_ROOT/mysql-shell"
CONFIG_DIR="$INSTALL_ROOT/conf"
DATA_DIR="$INSTALL_ROOT/data"
LOG_DIR="$INSTALL_ROOT/logs"
RUN_DIR="$INSTALL_ROOT/run"
IMPORT_DIR="$INSTALL_ROOT/import"
CONFIG_FILE="$CONFIG_DIR/my.cnf"
SOCKET_FILE="$RUN_DIR/mysql.sock"
SERVICE_NAME="aifar-mysql-innodb"
MYSQL_USER="aifar-mysql"
ADMIN_USER="aifar_cluster_admin"
ADMIN_PASSWORD="${AIFAR_MYSQL_ADMIN_PASSWORD:?}"
REPORT_HOST="${AIFAR_NODE_HOST:?}"
SERVER_ID="${AIFAR_NODE_INDEX:?}"
PORT=3306
MYSQLX_PORT=33060
GROUP_PORT=33061
ARCHIVE="$WORK_DIR/files/mysql-aifar-8.0.36-official-bundle.tar"
RPM_GPG_KEY="$WORK_DIR/files/RPM-GPG-KEY-openEuler"
SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo -n"; fi

cleanup_sensitive() {
  rm -f "$WORK_DIR/secure-mysql.sql" "$WORK_DIR/plugin.sql"
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
    $SUDO ls -ldZ "$INSTALL_ROOT" "$MYSQL_BASE/bin/mysqld" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" || true
    if command -v ausearch >/dev/null 2>&1; then
      $SUDO ausearch -m AVC,USER_AVC -ts recent 2>/dev/null | tail -n 80 || true
    fi
  fi
}

selinux_is_enabled() {
  command -v getenforce >/dev/null 2>&1 || return 1
  [ "$(getenforce 2>/dev/null || echo Disabled)" != "Disabled" ]
}

set_persistent_fcontext() {
  context_type="$1"
  path_pattern="$2"
  if $SUDO semanage fcontext -a -t "$context_type" "$path_pattern" >/dev/null 2>&1; then return 0; fi
  $SUDO semanage fcontext -m -t "$context_type" "$path_pattern" >/dev/null 2>&1
}

set_mysql_port_context() {
  port="$1"
  if $SUDO semanage port -a -t mysqld_port_t -p tcp "$port" >/dev/null 2>&1; then return 0; fi
  $SUDO semanage port -m -t mysqld_port_t -p tcp "$port" >/dev/null 2>&1
}

apply_selinux_contexts() {
  if ! selinux_is_enabled; then
    info "SELinux is disabled; file context configuration skipped"
    return 0
  fi
  info "configuring SELinux contexts for MySQL cluster node"
  if command -v semanage >/dev/null 2>&1; then
    set_persistent_fcontext usr_t "$INSTALL_ROOT(/.*)?" || fail "failed to register SELinux root context"
    set_persistent_fcontext bin_t "$MYSQL_BASE/bin(/.*)?" || fail "failed to register MySQL binary context"
    set_persistent_fcontext lib_t "$MYSQL_BASE/lib(/.*)?" || fail "failed to register MySQL library context"
    set_persistent_fcontext bin_t "$MYSQL_SHELL_BASE/bin(/.*)?" || fail "failed to register MySQL Shell binary context"
    set_persistent_fcontext lib_t "$MYSQL_SHELL_BASE/lib(/.*)?" || fail "failed to register MySQL Shell library context"
    set_persistent_fcontext mysqld_exec_t "$MYSQL_BASE/bin/mysqld" || fail "failed to register mysqld executable context"
    set_persistent_fcontext mysqld_etc_t "$CONFIG_DIR(/.*)?" || fail "failed to register MySQL config context"
    set_persistent_fcontext mysqld_db_t "$DATA_DIR(/.*)?" || fail "failed to register MySQL data context"
    set_persistent_fcontext mysqld_db_t "$IMPORT_DIR(/.*)?" || fail "failed to register MySQL import context"
    set_persistent_fcontext mysqld_log_t "$LOG_DIR(/.*)?" || fail "failed to register MySQL log context"
    set_persistent_fcontext mysqld_var_run_t "$RUN_DIR(/.*)?" || fail "failed to register MySQL runtime context"
    for mysql_port in "$PORT" "$MYSQLX_PORT" "$GROUP_PORT"; do
      set_mysql_port_context "$mysql_port" || fail "failed to register mysqld_port_t for $mysql_port/tcp"
    done
    $SUDO restorecon -RF "$INSTALL_ROOT" || fail "failed to restore SELinux contexts"
  else
    warn "semanage is unavailable; applying non-persistent SELinux labels with chcon"
    $SUDO chcon -R -t usr_t "$INSTALL_ROOT" || fail "failed to label installation root"
    $SUDO chcon -R -t bin_t "$MYSQL_BASE/bin" "$MYSQL_SHELL_BASE/bin" || fail "failed to label binaries"
    $SUDO chcon -R -t lib_t "$MYSQL_BASE/lib" "$MYSQL_SHELL_BASE/lib" || fail "failed to label libraries"
    $SUDO chcon -t mysqld_exec_t "$MYSQL_BASE/bin/mysqld" || fail "failed to label mysqld"
    $SUDO chcon -R -t mysqld_etc_t "$CONFIG_DIR" || fail "failed to label config"
    $SUDO chcon -R -t mysqld_db_t "$DATA_DIR" "$IMPORT_DIR" || fail "failed to label data"
    $SUDO chcon -R -t mysqld_log_t "$LOG_DIR" || fail "failed to label logs"
    $SUDO chcon -R -t mysqld_var_run_t "$RUN_DIR" || fail "failed to label runtime"
  fi
}

[ -f "$ARCHIVE" ] || fail "MySQL offline bundle is missing: $ARCHIVE"
[ -f "$RPM_GPG_KEY" ] || fail "openEuler RPM signing key is missing: $RPM_GPG_KEY"
command -v rpm >/dev/null 2>&1 || fail "rpm command is required"

set -- "$WORK_DIR"/rpms/*.rpm
[ -f "$1" ] || fail "offline RPM dependencies are missing"
info "importing the official openEuler RPM signing key"
$SUDO rpm --import "$RPM_GPG_KEY" 2>&1 || fail "openEuler RPM signing key import failed"
for package_file in "$@"; do
  $SUDO rpm --checksig "$package_file" 2>&1 || fail "RPM signature verification failed: $package_file"
done
info "installing offline RPM dependencies"
$SUDO rpm -Uvh --replacepkgs "$@" 2>&1 || fail "offline RPM dependency installation failed"
command -v tar >/dev/null 2>&1 || fail "tar is unavailable after installing offline RPMs"

info "extracting MySQL server and MySQL Shell"
rm -rf "$WORK_DIR/bundle" "$WORK_DIR/unpacked-server" "$WORK_DIR/unpacked-shell"
mkdir -p "$WORK_DIR/bundle" "$WORK_DIR/unpacked-server" "$WORK_DIR/unpacked-shell"
tar -xf "$ARCHIVE" -C "$WORK_DIR/bundle"
MYSQL_ARCHIVE="$(find "$WORK_DIR/bundle" -maxdepth 1 -type f \( -name 'mysql-*-minimal.tar.xz' -o -name 'mysql-*-linux*.tar.xz' -o -name 'mysql-*-linux*.tar.gz' \) | sort | head -n 1)"
MYSQL_SHELL_ARCHIVE="$(find "$WORK_DIR/bundle" -maxdepth 1 -type f \( -name 'mysql-shell-*-linux*.tar.xz' -o -name 'mysql-shell-*-linux*.tar.gz' \) | sort | head -n 1)"
[ -f "$MYSQL_ARCHIVE" ] || fail "MySQL server archive was not found in the bundle"
[ -f "$MYSQL_SHELL_ARCHIVE" ] || fail "MySQL Shell archive was not found in the bundle"
tar -xf "$MYSQL_ARCHIVE" -C "$WORK_DIR/unpacked-server"
tar -xf "$MYSQL_SHELL_ARCHIVE" -C "$WORK_DIR/unpacked-shell"
MYSQL_SOURCE="$(find "$WORK_DIR/unpacked-server" -maxdepth 1 -type d -name 'mysql-*' | sort | head -n 1)"
MYSQL_SHELL_SOURCE="$(find "$WORK_DIR/unpacked-shell" -maxdepth 1 -type d -name 'mysql-shell-*' | sort | head -n 1)"
[ -x "$MYSQL_SOURCE/bin/mysqld" ] || fail "mysqld was not found after extraction"
[ -x "$MYSQL_SHELL_SOURCE/bin/mysqlsh" ] || fail "mysqlsh was not found after extraction"

NOLOGIN="/sbin/nologin"
[ -x /usr/sbin/nologin ] && NOLOGIN="/usr/sbin/nologin"
if ! id "$MYSQL_USER" >/dev/null 2>&1; then
  $SUDO useradd --system --home-dir "$INSTALL_ROOT" --shell "$NOLOGIN" "$MYSQL_USER"
fi

info "preparing managed installation directories"
$SUDO mkdir -p "$INSTALL_ROOT" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" "$IMPORT_DIR" /etc/systemd/system
$SUDO systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
for component in mysql mysql-shell; do
  $SUDO rm -rf "$INSTALL_ROOT/$component.new"
  $SUDO mkdir -p "$INSTALL_ROOT/$component.new"
done
$SUDO cp -a "$MYSQL_SOURCE"/. "$MYSQL_BASE.new"/
$SUDO cp -a "$MYSQL_SHELL_SOURCE"/. "$MYSQL_SHELL_BASE.new"/
$SUDO rm -rf "$MYSQL_BASE" "$MYSQL_SHELL_BASE"
$SUDO mv "$MYSQL_BASE.new" "$MYSQL_BASE"
$SUDO mv "$MYSQL_SHELL_BASE.new" "$MYSQL_SHELL_BASE"
$SUDO chmod 0755 "$INSTALL_ROOT" "$MYSQL_BASE" "$MYSQL_BASE/bin" "$MYSQL_BASE/bin/mysqld" "$MYSQL_SHELL_BASE" "$MYSQL_SHELL_BASE/bin" "$MYSQL_SHELL_BASE/bin/mysqlsh"

cat > "$WORK_DIR/my.cnf" <<CONF
[mysqld]
basedir=$MYSQL_BASE
datadir=$DATA_DIR
port=$PORT
bind-address=0.0.0.0
report_host=$REPORT_HOST
server_id=$SERVER_ID
socket=$SOCKET_FILE
pid-file=$RUN_DIR/mysqld.pid
log-error=$LOG_DIR/mysql-error.log
mysqlx=ON
mysqlx_bind_address=0.0.0.0
mysqlx_port=$MYSQLX_PORT
skip_name_resolve=ON
character-set-server=utf8mb4
collation-server=utf8mb4_0900_ai_ci
secure-file-priv=$IMPORT_DIR
local_infile=OFF
gtid_mode=ON
enforce_gtid_consistency=ON
log_bin=$DATA_DIR/binlog
binlog_format=ROW
binlog_checksum=NONE
transaction_write_set_extraction=XXHASH64
binlog_transaction_dependency_tracking=WRITESET
relay_log=$DATA_DIR/relay-bin
relay_log_recovery=ON
log_replica_updates=ON
loose-group_replication_start_on_boot=OFF
loose-group_replication_bootstrap_group=OFF
loose-group_replication_exit_state_action=READ_ONLY
loose-group_replication_autorejoin_tries=3
loose-group_replication_member_expel_timeout=5
loose-group_replication_consistency=BEFORE_ON_PRIMARY_FAILOVER
innodb_buffer_pool_size=256M
innodb_log_buffer_size=16M
max_connections=100
table_open_cache=400
performance_schema=ON

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
  $SUDO rm -rf "$DATA_DIR"/*
  $SUDO "$MYSQL_BASE/bin/mysqld" --defaults-file="$CONFIG_FILE" --initialize-insecure --user="$MYSQL_USER"
  $SUDO chown -R "$MYSQL_USER:$MYSQL_USER" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" "$IMPORT_DIR"
else
  info "existing MySQL data directory detected; managed credential will be reused"
fi

apply_selinux_contexts

cat > "$WORK_DIR/$SERVICE_NAME.service" <<SERVICE
[Unit]
Description=AIFAR MySQL 8.0.36 InnoDB Cluster Node
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
TimeoutStartSec=180
LimitNOFILE=65536
PrivateTmp=true
NoNewPrivileges=true

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
  fail "MySQL cluster node service failed to start"
fi

info "waiting for MySQL readiness"
READY_MODE=""
i=1
while [ "$i" -le 180 ]; do
  if MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_BASE/bin/mysqladmin" --protocol=tcp -h 127.0.0.1 -P "$PORT" -u "$ADMIN_USER" ping >/dev/null 2>&1; then
    READY_MODE="managed"
    break
  fi
  if "$MYSQL_BASE/bin/mysqladmin" --protocol=socket --socket="$SOCKET_FILE" -uroot ping >/dev/null 2>&1; then
    READY_MODE="insecure"
    break
  fi
  i=$((i + 1))
  sleep 1
done
if [ -z "$READY_MODE" ]; then
  diagnostics
  fail "MySQL did not accept the managed cluster credential; rerun must use the credential created for this cluster name"
fi

if [ "$NEED_SECURE" -eq 1 ] || [ "$READY_MODE" = "insecure" ]; then
  info "creating the managed cluster administration account"
  umask 077
  cat > "$WORK_DIR/secure-mysql.sql" <<SQL
CREATE USER IF NOT EXISTS '$ADMIN_USER'@'%' IDENTIFIED BY '$ADMIN_PASSWORD';
ALTER USER '$ADMIN_USER'@'%' IDENTIFIED BY '$ADMIN_PASSWORD';
GRANT ALL PRIVILEGES ON *.* TO '$ADMIN_USER'@'%' WITH GRANT OPTION;
ALTER USER 'root'@'localhost' IDENTIFIED BY '$ADMIN_PASSWORD';
FLUSH PRIVILEGES;
SQL
  "$MYSQL_BASE/bin/mysql" --protocol=socket --socket="$SOCKET_FILE" -uroot < "$WORK_DIR/secure-mysql.sql"
  rm -f "$WORK_DIR/secure-mysql.sql"
fi

MYSQL_AUTH="$MYSQL_BASE/bin/mysql --protocol=tcp -h 127.0.0.1 -P $PORT -u $ADMIN_USER --batch --skip-column-names"
if ! MYSQL_PWD="$ADMIN_PASSWORD" $MYSQL_AUTH -e "SELECT 1" >/dev/null 2>&1; then
  diagnostics
  fail "managed MySQL cluster administrator verification failed"
fi

for plugin in clone group_replication; do
  if ! MYSQL_PWD="$ADMIN_PASSWORD" $MYSQL_AUTH -e "SELECT PLUGIN_NAME FROM INFORMATION_SCHEMA.PLUGINS WHERE PLUGIN_NAME='${plugin}'" | grep -qx "$plugin"; then
    case "$plugin" in
      clone) plugin_library="mysql_clone.so" ;;
      group_replication) plugin_library="group_replication.so" ;;
    esac
    printf "INSTALL PLUGIN %s SONAME '%s';\n" "$plugin" "$plugin_library" > "$WORK_DIR/plugin.sql"
    MYSQL_PWD="$ADMIN_PASSWORD" $MYSQL_AUTH < "$WORK_DIR/plugin.sql" || fail "failed to install MySQL plugin: $plugin"
    rm -f "$WORK_DIR/plugin.sql"
  fi
done

MYSQL_PWD="$ADMIN_PASSWORD" $MYSQL_AUTH -e "SELECT @@hostname,@@server_id,@@report_host,@@gtid_mode,@@port" 2>&1
$SUDO "$MYSQL_BASE/bin/mysqld" --version
$SUDO "$MYSQL_SHELL_BASE/bin/mysqlsh" --version
info "MySQL cluster node is ready: host=$REPORT_HOST server_id=$SERVER_ID ports=$PORT,$MYSQLX_PORT,$GROUP_PORT"
