#!/usr/bin/env bash
#
# Generate or update a builtin provider preset from Terraform provider docs.
#
# Usage:
#   ./scripts/generate-preset.sh azurerm
#   ./scripts/generate-preset.sh aws
#   ./scripts/generate-preset.sh google
#   ./scripts/generate-preset.sh azurerm --schema-file /tmp/azurerm-schema.json
#
# The script clones the provider repo, runs presetgen, merges with any
# existing builtin preset, writes the result, and cleans up.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILTIN_DIR="${REPO_ROOT}/internal/presets/builtin"
PRESETGEN="${REPO_ROOT}/presetgen"
TMPDIR=""

cleanup() {
    if [[ -n "${TMPDIR}" && -d "${TMPDIR}" ]]; then
        echo "Cleaning up ${TMPDIR}..." >&2
        rm -rf "${TMPDIR}"
    fi
}
trap cleanup EXIT

# --- Provider registry ---
declare -A PROVIDER_REPOS=(
    [azurerm]="https://github.com/hashicorp/terraform-provider-azurerm.git"
    [aws]="https://github.com/hashicorp/terraform-provider-aws.git"
    [google]="https://github.com/hashicorp/terraform-provider-google.git"
)

declare -A PROVIDER_DOCS_PATHS=(
    [azurerm]="website/docs/r"
    [aws]="website/docs/r"
    [google]="website/docs/r"
)

# --- Args ---
PROVIDER="${1:-}"
shift || true
EXTRA_ARGS=("$@")

if [[ -z "${PROVIDER}" ]]; then
    echo "Usage: $0 <provider> [--schema-file path]" >&2
    echo "Providers: ${!PROVIDER_REPOS[*]}" >&2
    exit 1
fi

REPO_URL="${PROVIDER_REPOS[${PROVIDER}]:-}"
if [[ -z "${REPO_URL}" ]]; then
    echo "Unknown provider: ${PROVIDER}" >&2
    echo "Known providers: ${!PROVIDER_REPOS[*]}" >&2
    exit 1
fi

DOCS_SUBPATH="${PROVIDER_DOCS_PATHS[${PROVIDER}]}"
OUTPUT="${BUILTIN_DIR}/${PROVIDER}.json"

# --- Build presetgen if needed ---
if [[ ! -x "${PRESETGEN}" ]]; then
    echo "Building presetgen..." >&2
    (cd "${REPO_ROOT}" && go build -o presetgen ./cmd/presetgen)
fi

# --- Clone provider docs (shallow, sparse) ---
TMPDIR="$(mktemp -d)"
echo "Cloning ${PROVIDER} provider docs into ${TMPDIR}..." >&2

git clone --depth 1 --filter=blob:none --sparse "${REPO_URL}" "${TMPDIR}/provider" 2>&1 | tail -1
(cd "${TMPDIR}/provider" && git sparse-checkout set "${DOCS_SUBPATH}" 2>/dev/null)

DOCS_DIR="${TMPDIR}/provider/${DOCS_SUBPATH}"

if [[ ! -d "${DOCS_DIR}" ]]; then
    echo "Error: docs directory not found at ${DOCS_DIR}" >&2
    echo "The provider repo structure may have changed." >&2
    exit 1
fi

DOC_COUNT=$(find "${DOCS_DIR}" -name '*.markdown' -o -name '*.md' | wc -l)
echo "Found ${DOC_COUNT} doc files" >&2

# --- Build presetgen args ---
ARGS=(
    --provider "${PROVIDER}"
    --docs-dir "${DOCS_DIR}"
    --output "${OUTPUT}"
)

# Merge with existing builtin if present
if [[ -f "${OUTPUT}" ]]; then
    echo "Merging with existing preset at ${OUTPUT}" >&2
    ARGS+=(--existing-preset "${OUTPUT}")
fi

ARGS+=("${EXTRA_ARGS[@]}")

# --- Run ---
echo "Running presetgen..." >&2
"${PRESETGEN}" "${ARGS[@]}"

# --- Summary ---
RESOURCE_COUNT=$(grep -c '"display_name"' "${OUTPUT}" 2>/dev/null || echo "?")
FILE_SIZE=$(wc -c < "${OUTPUT}" | tr -d ' ')
echo "" >&2
echo "Done: ${OUTPUT}" >&2
echo "  Resources: ~${RESOURCE_COUNT}" >&2
echo "  Size: ${FILE_SIZE} bytes" >&2
