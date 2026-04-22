#!/usr/bin/env bats
# parse-preserve-input.bats — exercises scripts/parse-preserve-input.sh as a
# path-list → `--preserve path` translator.

load helpers

run_parser() {
  run env TFREPORT_INPUT_PRESERVE="$1" bash "$SCRIPTS_DIR/parse-preserve-input.sh"
}

@test "empty input → empty output, exit 0" {
  run_parser ""
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "single path → single --preserve pair" {
  run_parser "id"
  [ "$status" -eq 0 ]
  assert_lines_equal "--preserve
id"
}

@test "multiple paths in order" {
  run_parser "$(printf 'id\nlocation\ntags.environment')"
  [ "$status" -eq 0 ]
  assert_lines_equal "--preserve
id
--preserve
location
--preserve
tags.environment"
}

@test "dotted paths pass through unchanged (nested-map semantics)" {
  run_parser "tags.environment"
  [ "$status" -eq 0 ]
  assert_lines_equal "--preserve
tags.environment"
}

@test "# comments and blank lines are skipped" {
  run_parser "$(printf '# preserved attrs\nid\n\n# another\nlocation')"
  [ "$status" -eq 0 ]
  assert_lines_equal "--preserve
id
--preserve
location"
}

@test "leading/trailing whitespace trimmed" {
  run_parser "   id   "
  [ "$status" -eq 0 ]
  assert_lines_equal "--preserve
id"
}
