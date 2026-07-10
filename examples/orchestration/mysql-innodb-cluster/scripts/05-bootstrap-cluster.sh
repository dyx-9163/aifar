#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

[ "${AIFAR_NODE_ROLE:-}" = "coordinator" ] || fail "cluster bootstrap must run on the coordinator"
[ "${AIFAR_CLUSTER_NODE_COUNT:-0}" -eq 3 ] || fail "cluster bootstrap requires exactly three nodes"

WORK_DIR="${AIFAR_WORK_DIR:?}"
DEPLOY_ROOT="$(dirname "$(dirname "$WORK_DIR")")"
INSTALL_ROOT="$DEPLOY_ROOT/mysql-innodb-cluster"
MYSQL_BASE="$INSTALL_ROOT/mysql"
MYSQLSH="$INSTALL_ROOT/mysql-shell/bin/mysqlsh"
ADMIN_USER="aifar_cluster_admin"
ADMIN_PASSWORD="${AIFAR_MYSQL_ADMIN_PASSWORD:?}"
CLUSTER_NAME="${AIFAR_CLUSTER_NAME:?}"
SEED_HOST="${AIFAR_CLUSTER_COORDINATOR_HOST:?}"
PORT=3306
JS_FILE="$WORK_DIR/bootstrap-cluster.js"

[ -x "$MYSQLSH" ] || fail "mysqlsh is missing: $MYSQLSH"
[ -x "$MYSQL_BASE/bin/mysqladmin" ] || fail "mysqladmin is missing"
case "$CLUSTER_NAME" in *[!A-Za-z0-9_-]*) fail "cluster name contains unsupported characters" ;; esac

cleanup() { rm -f "$JS_FILE"; }
trap cleanup EXIT

info "waiting until all MySQL nodes accept the managed credential"
i=1
while [ "$i" -le "$AIFAR_CLUSTER_NODE_COUNT" ]; do
  eval "node_host=\${AIFAR_CLUSTER_NODE_${i}_HOST:-}"
  case "$node_host" in *[!A-Za-z0-9._:-]*) fail "node host contains unsupported characters: $node_host" ;; esac
  ready=0
  attempt=1
  while [ "$attempt" -le 120 ]; do
    if MYSQL_PWD="$ADMIN_PASSWORD" "$MYSQL_BASE/bin/mysqladmin" --connect-timeout=3 --protocol=tcp -h "$node_host" -P "$PORT" -u "$ADMIN_USER" ping >/dev/null 2>&1; then
      ready=1
      break
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  [ "$ready" -eq 1 ] || fail "MySQL node is not reachable with the managed credential: $node_host:$PORT"
  info "MySQL node ready: $node_host:$PORT"
  i=$((i + 1))
done

NODES_JS="{ host: \"$SEED_HOST\", port: $PORT }"
i=1
while [ "$i" -le "$AIFAR_CLUSTER_NODE_COUNT" ]; do
  eval "node_host=\${AIFAR_CLUSTER_NODE_${i}_HOST:-}"
  if [ "$node_host" != "$SEED_HOST" ]; then
    NODES_JS="$NODES_JS, { host: \"$node_host\", port: $PORT }"
  fi
  i=$((i + 1))
done

umask 077
cat > "$JS_FILE" <<JS
const clusterName = "$CLUSTER_NAME";
const adminUser = "$ADMIN_USER";
const adminPassword = "$ADMIN_PASSWORD";
const nodes = [ $NODES_JS ];

function connection(node) {
  return { scheme: 'mysql', user: adminUser, password: adminPassword, host: node.host, port: node.port };
}

function endpoint(node) {
  return node.host + ':' + node.port;
}

function messageOf(error) {
  return String(error && error.message ? error.message : error);
}

for (const node of nodes) {
  print('checking InnoDB Cluster configuration: ' + endpoint(node));
  dba.checkInstanceConfiguration(connection(node));
  try {
    dba.configureInstance(connection(node), {
      clusterAdmin: adminUser,
      clusterAdminPassword: adminPassword,
      restart: true,
      interactive: false
    });
  } catch (error) {
    const message = messageOf(error);
    if (message.indexOf('already exists') >= 0 || message.indexOf('clusterAdminPassword is not allowed') >= 0) {
      dba.configureInstance(connection(node), {
        clusterAdmin: adminUser,
        restart: true,
        interactive: false
      });
    } else {
      throw error;
    }
  }
}

shell.connect(connection(nodes[0]));
let cluster;
try {
  cluster = dba.getCluster(clusterName);
  print('existing InnoDB Cluster metadata loaded: ' + clusterName);
} catch (error) {
  print('creating InnoDB Cluster: ' + clusterName);
  cluster = dba.createCluster(clusterName, {
    gtidSetIsComplete: true,
    consistency: 'BEFORE_ON_PRIMARY_FAILOVER'
  });
}

function topology() {
  const status = cluster.status({ extended: 1 });
  return status.defaultReplicaSet && status.defaultReplicaSet.topology
    ? status.defaultReplicaSet.topology
    : {};
}

function containsInstance(node) {
  const expected = endpoint(node).toLowerCase();
  return Object.keys(topology()).some(key => String(key).toLowerCase() === expected);
}

for (let index = 1; index < nodes.length; index++) {
  const node = nodes[index];
  if (containsInstance(node)) {
    print('instance is already a cluster member: ' + endpoint(node));
    continue;
  }
  print('adding InnoDB Cluster member with clone recovery: ' + endpoint(node));
  cluster.addInstance(connection(node), { recoveryMethod: 'clone', interactive: false });
}

cluster.setOption('expelTimeout', 5);
cluster.setOption('autoRejoinTries', 3);
const finalStatus = cluster.status({ extended: 1 });
const finalTopology = finalStatus.defaultReplicaSet && finalStatus.defaultReplicaSet.topology
  ? finalStatus.defaultReplicaSet.topology
  : {};
const members = Object.values(finalTopology);
const online = members.filter(member => String(member.status || '').toUpperCase() === 'ONLINE');
if (members.length !== 3 || online.length !== 3) {
  throw new Error('cluster bootstrap did not produce three ONLINE members: ' + JSON.stringify(finalStatus));
}
print(JSON.stringify(finalStatus, null, 2));
JS

info "running MySQL Shell AdminAPI bootstrap"
"$MYSQLSH" --js --file "$JS_FILE"
rm -f "$JS_FILE"
info "MySQL InnoDB Cluster bootstrap completed: $CLUSTER_NAME"
