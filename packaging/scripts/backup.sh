#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "backup.sh must run as root" >&2
  exit 1
fi

DESTINATION=${1:-/var/backups/autozeagent/autozeagent-$(date -u +%Y%m%dT%H%M%SZ).tar.gz}
was_active=0
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet autozeagent.service; then
  was_active=1
  systemctl stop autozeagent.service
fi
restart() {
  if [ "$was_active" -eq 1 ]; then
    systemctl start autozeagent.service
  fi
}
trap restart EXIT INT TERM
install -d -m 0700 "$(dirname "$DESTINATION")"
tar -C / -czf "$DESTINATION" etc/autozeagent var/lib/autozeagent
chmod 0600 "$DESTINATION"
echo "$DESTINATION"
