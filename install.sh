#!/bin/sh
set -e

BASE_URL="https://github.com/acopy-org/client/releases/latest/download"
INSTALL_DIR="/usr/local/bin"
BIN="acopy"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "unsupported architecture: $ARCH"; exit 1 ;;
esac

URL="${BASE_URL}/dl/${OS}-${ARCH}/${BIN}"

echo "downloading acopy for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "/tmp/${BIN}"
chmod +x "/tmp/${BIN}"

if [ "$OS" = "darwin" ]; then
    xattr -d com.apple.quarantine "/tmp/${BIN}" 2>/dev/null || true
fi

echo "installing to ${INSTALL_DIR}/${BIN} (may require sudo)..."
if [ -w "$INSTALL_DIR" ]; then
    mv "/tmp/${BIN}" "${INSTALL_DIR}/${BIN}"
else
    sudo mv "/tmp/${BIN}" "${INSTALL_DIR}/${BIN}"
fi

echo "installed. run: acopy setup"
