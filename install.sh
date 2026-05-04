#!/usr/bin/env bash
set -euo pipefail

REPO="sisimomo/aivm"
BINARY="aivm"
INSTALL_DIR="${AIVM_INSTALL_DIR:-${HOME}/.local/bin}"

OS=$(uname -s)
if [[ "$OS" != "Darwin" ]]; then
  echo "Error: aivm requires macOS" >&2
  exit 1
fi

ARCH=$(uname -m)
case "$ARCH" in
  arm64|aarch64) GORELEASER_ARCH="arm64" ;;
  x86_64)        GORELEASER_ARCH="amd64" ;;
  *) echo "Error: Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

echo "→ Fetching latest release..."
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
if [[ -z "$VERSION" ]]; then
  echo "Error: Could not determine latest version" >&2
  exit 1
fi

TARBALL="${BINARY}_darwin_${GORELEASER_ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

echo "→ Installing ${BINARY} ${VERSION} (darwin/${GORELEASER_ARCH})..."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$DOWNLOAD_URL" -o "${TMP}/${TARBALL}"

if curl -fsSL "$CHECKSUM_URL" -o "${TMP}/checksums.txt" 2>/dev/null; then
  (cd "$TMP" && grep "${TARBALL}" checksums.txt | shasum -a 256 -c -) \
    && echo "  ✓ Checksum verified"
fi

tar -xzf "${TMP}/${TARBALL}" -C "$TMP"
mkdir -p "${INSTALL_DIR}"
install -m 755 "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
xattr -dr com.apple.quarantine "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true

echo ""
echo "✓ ${BINARY} ${VERSION} installed → ${INSTALL_DIR}/${BINARY}"
if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  echo "  ⚠ Add ${INSTALL_DIR} to your PATH:"
  echo "    echo 'export PATH=\"\${HOME}/.local/bin:\${PATH}\"' >> ~/.zshrc"
fi
echo "  Run '${BINARY} --help' to get started."
