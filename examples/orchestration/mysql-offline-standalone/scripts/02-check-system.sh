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
PORT=3306
SERVICE_NAME="aifar-orchestration-mysql"
MIN_AVAILABLE_MEMORY_MB=768
RECOMMENDED_AVAILABLE_MEMORY_MB=2048

for cmd in awk cp df find grep head install ls od rpm sed sha256sum sort systemctl tr uname; do
  command -v "$cmd" >/dev/null 2>&1 || fail "required command missing: $cmd"
  info "command ok: $cmd"
done

[ -r /etc/os-release ] || fail "/etc/os-release is not readable"
OS_ID=""
OS_VERSION=""
. /etc/os-release
OS_ID="${ID:-}"
OS_VERSION="${VERSION_ID:-}"
OS_ID_NORMALIZED="$(printf '%s' "$OS_ID" | tr '[:upper:]' '[:lower:]')"
case "$OS_ID_NORMALIZED:$OS_VERSION" in
  openeuler:24.03*) ;;
  *) fail "this package requires openEuler 24.03, got ${OS_ID:-unknown} ${OS_VERSION:-unknown}" ;;
esac

ARCH="$(uname -m)"
[ "$ARCH" = "x86_64" ] || fail "this package requires x86_64, got $ARCH"
info "platform ok: $OS_ID $OS_VERSION $ARCH"

MEM_AVAILABLE_KB="$(awk '/MemAvailable:/ {print $2; found=1; exit} /MemTotal:/ {fallback=$2} END {if (!found && fallback) print fallback}' /proc/meminfo 2>/dev/null | head -n 1)"
MEM_AVAILABLE_MB=$(( ${MEM_AVAILABLE_KB:-0} / 1024 ))
info "available memory: ${MEM_AVAILABLE_MB}MB"
if [ "$MEM_AVAILABLE_MB" -lt "$MIN_AVAILABLE_MEMORY_MB" ]; then
  fail "available memory is less than the ${MIN_AVAILABLE_MEMORY_MB}MB minimum"
elif [ "$MEM_AVAILABLE_MB" -lt "$RECOMMENDED_AVAILABLE_MEMORY_MB" ]; then
  warn "available memory is below the recommended ${RECOMMENDED_AVAILABLE_MEMORY_MB}MB; the low-memory MySQL profile will be used"
else
  info "available memory meets the recommended ${RECOMMENDED_AVAILABLE_MEMORY_MB}MB"
fi

DISK_AVAILABLE_MB="$(df -Pm "$WORK_DIR" | awk 'NR==2 {print $4}')"
info "available disk: ${DISK_AVAILABLE_MB:-0}MB"
[ "${DISK_AVAILABLE_MB:-0}" -ge 2048 ] || fail "available disk is less than 2048MB"

[ -f "$WORK_DIR/files/mysql-aifar-8.0.36-official-bundle.tar" ] || fail "MySQL offline bundle is missing"
[ -f "$WORK_DIR/files/RPM-GPG-KEY-openEuler" ] || fail "openEuler RPM signing key is missing"
ls "$WORK_DIR"/rpms/tar-*.rpm >/dev/null 2>&1 || fail "offline tar RPM is missing"

if command -v getenforce >/dev/null 2>&1; then
  SELINUX_MODE="$(getenforce 2>/dev/null || echo Disabled)"
  info "SELinux mode: $SELINUX_MODE"
  if [ "$SELINUX_MODE" != "Disabled" ]; then
    command -v chcon >/dev/null 2>&1 || fail "SELinux is enabled but chcon is missing"
    command -v restorecon >/dev/null 2>&1 || fail "SELinux is enabled but restorecon is missing"
    if command -v semanage >/dev/null 2>&1; then
      info "SELinux persistent policy tool available: semanage"
    else
      warn "semanage is unavailable; current files will use chcon fallback labels"
    fi
  fi
else
  info "SELinux tools are unavailable; SELinux is treated as disabled"
fi

info "verifying package file checksums"
(cd "$WORK_DIR" && sha256sum -c checksums.sha256)

if command -v ss >/dev/null 2>&1; then
  if ss -lnt | awk '{print $4}' | grep -Eq "(:|\\.)${PORT}$"; then
    if systemctl is-active --quiet "$SERVICE_NAME"; then
      warn "port $PORT is already used by $SERVICE_NAME; idempotent reinstall will continue"
    else
      fail "port $PORT is already in use by another service"
    fi
  else
    info "port $PORT is available"
  fi
else
  warn "ss is unavailable; TCP port conflict check was skipped"
fi

info "system prerequisite check passed"
