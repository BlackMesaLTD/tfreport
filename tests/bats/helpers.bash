#!/usr/bin/env bash
# helpers.bash — shared setup/teardown and assert primitives for tfreport
# bats suites. Source this from every *.bats file via `load helpers`.

# REPO_ROOT resolves to the tfreport repo root regardless of where bats is
# invoked from. BATS_TEST_DIRNAME is set by bats to the .bats file's dir.
REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
SCRIPTS_DIR="$REPO_ROOT/scripts"

# strip_gha_env — unset every GITHUB_* var so tests run with predictable
# (non-auto-injecting) env. parse-custom-input.sh gates its auto-inject
# block on $GITHUB_RUN_ID specifically, but we clear the whole namespace
# to keep tests hermetic.
strip_gha_env() {
  local var
  for var in $(env | awk -F= '/^GITHUB_/{print $1}'); do
    unset "$var"
  done
}

# mktemp_test_dir — per-test tempdir under $BATS_TEST_TMPDIR (auto-cleaned
# by bats). Exports TMP_TEST_DIR for the caller.
mktemp_test_dir() {
  TMP_TEST_DIR="$(mktemp -d "${BATS_TEST_TMPDIR:-/tmp}/tfreport-bats.XXXXXX")"
  export TMP_TEST_DIR
}

# assert_lines_equal — compare bats `$output` (one string, \n-joined) to a
# newline-separated expected value. Fails with a diff on mismatch.
assert_lines_equal() {
  local expected="$1"
  if [ "$output" != "$expected" ]; then
    printf 'expected:\n%s\n---\ngot:\n%s\n' "$expected" "$output" >&2
    return 1
  fi
}

# assert_line_present — fail unless $output contains the given line exactly
# (as one of the lines, anchored).
assert_line_present() {
  local needle="$1"
  if ! printf '%s\n' "$output" | grep -qxF "$needle"; then
    printf 'line not present: %s\n---\noutput:\n%s\n' "$needle" "$output" >&2
    return 1
  fi
}
