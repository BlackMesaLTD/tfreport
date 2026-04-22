#!/usr/bin/env bash
# install-tfreport.sh — downloads and verifies the tfreport binary, installs
# to /usr/local/bin. Single-sourced here so every composite action doesn't
# carry its own copy of the install logic.
#
# Inputs (env vars):
#   TFREPORT_VERSION       — version tag (e.g. "v0.0.5") or "latest" (default).
#   TFREPORT_SKIP_INSTALL  — when "1" AND `tfreport` already resolves on PATH,
#                            skip the download entirely and use the pre-
#                            installed binary. ci.yml action-smoke jobs use
#                            this to test the PR-branch-built binary rather
#                            than the last released one. Unset in production.
#
# Exits non-zero on any failure (curl, checksum mismatch, tar extract).
set -euo pipefail

if [ "${TFREPORT_SKIP_INSTALL:-}" = "1" ] && command -v tfreport >/dev/null 2>&1; then
  echo "Using pre-installed tfreport: $(command -v tfreport)"
  exit 0
fi

VERSION="${TFREPORT_VERSION:-latest}"
if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -s https://api.github.com/repos/BlackMesaLTD/tfreport/releases/latest | grep tag_name | cut -d '"' -f 4)
fi
VERSION="${VERSION#v}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

BASE_URL="https://github.com/BlackMesaLTD/tfreport/releases/download/v${VERSION}"
ARCHIVE="tfreport_${VERSION}_${OS}_${ARCH}.tar.gz"

curl -sL "${BASE_URL}/${ARCHIVE}"      -o "/tmp/${ARCHIVE}"
curl -sL "${BASE_URL}/checksums.txt"   -o /tmp/checksums.txt
cd /tmp && grep "${ARCHIVE}" checksums.txt | sha256sum --check --strict

tar xzf "/tmp/${ARCHIVE}" -C /usr/local/bin tfreport
chmod +x /usr/local/bin/tfreport
rm -f "/tmp/${ARCHIVE}" /tmp/checksums.txt

echo "Installed tfreport v${VERSION} to /usr/local/bin/tfreport"
