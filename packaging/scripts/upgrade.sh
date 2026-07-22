#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "upgrade.sh must run as root" >&2
  exit 1
fi
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: upgrade.sh RELEASE_DIR [BACKUP.tar.gz]" >&2
  exit 1
fi
RELEASE_DIR=$1
BACKUP=${2:-/var/backups/autozeagent/pre-upgrade-$(date -u +%Y%m%dT%H%M%SZ).tar.gz}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
"$SCRIPT_DIR/backup.sh" "$BACKUP"
systemctl stop autozeagent.service 2>/dev/null || true
"$SCRIPT_DIR/install.sh" "$RELEASE_DIR"
/usr/local/bin/autozeagent config validate --mode system
systemctl restart autozeagent.service
/usr/local/bin/autozeagent health --mode system
echo "Upgrade complete. Backup: $BACKUP"
