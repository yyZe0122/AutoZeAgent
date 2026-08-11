#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "uninstall.sh must run as root" >&2
  exit 1
fi

PREFIX=${PREFIX:-/usr/local}
SYSCONFDIR=${SYSCONFDIR:-/etc/yunmengze}
STATE_DIR=${STATE_DIR:-/var/lib/yunmengze}
LOG_DIR=${LOG_DIR:-/var/log/yunmengze}
SERVICE_DIR=${SERVICE_DIR:-/etc/systemd/system}

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now yunmengze.service 2>/dev/null || true
fi
rm -f "$SERVICE_DIR/yunmengze.service"
rm -f "$PREFIX/bin/ymz" "$PREFIX/bin/ymzd"
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
  safe_purge "$SYSCONFDIR" /etc/yunmengze SYSCONFDIR
else
  echo "Configuration preserved at $SYSCONFDIR"
fi
if [ "${PURGE_DATA:-0}" = "1" ]; then
  safe_purge "$STATE_DIR" /var/lib/yunmengze STATE_DIR
  safe_purge "$LOG_DIR" /var/log/yunmengze LOG_DIR
else
  echo "Data preserved at $STATE_DIR"
fi
