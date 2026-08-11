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
if [ -f "$SOURCE_DIR/aze" ]; then
  install -m 0755 "$SOURCE_DIR/aze" "$PREFIX/bin/aze"
else
  ln -sfn autozeagent "$PREFIX/bin/aze"
fi

install -m 0644 "$SOURCE_DIR/packaging/systemd/autozeagent.service" "$SERVICE_DIR/autozeagent.service"
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
fi

# Seed system config + env when missing (do not overwrite).
if [ ! -f "$SYSCONFDIR/autozeagent.json" ] && [ ! -f "$SYSCONFDIR/autozeagent.local.json" ]; then
  EXAMPLE="$SOURCE_DIR/configs/autozeagent.json.example"
  if [ -f "$EXAMPLE" ]; then
    if command -v sed >/dev/null 2>&1; then
      sed '/"\$schema"/d' "$EXAMPLE" >"$SYSCONFDIR/autozeagent.json"
    else
      cp "$EXAMPLE" "$SYSCONFDIR/autozeagent.json"
    fi
  else
    cat >"$SYSCONFDIR/autozeagent.json" <<'EOF'
{
  "model": "deepseek/deepseek-chat",
  "provider": {
    "deepseek": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com",
        "apiKey": "{env:DEEPSEEK_API_KEY}"
      },
      "models": {
        "deepseek-chat": { "name": "DeepSeek Chat" }
      }
    }
  }
}
EOF
  fi
  chown root:autozeagent "$SYSCONFDIR/autozeagent.json"
  chmod 0640 "$SYSCONFDIR/autozeagent.json"
  echo "config: created $SYSCONFDIR/autozeagent.json"
fi

if [ ! -f "$SYSCONFDIR/env" ]; then
  cat >"$SYSCONFDIR/env" <<'EOF'
# Optional KEY=value for system mode (daemon loads; does not override process env).
# Or put a literal apiKey in autozeagent.json / use {file:…}.
DEEPSEEK_API_KEY=
OPENAI_API_KEY=
ANTHROPIC_API_KEY=
GEMINI_API_KEY=
EOF
  chown root:autozeagent "$SYSCONFDIR/env"
  chmod 0640 "$SYSCONFDIR/env"
  echo "env: created $SYSCONFDIR/env"
fi

echo "AutoZeAgent installed."
echo "  binaries: $PREFIX/bin/{autozeagent,aze,autozeagentd}"
echo "  config:   $SYSCONFDIR (edit apiKey via env file, {env:}, {file:}, or literal JSON)"
echo "  service:  systemctl enable --now autozeagent"
