#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "restore.sh must run as root" >&2
  exit 1
fi
if [ "$#" -ne 1 ]; then
  echo "usage: restore.sh BACKUP.tar.gz" >&2
  exit 1
fi
ARCHIVE=$1
if [ ! -f "$ARCHIVE" ]; then
  echo "backup archive not found: $ARCHIVE" >&2
  exit 1
fi
if tar -tzf "$ARCHIVE" | grep -E '(^/|(^|/)\.\.(/|$))' >/dev/null; then
  echo "unsafe path in backup archive" >&2
  exit 1
fi
if tar -tzf "$ARCHIVE" | grep -Ev '^(etc/yunmengze|var/lib/yunmengze)(/|$)' >/dev/null; then
  echo "backup contains paths outside YunmengZe configuration and state" >&2
  exit 1
fi

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
tar -C / -xzf "$ARCHIVE"
chown -R yunmengze:yunmengze /var/lib/yunmengze
find /etc/yunmengze -type d -exec chmod 0750 {} \;
find /etc/yunmengze -type f -exec chmod 0640 {} \;
/usr/local/bin/ymz config validate --mode system
/usr/local/bin/ymz db check --mode system
