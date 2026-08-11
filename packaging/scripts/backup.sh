#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "backup.sh must run as root" >&2
  exit 1
fi

DESTINATION=${1:-/var/backups/yunmengze/yunmengze-$(date -u +%Y%m%dT%H%M%SZ).tar.gz}
was_active=0
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet yunmengze.service; then
  was_active=1
  systemctl stop yunmengze.service
fi
restart() {
  if [ "$was_active" -eq 1 ]; then
    systemctl start yunmengze.service
  fi
}
trap restart EXIT INT TERM
install -d -m 0700 "$(dirname "$DESTINATION")"
tar -C / -czf "$DESTINATION" etc/yunmengze var/lib/yunmengze
chmod 0600 "$DESTINATION"
echo "$DESTINATION"
