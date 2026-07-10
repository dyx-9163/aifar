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

PORT=3306
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  SUDO="sudo -n"
fi

if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
  if $SUDO firewall-cmd --quiet --permanent --query-port="${PORT}/tcp"; then
    info "firewalld already allows ${PORT}/tcp"
  else
    info "adding ${PORT}/tcp to firewalld"
    $SUDO firewall-cmd --permanent --add-port="${PORT}/tcp" >/dev/null || fail "failed to add firewalld rule"
  fi
  $SUDO firewall-cmd --reload >/dev/null || fail "failed to reload firewalld"
  $SUDO firewall-cmd --quiet --query-port="${PORT}/tcp" || fail "firewalld rule is not active"
  info "firewalld allows ${PORT}/tcp"
elif command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q '^Status: active'; then
  info "allowing ${PORT}/tcp through ufw"
  $SUDO ufw allow "${PORT}/tcp" >/dev/null || fail "failed to add ufw rule"
  info "ufw allows ${PORT}/tcp"
elif systemctl is-active --quiet nftables 2>/dev/null; then
  fail "nftables is active without a supported firewall manager; add a persistent ${PORT}/tcp rule explicitly"
else
  warn "no active supported firewall manager detected; no firewall rule was required"
fi

info "firewall step completed"
