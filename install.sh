#!/usr/bin/env sh
set -eu

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

VERSION=$(curl -sSL https://api.github.com/repos/ytnobody/hermit/releases/latest \
  | grep '"tag_name"' | cut -d'"' -f4)

if [ -z "$VERSION" ]; then
  echo "Failed to fetch latest release version" >&2
  exit 1
fi

BINARY_URL="https://github.com/ytnobody/hermit/releases/download/${VERSION}/hermit_${OS}_${ARCH}"
SHA256_URL="${BINARY_URL}.sha256"

echo "Downloading HERMIT ${VERSION} for ${OS}/${ARCH}..."

curl -sSL "$BINARY_URL"  -o /tmp/hermit
curl -sSL "$SHA256_URL"  -o /tmp/hermit.sha256

# Verify checksum
cd /tmp
sha256sum -c hermit.sha256
cd - > /dev/null

install -m 755 /tmp/hermit "${HOME}/.local/bin/hermit"
rm -f /tmp/hermit /tmp/hermit.sha256

echo "Installed hermit to ${HOME}/.local/bin/hermit"

hermit install

echo ""
echo "HERMIT installed successfully!"
echo "Next: cd <your-project> && hermit init"
