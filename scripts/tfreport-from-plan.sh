#!/usr/bin/env bash
# tfreport-from-plan — derive plan JSON and text from a terraform binary plan
# file, then invoke tfreport with both so step-summary and pr-comment targets
# render their native per-resource text blocks.
#
# Usage:
#   tfreport-from-plan <plan.out> [tfreport flags...]
#
# Examples:
#   tfreport-from-plan plan.out --target github-step-summary
#   tfreport-from-plan plan.out --target github-pr-body --config .tfreport.yml
#
# Environment:
#   TFREPORT_TERRAFORM_BIN — path to the terraform binary (default: terraform from PATH)
#   TFREPORT_BIN           — path to the tfreport binary   (default: tfreport from PATH)

set -euo pipefail

TF="${TFREPORT_TERRAFORM_BIN:-terraform}"
TR="${TFREPORT_BIN:-tfreport}"

if [ "${#:-0}" -lt 1 ]; then
    echo "usage: $(basename "$0") <plan.out> [tfreport flags...]" >&2
    exit 2
fi

PLAN="$1"
shift

if ! command -v "$TF" >/dev/null 2>&1; then
    echo "error: terraform binary '$TF' not found (set TFREPORT_TERRAFORM_BIN to override)" >&2
    exit 1
fi
if ! command -v "$TR" >/dev/null 2>&1; then
    echo "error: tfreport binary '$TR' not found (set TFREPORT_BIN to override)" >&2
    exit 1
fi
if [ ! -f "$PLAN" ]; then
    echo "error: plan file not found: $PLAN" >&2
    exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

"$TF" show -json     "$PLAN" > "$TMP/plan.json"
"$TF" show -no-color "$PLAN" > "$TMP/plan.txt"

"$TR" --plan-file "$TMP/plan.json" --text-plan-file "$TMP/plan.txt" "$@"
