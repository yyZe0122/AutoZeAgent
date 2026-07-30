#!/bin/sh
set -eu

REPOSITORY=${AUTOZEAGENT_REPOSITORY:-yyZe0122/AutoZeAgent}
VERSION=${AUTOZEAGENT_VERSION:-latest}
INSTALL_DIR=${AUTOZEAGENT_INSTALL_DIR:-"${HOME}/.local/bin"}

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

ASSET="autozeagent_${OS}_${ARCH}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPOSITORY}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPOSITORY}/releases/download/${VERSION}"
fi

TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t autozeagent)
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

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

echo "AutoZeAgent installed to $INSTALL_DIR."
echo "Interactive TUI: aze  (or autozeagent with no arguments)"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo "Add this directory to PATH before using AutoZeAgent:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
