#!/usr/bin/env sh
set -eu

info() { echo "[INFO] $1"; }
warn() { echo "[WARN] $1"; }
fail() { echo "[ERROR] $1"; exit 1; }

PORTS="3306 33060 33061"
if [ "${AIFAR_NODE_ROLE:-}" = "coordinator" ]; then
  PORTS="$PORTS 6446 6447 6448 6449"
fi
SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo -n"; fi

if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
  changed=0
  for port in $PORTS; do
    if $SUDO firewall-cmd --quiet --permanent --query-port="${port}/tcp"; then
      info "firewalld already allows ${port}/tcp"
    else
      $SUDO firewall-cmd --permanent --add-port="${port}/tcp" >/dev/null || fail "failed to add firewalld rule ${port}/tcp"
      changed=1
    fi
  done
  if [ "$changed" -eq 1 ]; then
    $SUDO firewall-cmd --reload >/dev/null || fail "failed to reload firewalld"
  fi
  for port in $PORTS; do
    $SUDO firewall-cmd --quiet --query-port="${port}/tcp" || fail "firewalld rule ${port}/tcp is not active"
  done
elif command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q '^Status: active'; then
  for port in $PORTS; do
    $SUDO ufw allow "${port}/tcp" >/dev/null || fail "failed to add ufw rule ${port}/tcp"
  done
elif systemctl is-active --quiet nftables 2>/dev/null; then
  fail "nftables is active without a supported firewall manager; add persistent rules for ports: $PORTS"
else
  warn "no active supported firewall manager detected; no firewall changes were required"
fi

info "firewall ports ready: $PORTS"
