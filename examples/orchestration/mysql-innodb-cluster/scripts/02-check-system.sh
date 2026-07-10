#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
warn() { echo "[WARN] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

WORK_DIR="${AIFAR_WORK_DIR:?}"
SERVICE_NAME="aifar-mysql-innodb"
MIN_AVAILABLE_MEMORY_MB=768
RECOMMENDED_AVAILABLE_MEMORY_MB=2048

for cmd in awk cp df find grep head install ls od rpm sed sha256sum sort systemctl tr uname; do
  command -v "$cmd" >/dev/null 2>&1 || fail "required command missing: $cmd"
done

[ -r /etc/os-release ] || fail "/etc/os-release is not readable"
. /etc/os-release
OS_ID_NORMALIZED="$(printf '%s' "${ID:-}" | tr '[:upper:]' '[:lower:]')"
case "$OS_ID_NORMALIZED:${VERSION_ID:-}" in
  openeuler:24.03*) ;;
  *) fail "this package requires openEuler 24.03, got ${ID:-unknown} ${VERSION_ID:-unknown}" ;;
esac
[ "$(uname -m)" = "x86_64" ] || fail "this package requires x86_64"
info "platform ok: ${ID:-unknown} ${VERSION_ID:-unknown} $(uname -m)"

MEM_AVAILABLE_KB="$(awk '/MemAvailable:/ {print $2; found=1; exit} /MemTotal:/ {fallback=$2} END {if (!found && fallback) print fallback}' /proc/meminfo 2>/dev/null | head -n 1)"
MEM_AVAILABLE_MB=$(( ${MEM_AVAILABLE_KB:-0} / 1024 ))
info "available memory: ${MEM_AVAILABLE_MB}MB"
if [ "$MEM_AVAILABLE_MB" -lt "$MIN_AVAILABLE_MEMORY_MB" ]; then
  fail "available memory is less than the ${MIN_AVAILABLE_MEMORY_MB}MB minimum"
elif [ "$MEM_AVAILABLE_MB" -lt "$RECOMMENDED_AVAILABLE_MEMORY_MB" ]; then
  warn "available memory is below the recommended ${RECOMMENDED_AVAILABLE_MEMORY_MB}MB; low-memory settings will be used"
fi

DISK_AVAILABLE_MB="$(df -Pm "$WORK_DIR" | awk 'NR==2 {print $4}')"
info "available disk: ${DISK_AVAILABLE_MB:-0}MB"
[ "${DISK_AVAILABLE_MB:-0}" -ge 4096 ] || fail "available disk is less than 4096MB"

[ -f "$WORK_DIR/files/mysql-aifar-8.0.36-official-bundle.tar" ] || fail "MySQL offline bundle is missing"
[ -f "$WORK_DIR/files/RPM-GPG-KEY-openEuler" ] || fail "openEuler RPM signing key is missing"
ls "$WORK_DIR"/rpms/tar-*.rpm >/dev/null 2>&1 || fail "offline tar RPM is missing"
info "verifying package file checksums"
(cd "$WORK_DIR" && sha256sum -c checksums.sha256)

if command -v ss >/dev/null 2>&1; then
  for port in 3306 33060 33061; do
    if ss -lnt | awk '{print $4}' | grep -Eq "(:|\\.)${port}$"; then
      if systemctl is-active --quiet "$SERVICE_NAME"; then
        warn "port $port is already used by managed service $SERVICE_NAME; repair execution will continue"
      else
        fail "port $port is already in use by another service"
      fi
    fi
  done
else
  warn "ss is unavailable; TCP port conflict checks were skipped"
fi

if command -v timedatectl >/dev/null 2>&1; then
  if timedatectl show -p NTPSynchronized --value 2>/dev/null | grep -qi '^yes$'; then
    info "system clock reports NTP synchronization"
  else
    warn "system clock does not report NTP synchronization; clock skew can affect cluster operations"
  fi
fi

if command -v getenforce >/dev/null 2>&1; then
  SELINUX_MODE="$(getenforce 2>/dev/null || echo Disabled)"
  info "SELinux mode: $SELINUX_MODE"
  if [ "$SELINUX_MODE" != "Disabled" ]; then
    command -v chcon >/dev/null 2>&1 || fail "SELinux is enabled but chcon is missing"
    command -v restorecon >/dev/null 2>&1 || fail "SELinux is enabled but restorecon is missing"
  fi
fi

info "system prerequisite check passed"
