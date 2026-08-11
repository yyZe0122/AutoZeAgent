#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "uninstall.sh must run as root" >&2
  exit 1
fi

PREFIX=${PREFIX:-/usr/local}
SYSCONFDIR=${SYSCONFDIR:-/etc/autozeagent}
STATE_DIR=${STATE_DIR:-/var/lib/autozeagent}
LOG_DIR=${LOG_DIR:-/var/log/autozeagent}
MODULE_DIR=${MODULE_DIR:-$PREFIX/lib/autozeagent/modules}
SERVICE_DIR=${SERVICE_DIR:-/etc/systemd/system}

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now autozeagent.service 2>/dev/null || true
fi
rm -f "$SERVICE_DIR/autozeagent.service"
rm -f "$PREFIX/bin/autozeagent" "$PREFIX/bin/autozeagentd" "$PREFIX/bin/aze"
# Remove retired module binaries so upgrades clean legacy installations.
rm -f "$MODULE_DIR/memory/autozeagent-memory" "$MODULE_DIR/autozeagent-memory" "$MODULE_DIR/autozeagent-skills" "$MODULE_DIR/autozeagent-scheduler" "$MODULE_DIR/autozeagent-evolution"
rmdir "$MODULE_DIR/memory" 2>/dev/null || true
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
fi

safe_purge() {
  target=$1
  allowed_root=$2
  label=$3
  if [ ! -e "$target" ]; then
    return
  fi
  resolved=$(CDPATH= cd -- "$target" 2>/dev/null && pwd -P) || {
    echo "refusing to purge unresolved $label=$target" >&2
    exit 1
  }
  case "$resolved" in
    "$allowed_root"|"$allowed_root"/*) rm -rf -- "$resolved" ;;
    *) echo "refusing to purge unexpected $label=$target (resolved to $resolved)" >&2; exit 1 ;;
  esac
}

if [ "${PURGE_CONFIG:-0}" = "1" ]; then
  safe_purge "$SYSCONFDIR" /etc/autozeagent SYSCONFDIR
else
  echo "Configuration preserved at $SYSCONFDIR"
fi
if [ "${PURGE_DATA:-0}" = "1" ]; then
  safe_purge "$STATE_DIR" /var/lib/autozeagent STATE_DIR
  safe_purge "$LOG_DIR" /var/log/autozeagent LOG_DIR
else
  echo "Data preserved at $STATE_DIR"
fi
