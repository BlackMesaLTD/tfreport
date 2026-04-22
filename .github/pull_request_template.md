## Summary

<!-- What does this PR do and why?
     Link any related issue, e.g. `Closes #123`. -->

## Testing

<!-- How did you verify the change?
     Call out specific fixtures used, rendered output reviewed, etc.
     CI covers unit (go/python/bats), binary smoke, action smoke, and E2E —
     mention anything not covered. -->

- [ ] `go test -race ./...` passes locally
- [ ] `bats tests/bats/` passes locally (if `scripts/parse-*.sh` touched)
- [ ] Output-affecting change: rendered against `testdata/` fixtures and the diff looks right
- [ ] Composite-action change: all 7 `.github/action/*/action.yml` files in sync if a new binary flag is being exposed

## Breaking changes

<!-- Delete this section if none.
     Flag any change to: CLI flags, .tfreport.yml schema, the JSON export
     format (round-trip via `core.MarshalReport` / `core.UnmarshalReport`),
     or composite action inputs/outputs. Describe the migration. -->

## Notes for reviewers

<!-- Anything non-obvious: design decisions, deliberate scope boundaries,
     known follow-ups, screenshots for rendered-output changes. -->
