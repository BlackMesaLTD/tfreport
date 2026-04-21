#!/usr/bin/env bash
# parse-custom-input.sh — translates the YAML-block `custom:` input used
# by the prepare / action / report-plan composites into repeated
# `--custom key=value` tfreport CLI flags, printed one per line.
#
# Input (env var):
#   TFREPORT_INPUT_CUSTOM — the raw multi-line YAML-block value.
#
# Output (stdout): one `--custom\nkey=value` pair per input line, tab-
# separated for easy readarray consumption. Emits nothing when the input
# is empty. Non-zero exit on malformed lines.
#
# Caller pattern (bash array build):
#   mapfile -t CUSTOM_ARGS < <(TFREPORT_INPUT_CUSTOM="$INPUT_CUSTOM" \
#       bash "$SCRIPT/parse-custom-input.sh")
#   tfreport ... "${CUSTOM_ARGS[@]}"
#
# Semantics:
#   - One `key: value` per line
#   - First `:` splits; values may contain further colons
#   - Leading/trailing whitespace trimmed from key AND value
#   - Lines starting with `#` are comments (skipped)
#   - Blank lines skipped
#   - Values are LITERAL STRINGS — no quotes stripped, no YAML escapes
set -euo pipefail

INPUT="${TFREPORT_INPUT_CUSTOM:-}"
if [ -z "$INPUT" ]; then
  exit 0
fi

while IFS= read -r line; do
  line="${line%%#*}"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [ -z "$line" ] && continue
  case "$line" in
    *:*) ;;
    *)
      echo "::error::parse-custom-input: line missing ':' — $line" >&2
      exit 1
      ;;
  esac
  key="${line%%:*}"
  value="${line#*:}"
  key="${key#"${key%%[![:space:]]*}"}"
  key="${key%"${key##*[![:space:]]}"}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  if [ -z "$key" ]; then
    echo "::error::parse-custom-input: line has empty key — $line" >&2
    exit 1
  fi
  printf -- '--custom\n%s=%s\n' "$key" "$value"
done <<< "$INPUT"
