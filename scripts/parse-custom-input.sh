#!/usr/bin/env bash
# parse-custom-input.sh — translates user-supplied `custom:` YAML-block input
# into repeated `--custom key=value` tfreport CLI flags, AND auto-prepends a
# standard set of GitHub-Actions context fields when the usual GHA env vars
# are available. User values take precedence over auto-injected ones (the
# Go CLI's --custom parser resolves repeated keys last-wins).
#
# Input (env vars):
#   TFREPORT_INPUT_CUSTOM  — raw multi-line YAML-block value (optional).
#   GITHUB_*               — GHA runner-injected context (optional; present
#                            whenever this script runs inside a GHA step).
#
# Output (stdout): one `--custom\nkey=value` pair per resolved entry, tab-
# separated for easy mapfile consumption. Emits nothing when nothing is set.
# Non-zero exit on malformed user input.
#
# Caller pattern (bash array build):
#   mapfile -t CUSTOM_ARGS < <(TFREPORT_INPUT_CUSTOM="$INPUT_CUSTOM" \
#       bash "$SCRIPT/parse-custom-input.sh")
#   tfreport ... "${CUSTOM_ARGS[@]}"
#
# Semantics:
#   - Auto-injected keys (when $GITHUB_RUN_ID is set):
#       workflow_url   Composed URL to the current workflow run.
#       run_id         $GITHUB_RUN_ID
#       run_number     $GITHUB_RUN_NUMBER
#       run_attempt    $GITHUB_RUN_ATTEMPT
#       sha            $GITHUB_SHA
#       ref_name       $GITHUB_REF_NAME (branch/tag name)
#       actor          $GITHUB_ACTOR
#       workflow       $GITHUB_WORKFLOW
#       event_name     $GITHUB_EVENT_NAME
#       repository     $GITHUB_REPOSITORY
#       server_url     $GITHUB_SERVER_URL
#   - User input parsing:
#     - One `key: value` per line
#     - First `:` splits; values may contain further colons
#     - Leading/trailing whitespace trimmed from key AND value
#     - Lines starting with `#` are comments (skipped)
#     - Blank lines skipped
#     - Values are LITERAL STRINGS — no quotes stripped, no YAML escapes
set -euo pipefail

# --- auto-inject GHA context fields ---
AUTO=""
if [ -n "${GITHUB_RUN_ID:-}" ]; then
  AUTO=$(cat <<EOF
workflow_url: ${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-}/actions/runs/${GITHUB_RUN_ID}
run_id: ${GITHUB_RUN_ID}
run_number: ${GITHUB_RUN_NUMBER:-}
run_attempt: ${GITHUB_RUN_ATTEMPT:-1}
sha: ${GITHUB_SHA:-}
ref_name: ${GITHUB_REF_NAME:-}
actor: ${GITHUB_ACTOR:-}
workflow: ${GITHUB_WORKFLOW:-}
event_name: ${GITHUB_EVENT_NAME:-}
repository: ${GITHUB_REPOSITORY:-}
server_url: ${GITHUB_SERVER_URL:-https://github.com}
EOF
)
fi

USER_INPUT="${TFREPORT_INPUT_CUSTOM:-}"

# --- compose: auto first so user values override on key collision ---
if [ -n "$AUTO" ] && [ -n "$USER_INPUT" ]; then
  COMBINED=$(printf '%s\n%s' "$AUTO" "$USER_INPUT")
elif [ -n "$AUTO" ]; then
  COMBINED="$AUTO"
else
  COMBINED="$USER_INPUT"
fi

if [ -z "$COMBINED" ]; then
  exit 0
fi

# --- parse each line into --custom key=value pairs ---
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
done <<< "$COMBINED"
