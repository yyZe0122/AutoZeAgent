#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

SOURCE_DIR=${1:-.}
PREFIX=${PREFIX:-/usr/local}
SYSCONFDIR=${SYSCONFDIR:-/etc/autozeagent}
STATE_DIR=${STATE_DIR:-/var/lib/autozeagent}
LOG_DIR=${LOG_DIR:-/var/log/autozeagent}
SERVICE_DIR=${SERVICE_DIR:-/etc/systemd/system}

if ! id autozeagent >/dev/null 2>&1; then
  useradd --system --home "$STATE_DIR" --shell /usr/sbin/nologin autozeagent
fi

install -d -m 0755 "$PREFIX/bin"
install -d -o root -g autozeagent -m 0750 "$SYSCONFDIR"
install -d -o autozeagent -g autozeagent -m 0750 "$STATE_DIR" "$LOG_DIR"
install -m 0755 "$SOURCE_DIR/autozeagent" "$PREFIX/bin/autozeagent"
install -m 0755 "$SOURCE_DIR/autozeagentd" "$PREFIX/bin/autozeagentd"


install -m 0644 "$SOURCE_DIR/packaging/systemd/autozeagent.service" "$SERVICE_DIR/autozeagent.service"
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
fi

echo "AutoZeAgent installed. Run: systemctl enable --now autozeagent"
