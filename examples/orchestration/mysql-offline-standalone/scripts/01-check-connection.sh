#!/usr/bin/env sh
set -eu

info() {
  echo "[INFO] $1"
}

fail() {
  echo "[ERROR] $1"
  exit 1
}

WORK_DIR="${AIFAR_WORK_DIR:-}"
[ -n "$WORK_DIR" ] || fail "AIFAR_WORK_DIR is empty"
[ -d "$WORK_DIR" ] || fail "remote work directory does not exist: $WORK_DIR"
[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || fail "target operating system is not Linux"

if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || fail "non-root SSH user requires sudo"
  sudo -n true >/dev/null 2>&1 || fail "non-root SSH user requires passwordless sudo"
fi

info "ssh execution channel is available"
info "host=$(hostname 2>/dev/null || echo unknown)"
info "user=$(id -un 2>/dev/null || echo unknown) uid=$(id -u)"
info "kernel=$(uname -r) arch=$(uname -m)"
info "work_dir=$WORK_DIR"
info "server connectivity check passed"
