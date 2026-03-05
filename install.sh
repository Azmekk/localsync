#!/bin/sh
set -e

REPO="Azmekk/localsync"

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *)      echo "Error: unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       echo "Error: unsupported architecture: $ARCH"; exit 1 ;;
esac

# Only supported combinations
case "${OS}-${ARCH}" in
    linux-amd64|darwin-amd64|darwin-arm64) ;;
    *) echo "Error: no prebuilt binary for ${OS}-${ARCH}"; exit 1 ;;
esac

echo "Detected ${OS}-${ARCH}"

# Get latest release download URLs
RELEASE_URL="https://api.github.com/repos/${REPO}/releases/latest"
echo "Fetching latest release..."
RELEASE_JSON="$(curl -fsSL "$RELEASE_URL")"

LOCALSYNC_URL="$(echo "$RELEASE_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*localsync-${OS}-${ARCH}\"" | head -1 | cut -d'"' -f4)"
SYNCCLIENT_URL="$(echo "$RELEASE_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*syncclient-${OS}-${ARCH}\"" | head -1 | cut -d'"' -f4)"

if [ -z "$LOCALSYNC_URL" ] || [ -z "$SYNCCLIENT_URL" ]; then
    echo "Error: could not find release assets for ${OS}-${ARCH}"
    exit 1
fi

# Determine install directory
INSTALL_DIR="/usr/local/bin"
NEED_SUDO=""

if [ -w "$INSTALL_DIR" ]; then
    NEED_SUDO=""
elif command -v sudo >/dev/null 2>&1; then
    NEED_SUDO="sudo"
    echo "Will install to ${INSTALL_DIR} (using sudo)"
else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
    echo "Will install to ${INSTALL_DIR}"
fi

if [ -z "$NEED_SUDO" ] && [ "$INSTALL_DIR" = "/usr/local/bin" ]; then
    echo "Will install to ${INSTALL_DIR}"
fi

# Download and install
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading localsync..."
curl -fsSL -o "${TMPDIR}/localsync" "$LOCALSYNC_URL"

echo "Downloading syncclient..."
curl -fsSL -o "${TMPDIR}/syncclient" "$SYNCCLIENT_URL"

chmod +x "${TMPDIR}/localsync" "${TMPDIR}/syncclient"

$NEED_SUDO mv "${TMPDIR}/localsync" "${INSTALL_DIR}/localsync"
$NEED_SUDO mv "${TMPDIR}/syncclient" "${INSTALL_DIR}/syncclient"

echo ""
echo "Installed localsync and syncclient to ${INSTALL_DIR}"

# Check for optional dependencies
if ! command -v mpv >/dev/null 2>&1; then
    echo "Warning: mpv not found — install it before running localsync"
fi

if ! command -v ffmpeg >/dev/null 2>&1; then
    echo "Warning: ffmpeg not found — install it before running localsync"
fi

echo "Done!"
