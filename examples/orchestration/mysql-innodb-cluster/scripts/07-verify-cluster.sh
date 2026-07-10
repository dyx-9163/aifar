#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

[ "${AIFAR_NODE_ROLE:-}" = "coordinator" ] || fail "cluster verification must run on the coordinator"

WORK_DIR="${AIFAR_WORK_DIR:?}"
DEPLOY_ROOT="$(dirname "$(dirname "$WORK_DIR")")"
INSTALL_ROOT="$DEPLOY_ROOT/mysql-innodb-cluster"
MYSQL_BASE="$INSTALL_ROOT/mysql"
MYSQLSH="$INSTALL_ROOT/mysql-shell/bin/mysqlsh"
ADMIN_USER="aifar_cluster_admin"
ADMIN_PASSWORD="${AIFAR_MYSQL_ADMIN_PASSWORD:?}"
CLUSTER_NAME="${AIFAR_CLUSTER_NAME:?}"
SEED_HOST="${AIFAR_CLUSTER_COORDINATOR_HOST:?}"
ROUTER_SERVICE="aifar-mysql-router"
JS_FILE="$WORK_DIR/verify-cluster.js"
SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo -n"; fi

[ -x "$MYSQLSH" ] || fail "mysqlsh is missing"
[ -x "$MYSQL_BASE/bin/mysql" ] || fail "mysql client is missing"
cleanup() { rm -f "$JS_FILE"; }
trap cleanup EXIT

umask 077
cat > "$JS_FILE" <<JS
const connection = {
  scheme: 'mysql',
  user: '$ADMIN_USER',
  password: '$ADMIN_PASSWORD',
  host: '$SEED_HOST',
  port: 3306
};
shell.connect(connection);
const cluster = dba.getCluster('$CLUSTER_NAME');
const status = cluster.status({ extended: 1 });
const topology = status.defaultReplicaSet && status.defaultReplicaSet.topology
  ? status.defaultReplicaSet.topology
  : {};
const members = Object.values(topology);
const online = members.filter(member => String(member.status || '').toUpperCase() === 'ONLINE');
const primary = members.filter(member => String(member.memberRole || '').toUpperCase() === 'PRIMARY');
if (members.length !== 3) {
  throw new Error('expected 3 cluster members, got ' + members.length);
}
if (online.length !== 3) {
  throw new Error('expected 3 ONLINE members, got ' + online.length);
}
if (primary.length !== 1) {
  throw new Error('expected exactly one PRIMARY member, got ' + primary.length);
}
print(JSON.stringify(status, null, 2));
JS

"$MYSQLSH" --js --file "$JS_FILE"
rm -f "$JS_FILE"

MYSQL_CLIENT="$MYSQL_BASE/bin/mysql"
ONLINE_COUNT="$(MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_CLIENT" --protocol=tcp -h "$SEED_HOST" -P 3306 -u "$ADMIN_USER" --batch --skip-column-names -e "SELECT COUNT(*) FROM performance_schema.replication_group_members WHERE MEMBER_STATE='ONLINE'")"
PRIMARY_COUNT="$(MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_CLIENT" --protocol=tcp -h "$SEED_HOST" -P 3306 -u "$ADMIN_USER" --batch --skip-column-names -e "SELECT COUNT(*) FROM performance_schema.replication_group_members WHERE MEMBER_ROLE='PRIMARY' AND MEMBER_STATE='ONLINE'")"
[ "$ONLINE_COUNT" -eq 3 ] || fail "performance_schema reports $ONLINE_COUNT ONLINE members"
[ "$PRIMARY_COUNT" -eq 1 ] || fail "performance_schema reports $PRIMARY_COUNT ONLINE PRIMARY members"
MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_CLIENT" --protocol=tcp -h "$SEED_HOST" -P 3306 -u "$ADMIN_USER" --table -e "SELECT MEMBER_HOST,MEMBER_PORT,MEMBER_STATE,MEMBER_ROLE FROM performance_schema.replication_group_members ORDER BY MEMBER_ROLE,MEMBER_HOST"

$SUDO systemctl is-active --quiet "$ROUTER_SERVICE" || fail "MySQL Router service is not active"
ROUTER_READY=0
attempt=1
while [ "$attempt" -le 60 ]; do
  if MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_CLIENT" --connect-timeout=3 --protocol=tcp -h 127.0.0.1 -P 6446 -u "$ADMIN_USER" --batch --skip-column-names -e "SELECT @@hostname,@@port" >/dev/null 2>&1; then
    ROUTER_READY=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
[ "$ROUTER_READY" -eq 1 ] || fail "MySQL Router read-write endpoint did not accept a query"
ROUTED_TARGET="$(MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_CLIENT" --protocol=tcp -h 127.0.0.1 -P 6446 -u "$ADMIN_USER" --batch --skip-column-names -e "SELECT CONCAT(@@hostname,':',@@port)")"

info "cluster verification passed: members=3 online=3 primary=1"
info "Router read-write endpoint ${AIFAR_NODE_HOST}:6446 routed to $ROUTED_TARGET"
info "Router read-only endpoint ${AIFAR_NODE_HOST}:6447"
