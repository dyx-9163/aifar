#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
warn() { echo "[WARN] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

WORK_DIR="${AIFAR_WORK_DIR:?}"
[ -d "$WORK_DIR" ] || fail "remote work directory does not exist: $WORK_DIR"
[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || fail "target operating system is not Linux"
[ "${AIFAR_CLUSTER_NODE_COUNT:-0}" -eq 3 ] || fail "this package requires exactly three cluster nodes"
[ -n "${AIFAR_CLUSTER_ID:-}" ] || fail "cluster id is missing"
[ -n "${AIFAR_CLUSTER_NAME:-}" ] || fail "cluster name is missing"
[ -n "${AIFAR_NODE_SERVER_ID:-}" ] || fail "node server id is missing"
[ -n "${AIFAR_NODE_HOST:-}" ] || fail "node host is missing"
case "${AIFAR_NODE_ROLE:-}" in
  coordinator|member) ;;
  *) fail "invalid node role: ${AIFAR_NODE_ROLE:-empty}" ;;
esac

if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || fail "non-root SSH user requires sudo"
  sudo -n true >/dev/null 2>&1 || fail "non-root SSH user requires passwordless sudo"
fi

i=1
while [ "$i" -le "$AIFAR_CLUSTER_NODE_COUNT" ]; do
  eval "peer_host=\${AIFAR_CLUSTER_NODE_${i}_HOST:-}"
  [ -n "$peer_host" ] || fail "cluster node $i host is missing"
  case "$peer_host" in
    *[!A-Za-z0-9._:-]*) fail "cluster node $i host contains unsupported characters: $peer_host" ;;
  esac
  if command -v getent >/dev/null 2>&1 && ! getent hosts "$peer_host" >/dev/null 2>&1; then
    case "$peer_host" in
      *:*) warn "host lookup did not resolve IPv6 literal $peer_host; connectivity is checked after MySQL starts" ;;
      [0-9]*.[0-9]*.[0-9]*.[0-9]*) warn "host lookup did not return IP literal $peer_host; connectivity is checked after MySQL starts" ;;
      *) fail "cluster peer host cannot be resolved: $peer_host" ;;
    esac
  fi
  i=$((i + 1))
done

info "ssh execution channel is available"
info "host=$(hostname 2>/dev/null || echo unknown)"
info "user=$(id -un 2>/dev/null || echo unknown) uid=$(id -u)"
info "kernel=$(uname -r) arch=$(uname -m)"
info "cluster=$AIFAR_CLUSTER_NAME node=$AIFAR_NODE_SERVER_ID role=$AIFAR_NODE_ROLE index=$AIFAR_NODE_INDEX"
info "server connectivity and cluster context check passed"
