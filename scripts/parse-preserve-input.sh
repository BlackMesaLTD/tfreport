#!/usr/bin/env bash
# parse-preserve-input.sh — translates the YAML-block `preserve_attributes:`
# input (one attribute path per line) into repeated `--preserve path` tfreport
# CLI flags, printed one per line for mapfile consumption.
#
# Input (env var):
#   TFREPORT_INPUT_PRESERVE  — raw multi-line input (optional).
#
# Output (stdout): `--preserve\npath` pairs, tab-separated.
#
# Caller pattern:
#   mapfile -t PRESERVE_ARGS < <(TFREPORT_INPUT_PRESERVE="$INPUT_PRESERVE_ATTRIBUTES" \
#       bash "$SCRIPT/parse-preserve-input.sh")
#   tfreport ... "${PRESERVE_ARGS[@]}"
#
# Semantics:
#   - One path per line (dotted paths OK: `tags.environment`)
#   - Lines starting with `#` are comments
#   - Blank lines skipped
#   - Leading/trailing whitespace trimmed
#   - No key=value shape (unlike parse-custom-input.sh) — this is a flat
#     list of paths, each passed to tfreport as `--preserve <path>`
set -euo pipefail

INPUT="${TFREPORT_INPUT_PRESERVE:-}"
if [ -z "$INPUT" ]; then
  exit 0
fi

while IFS= read -r line; do
  line="${line%%#*}"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [ -z "$line" ] && continue
  printf -- '--preserve\n%s\n' "$line"
done <<< "$INPUT"
