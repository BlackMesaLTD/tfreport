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
#   GITHUB_TOKEN           — optional. When set, sent as a Bearer token on
#                            GitHub requests. Raises the api.github.com rate
#                            limit from 60/hr (unauthenticated, shared per
#                            runner egress IP) to 1000+/hr, and is required if
#                            the release repo is ever made private. curl strips
#                            this header automatically on cross-host redirects
#                            (>=7.58), so it never leaks to the asset CDN.
#
# Resolving "latest": we follow the github.com *web* redirect
# (releases/latest -> releases/tag/vX.Y.Z) rather than calling the REST API.
# The web endpoint is not subject to the 60-requests/hour unauthenticated REST
# limit that intermittently 403s a fan-out matrix and leaves the version empty.
# The authenticated REST API is kept only as a fallback.
#
# Idempotency: when a binary already exists at /usr/local/bin/tfreport AND
# its version satisfies the request (matches an explicit tag, or any version
# satisfies "latest"), the script exits 0 without re-downloading. This makes
# repeated invocations within a job (e.g. prepare + report-plan in the same
# matrix leg) cheap.
#
# Exits non-zero (loudly) on any failure (resolve, curl, checksum, extract).
set -euo pipefail

REPO="BlackMesaLTD/tfreport"

# All diagnostics go through log() with a consistent prefix so a failed install
# is greppable in the action log — the original silent failure mode (empty
# version from a rate-limited curl -s) left nothing to go on.
log() { echo "tfreport-install: $*"; }

if [ "${TFREPORT_SKIP_INSTALL:-}" = "1" ] && command -v tfreport >/dev/null 2>&1; then
  log "TFREPORT_SKIP_INSTALL=1; using pre-installed tfreport: $(command -v tfreport)"
  exit 0
fi

VERSION="${TFREPORT_VERSION:-latest}"
log "requested version: ${VERSION}"

# Normalise non-latest requests so "v0.3.0" and "0.3.0" compare equal.
if [ "$VERSION" != "latest" ]; then
  VERSION="${VERSION#v}"
fi

# Skip when the canonical binary already matches. Composite actions that
# source this script multiple times in one job (prepare + report-plan in the
# same matrix leg) avoid redundant curl + sha256 + tar extract.
if [ -x /usr/local/bin/tfreport ]; then
  installed=$(/usr/local/bin/tfreport --version 2>/dev/null | awk '{print $NF}' || true)
  if [ "$VERSION" = "latest" ] || [ "$VERSION" = "$installed" ]; then
    log "v${installed:-?} already at /usr/local/bin/tfreport (requested: ${VERSION}); skipping install"
    exit 0
  fi
fi

# Shared curl options: fail loudly on HTTP errors (-f), stay quiet on progress
# (-sS still prints errors), follow redirects (-L), and retry transient
# failures (5xx, 429, timeouts, connection-refused) a few times before giving
# up. --retry-connrefused is curl >=7.52 (universal on GitHub runners); we
# avoid --retry-all-errors (>=7.71) since it would also retry non-transient
# 4xx like 404, and the rate-limited REST API is no longer on the hot path.
CURL_OPTS=(--fail --silent --show-error --location \
  --retry 3 --retry-delay 2 --retry-connrefused)

AUTH=()
if [ -n "${GITHUB_TOKEN:-}" ]; then
  AUTH=(--header "Authorization: Bearer ${GITHUB_TOKEN}")
  log "GitHub requests: authenticated (token present)"
else
  log "GitHub requests: unauthenticated (no GITHUB_TOKEN) — subject to lower rate limits"
fi

# Resolve "latest" via the github.com web redirect (NOT the rate-limited REST
# API). The effective URL after following redirects is the tag page, e.g.
# https://github.com/<repo>/releases/tag/v0.3.0 — parse the tag off the end.
resolve_latest_redirect() {
  local url
  url=$(curl "${CURL_OPTS[@]}" "${AUTH[@]}" --output /dev/null \
        --write-out '%{url_effective}' \
        "https://github.com/${REPO}/releases/latest") || return 1
  case "$url" in
    */releases/tag/*) printf '%s' "${url##*/tag/}" ;;
    *) return 1 ;;  # no releases / unexpected redirect target
  esac
}

# Fallback: authenticated REST API. Only reached if the web redirect failed.
resolve_latest_api() {
  curl "${CURL_OPTS[@]}" "${AUTH[@]}" \
    --header 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 | cut -d '"' -f 4
}

if [ "$VERSION" = "latest" ]; then
  # `if VERSION=$(...)` keeps set -e from aborting on a resolver's `return 1`,
  # and lets us log which mechanism actually produced the tag.
  if VERSION=$(resolve_latest_redirect); then
    log "resolved 'latest' -> v${VERSION#v} (github.com web redirect)"
  elif VERSION=$(resolve_latest_api); then
    log "web redirect failed; resolved 'latest' -> v${VERSION#v} (REST API fallback)"
  else
    VERSION=""
  fi
  VERSION="${VERSION#v}"
  if [ -z "$VERSION" ]; then
    echo "::error::install-tfreport: could not resolve the latest tfreport release tag" \
         "(github.com redirect and REST API both failed). Pin TFREPORT_VERSION to a" \
         "specific tag, or set GITHUB_TOKEN to lift the API rate limit." >&2
    exit 1
  fi
fi

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"
ARCHIVE="tfreport_${VERSION}_${OS}_${ARCH}.tar.gz"

log "downloading ${ARCHIVE} from ${BASE_URL}"
curl "${CURL_OPTS[@]}" "${AUTH[@]}" "${BASE_URL}/${ARCHIVE}"    -o "/tmp/${ARCHIVE}"
curl "${CURL_OPTS[@]}" "${AUTH[@]}" "${BASE_URL}/checksums.txt" -o /tmp/checksums.txt

log "verifying sha256 checksum"
cd /tmp && grep "${ARCHIVE}" checksums.txt | sha256sum --check --strict

tar xzf "/tmp/${ARCHIVE}" -C /usr/local/bin tfreport
chmod +x /usr/local/bin/tfreport
rm -f "/tmp/${ARCHIVE}" /tmp/checksums.txt

log "installed tfreport v${VERSION} to /usr/local/bin/tfreport"
