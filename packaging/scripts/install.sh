#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

SOURCE_DIR=${1:-.}
PREFIX=${PREFIX:-/usr/local}
SYSCONFDIR=${SYSCONFDIR:-/etc/yunmengze}
STATE_DIR=${STATE_DIR:-/var/lib/yunmengze}
LOG_DIR=${LOG_DIR:-/var/log/yunmengze}
SERVICE_DIR=${SERVICE_DIR:-/etc/systemd/system}

if ! id yunmengze >/dev/null 2>&1; then
  useradd --system --home "$STATE_DIR" --shell /usr/sbin/nologin yunmengze
fi

install -d -m 0755 "$PREFIX/bin"
install -d -o root -g yunmengze -m 0750 "$SYSCONFDIR"
install -d -o yunmengze -g yunmengze -m 0750 "$STATE_DIR" "$LOG_DIR"
install -m 0755 "$SOURCE_DIR/ymz" "$PREFIX/bin/ymz"
install -m 0755 "$SOURCE_DIR/ymzd" "$PREFIX/bin/ymzd"

install -m 0644 "$SOURCE_DIR/packaging/systemd/yunmengze.service" "$SERVICE_DIR/yunmengze.service"
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
fi

# Seed system config + env when missing (do not overwrite).
if [ ! -f "$SYSCONFDIR/agent.json" ] && [ ! -f "$SYSCONFDIR/agent.local.json" ]; then
  EXAMPLE="$SOURCE_DIR/configs/agent.json.example"
  if [ -f "$EXAMPLE" ]; then
    if command -v sed >/dev/null 2>&1; then
      sed '/"\$schema"/d' "$EXAMPLE" >"$SYSCONFDIR/agent.json"
    else
      cp "$EXAMPLE" "$SYSCONFDIR/agent.json"
    fi
  else
    cat >"$SYSCONFDIR/agent.json" <<'EOF'
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
  chown root:yunmengze "$SYSCONFDIR/agent.json"
  chmod 0640 "$SYSCONFDIR/agent.json"
  echo "config: created $SYSCONFDIR/agent.json"
fi

if [ ! -f "$SYSCONFDIR/env" ]; then
  cat >"$SYSCONFDIR/env" <<'EOF'
# Optional KEY=value for system mode (daemon loads; does not override process env).
# Or put a literal apiKey in agent.json / use {file:…}.
DEEPSEEK_API_KEY=
OPENAI_API_KEY=
ANTHROPIC_API_KEY=
GEMINI_API_KEY=
EOF
  chown root:yunmengze "$SYSCONFDIR/env"
  chmod 0640 "$SYSCONFDIR/env"
  echo "env: created $SYSCONFDIR/env"
fi

echo "YunmengZe Agent installed."
echo "  binaries: $PREFIX/bin/{ymz,ymzd}"
echo "  config:   $SYSCONFDIR (edit apiKey via env file, {env:}, {file:}, or literal JSON)"
echo "  service:  systemctl enable --now yunmengze"
