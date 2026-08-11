#!/bin/sh
# User install: binaries → ~/.local/bin (or AUTOZEAGENT_INSTALL_DIR),
# PATH in shell rc, ConfigDir template + optional env file.
# apiKey may use {env:}, {file:}, or a literal string in JSON — none is forced.
set -eu

REPOSITORY=${AUTOZEAGENT_REPOSITORY:-yyZe0122/AutoZeAgent}
VERSION=${AUTOZEAGENT_VERSION:-latest}
INSTALL_DIR=${AUTOZEAGENT_INSTALL_DIR:-"${HOME}/.local/bin"}
if [ -n "${XDG_CONFIG_HOME:-}" ]; then
  CONFIG_DIR=${AUTOZEAGENT_CONFIG_DIR:-"${XDG_CONFIG_HOME}/autozeagent"}
else
  CONFIG_DIR=${AUTOZEAGENT_CONFIG_DIR:-"${HOME}/.config/autozeagent"}
fi

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *)
    echo "Unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *)
    echo "Unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

fetch() {
  url=$1
  output=$2
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --silent --show-error "$url" --output "$output"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$output"
  else
    echo "curl or wget is required." >&2
    exit 1
  fi
}

resolve_tag() {
  if [ "$VERSION" != "latest" ]; then
    echo "$VERSION"
    return
  fi
  fetch_body() {
    url=$1
    if command -v curl >/dev/null 2>&1; then
      curl --fail --location --silent --show-error "$url" 2>/dev/null || true
    elif command -v wget >/dev/null 2>&1; then
      wget -q -O - "$url" 2>/dev/null || true
    fi
  }
  body=$(fetch_body "https://api.github.com/repos/${REPOSITORY}/releases/latest")
  tag=$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  if [ -z "$tag" ]; then
    body=$(fetch_body "https://api.github.com/repos/${REPOSITORY}/releases?per_page=20")
    tag=$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  fi
  if [ -z "$tag" ]; then
    echo "Could not resolve latest release tag for ${REPOSITORY}." >&2
    echo "Set AUTOZEAGENT_VERSION=vX.Y.Z (required for Pre-release-only repos)." >&2
    exit 1
  fi
  echo "$tag"
}

append_path_rc() {
  install_dir=$1
  marker="# AutoZeAgent PATH"
  env_marker="# AutoZeAgent env file"
  shell_name=$(basename "${SHELL:-}")
  case "$shell_name" in
    zsh) rc="${HOME}/.zshrc" ;;
    bash) rc="${HOME}/.bashrc" ;;
    *) rc="${HOME}/.profile" ;;
  esac
  mkdir -p "$(dirname "$rc")"
  if [ ! -f "$rc" ]; then
    touch "$rc"
  fi
  if ! grep -qF "$marker" "$rc" 2>/dev/null; then
    {
      echo ""
      echo "$marker"
      echo "export PATH=\"${install_dir}:\$PATH\""
    } >>"$rc"
    echo "  PATH: appended to $rc"
  fi
  if ! grep -qF "$env_marker" "$rc" 2>/dev/null; then
    {
      echo "$env_marker"
      echo "if [ -f \"${CONFIG_DIR}/env\" ]; then set -a; . \"${CONFIG_DIR}/env\"; set +a; fi"
    } >>"$rc"
    echo "  env: source block appended to $rc (optional file ${CONFIG_DIR}/env)"
  fi
}

seed_config() {
  example_src=$1
  mkdir -p "$CONFIG_DIR"
  chmod 0750 "$CONFIG_DIR" 2>/dev/null || true
  if [ ! -f "${CONFIG_DIR}/autozeagent.json" ] && [ ! -f "${CONFIG_DIR}/autozeagent.local.json" ]; then
    if [ -f "$example_src" ]; then
      # Drop $schema relative path that is wrong outside the repo.
      if command -v sed >/dev/null 2>&1; then
        sed '/"\$schema"/d' "$example_src" >"${CONFIG_DIR}/autozeagent.json"
      else
        cp "$example_src" "${CONFIG_DIR}/autozeagent.json"
      fi
    else
      cat >"${CONFIG_DIR}/autozeagent.json" <<'EOF'
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
        "deepseek-chat": {
          "name": "DeepSeek Chat",
          "maxTokens": 4096,
          "contextWindow": 65536
        }
      }
    }
  },
  "chat": {
    "workspace": { "default": "client_cwd", "allow": [], "allow_all": false },
    "allow_write": true,
    "permission": { "mode": "preauth" }
  }
}
EOF
    fi
    chmod 0600 "${CONFIG_DIR}/autozeagent.json"
    echo "  config: created ${CONFIG_DIR}/autozeagent.json"
  else
    echo "  config: kept existing file under ${CONFIG_DIR}"
  fi
  if [ ! -f "${CONFIG_DIR}/env" ]; then
    cat >"${CONFIG_DIR}/env" <<'EOF'
# Optional KEY=value (loaded by daemon/CLI; does not override existing process env).
# Pair with apiKey "{env:DEEPSEEK_API_KEY}" in autozeagent.json, or put a literal apiKey in JSON, or use {file:…}.
# chmod 600 recommended. Do not commit secrets.
#
DEEPSEEK_API_KEY=
OPENAI_API_KEY=
ANTHROPIC_API_KEY=
GEMINI_API_KEY=
EOF
    chmod 0600 "${CONFIG_DIR}/env"
    echo "  env: created ${CONFIG_DIR}/env (fill keys as needed)"
  else
    echo "  env: kept existing ${CONFIG_DIR}/env"
  fi
}

TAG=$(resolve_tag)
case "$TAG" in
  v*) VER_NUM=${TAG#v} ;;
  *) VER_NUM=$TAG; TAG="v$TAG" ;;
esac

ASSET="autozeagent_${VER_NUM}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPOSITORY}/releases/download/${TAG}"

echo "Installing AutoZeAgent ${TAG} (${OS}/${ARCH}) ..."
echo "  binaries → ${INSTALL_DIR}"
echo "  config   → ${CONFIG_DIR}"

TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t autozeagent)
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

fetch "$BASE_URL/$ASSET" "$TMP_DIR/$ASSET"
fetch "$BASE_URL/checksums.txt" "$TMP_DIR/checksums.txt"

EXPECTED=$(awk -v file="$ASSET" '$2 == file || $2 == "*" file { print $1; exit }' "$TMP_DIR/checksums.txt")
if [ -z "$EXPECTED" ]; then
  echo "No checksum entry found for $ASSET." >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMP_DIR/$ASSET" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "$TMP_DIR/$ASSET" | awk '{print $1}')
else
  echo "sha256sum or shasum is required to verify the download." >&2
  exit 1
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "Checksum verification failed for $ASSET." >&2
  exit 1
fi

tar -xzf "$TMP_DIR/$ASSET" -C "$TMP_DIR"
mkdir -p "$INSTALL_DIR"
cp "$TMP_DIR/autozeagent" "$INSTALL_DIR/autozeagent"
cp "$TMP_DIR/autozeagentd" "$INSTALL_DIR/autozeagentd"
if [ -f "$TMP_DIR/aze" ]; then
  cp "$TMP_DIR/aze" "$INSTALL_DIR/aze"
else
  ln -sfn autozeagent "$INSTALL_DIR/aze"
fi
chmod 0755 "$INSTALL_DIR/autozeagent" "$INSTALL_DIR/autozeagentd" "$INSTALL_DIR/aze"

EXAMPLE=""
if [ -f "$TMP_DIR/configs/autozeagent.json.example" ]; then
  EXAMPLE="$TMP_DIR/configs/autozeagent.json.example"
fi
seed_config "$EXAMPLE"
append_path_rc "$INSTALL_DIR"

# Current shell session
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) export PATH="${INSTALL_DIR}:$PATH" ;;
esac
if [ -f "${CONFIG_DIR}/env" ]; then
  set -a
  # shellcheck disable=SC1090
  . "${CONFIG_DIR}/env"
  set +a
fi

echo ""
echo "Installed AutoZeAgent ${TAG}"
echo "  ${INSTALL_DIR}/autozeagent"
echo "  ${INSTALL_DIR}/aze"
echo "  ${INSTALL_DIR}/autozeagentd"
echo ""
echo "API key (pick one; nothing is forced):"
echo "  1) Edit ${CONFIG_DIR}/env  (e.g. DEEPSEEK_API_KEY=...) — loaded automatically"
echo "  2) Export in the shell:  export DEEPSEEK_API_KEY=..."
echo "  3) Put a literal apiKey string in ${CONFIG_DIR}/autozeagent.json"
echo "  4) Use {file:relative-or-abs-path} in apiKey"
echo ""
echo "Open a new terminal (or source your shell rc), then:"
echo "  aze"
echo "  autozeagent version"
