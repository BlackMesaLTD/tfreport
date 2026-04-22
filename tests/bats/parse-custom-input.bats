#!/usr/bin/env bats
# parse-custom-input.bats — exercises scripts/parse-custom-input.sh as a
# YAML-block → `--custom key=value` translator.

load helpers

setup() {
  strip_gha_env
}

run_parser() {
  run env TFREPORT_INPUT_CUSTOM="$1" bash "$SCRIPTS_DIR/parse-custom-input.sh"
}

@test "empty input with no GHA env → empty output, exit 0" {
  run_parser ""
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "single key: value → single --custom pair" {
  run_parser "owner: platform-team"
  [ "$status" -eq 0 ]
  assert_lines_equal "--custom
owner=platform-team"
}

@test "multiple lines → multiple --custom pairs in order" {
  run_parser "$(printf 'owner: platform-team\nenv: prod')"
  [ "$status" -eq 0 ]
  assert_lines_equal "--custom
owner=platform-team
--custom
env=prod"
}

@test "values with colons (URLs) preserve the colon in value" {
  run_parser "docs_url: https://example.com/docs"
  [ "$status" -eq 0 ]
  assert_lines_equal "--custom
docs_url=https://example.com/docs"
}

@test "leading/trailing whitespace trimmed from key and value" {
  run_parser "   owner   :    platform-team   "
  [ "$status" -eq 0 ]
  assert_lines_equal "--custom
owner=platform-team"
}

@test "# comments and blank lines are skipped" {
  run_parser "$(printf '# comment line\nowner: platform-team\n\n# another comment\nenv: prod')"
  [ "$status" -eq 0 ]
  assert_lines_equal "--custom
owner=platform-team
--custom
env=prod"
}

@test "malformed line (no colon) → non-zero exit with error" {
  run_parser "this-line-has-no-colon"
  [ "$status" -ne 0 ]
  [[ "$stderr" =~ "line missing ':'" ]] || [[ "$output" =~ "line missing ':'" ]]
}

@test "empty key (bare colon) → non-zero exit with error" {
  run_parser ": value-with-no-key"
  [ "$status" -ne 0 ]
  [[ "$stderr" =~ "empty key" ]] || [[ "$output" =~ "empty key" ]]
}

@test "GHA env present → auto-injected keys emitted" {
  run env -i \
    PATH="$PATH" \
    GITHUB_RUN_ID=42 \
    GITHUB_REPOSITORY=owner/repo \
    GITHUB_SHA=deadbeef \
    GITHUB_SERVER_URL=https://github.com \
    TFREPORT_INPUT_CUSTOM="" \
    bash "$SCRIPTS_DIR/parse-custom-input.sh"
  [ "$status" -eq 0 ]
  assert_line_present "run_id=42"
  assert_line_present "repository=owner/repo"
  assert_line_present "sha=deadbeef"
  assert_line_present "workflow_url=https://github.com/owner/repo/actions/runs/42"
}

@test "GHA env + user input → auto pairs emitted first, user after (last-wins via tfreport)" {
  run env -i \
    PATH="$PATH" \
    GITHUB_RUN_ID=42 \
    GITHUB_REPOSITORY=owner/repo \
    GITHUB_SHA=deadbeef \
    GITHUB_SERVER_URL=https://github.com \
    TFREPORT_INPUT_CUSTOM="owner: platform-team" \
    bash "$SCRIPTS_DIR/parse-custom-input.sh"
  [ "$status" -eq 0 ]
  # Auto-injected repository appears
  assert_line_present "repository=owner/repo"
  # User-supplied value appears
  assert_line_present "owner=platform-team"
  # User's line appears AFTER the auto-injected block so tfreport resolves last-wins correctly
  user_line_num=$(printf '%s\n' "$output" | grep -n '^owner=platform-team$' | cut -d: -f1)
  auto_line_num=$(printf '%s\n' "$output" | grep -n '^repository=owner/repo$' | cut -d: -f1)
  [ "$user_line_num" -gt "$auto_line_num" ]
}
