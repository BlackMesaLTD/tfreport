# TODO — Unimplemented Features

Tracking features that are designed but not yet implemented.

## Full Provider Doc Enrichment

**Status:** presetgen tool works, but only subnet and virtual_network have enriched attributes in the bundled preset.

The bundled `azurerm.json` has 54 resource types with display names, but only 2 have per-attribute metadata (descriptions, force_new). Running presetgen against the full `terraform-provider-azurerm/website/docs/r/` directory would enrich all types.

**What's needed:**
- Clone terraform-provider-azurerm at target version
- Run `presetgen --provider azurerm --docs-dir ... --existing-preset ... --output ...`
- Validate output, update bundled preset
- See workflow in [docs/presetgen.md](presetgen.md#workflow-updating-presets-on-provider-upgrade)

## Additional Provider Presets

**Status:** Only azurerm is bundled. aws and google use the same doc format but have no presets.

**What's needed:**
- Generate `aws.json` preset from `terraform-provider-aws/website/docs/r/`
- Generate `google.json` preset from `terraform-provider-google/website/docs/r/`
- Add to `internal/presets/builtin/`
- Test that `presets.Load("aws")` and `presets.Load("google")` work

## Preset Attribute Descriptions in Output

**Status:** Presets contain per-attribute `description` strings, but no formatter uses them.

The enriched preset has descriptions like "The name of the subnet" for each attribute. These could be shown in diff sections or hover text to give reviewers context.

**What's needed:**
- Pass attribute descriptions through the pipeline (likely via `Report` or a separate lookup)
- Show descriptions in formatters (e.g., as inline comments in diff blocks)

## GitLab / Atlantis Formatter Targets

**Status:** Only GitHub-oriented formatters exist.

Five formatters are implemented, all targeting GitHub. GitLab MR comments and Atlantis webhook output would broaden adoption.

**What's needed:**
- `gitlab-mr-comment` formatter using GitLab markdown flavor
- `atlantis` formatter matching Atlantis output conventions
- Register in `formatter.Get()` dispatcher

## GitHub Action — presetgen

**Status:** The composite action only ships `tfreport`, not `presetgen`.

**What's needed:**
- Either bundle presetgen in the same release binary
- Or add a separate action for preset generation workflows
- Update `.goreleaser.yml` to build and release presetgen alongside tfreport

## CI Workflows

**Status:** `ci.yml` runs go/python/bats unit blocks, binary smoke, action smoke and e2e. `release.yml` triggers goreleaser on tags.

**Still missing:**
- `gofmt -l` / `golangci-lint` gate. `gofmt -l .` currently flags 18 files: 9 CRLF line endings (`textplan.go`, `report_io.go`, `presetgen/*`, `cmd/presetgen`) and 9 with real formatting drift.
- `.gitattributes` (`* text=auto eol=lf`) plus a one-off normalisation; `Makefile` is CRLF, which is why `make docs` reports nothing to do and docgen must be run with `go run ./cmd/docgen --out docs/blocks.md`.

## Step-summary size guard

**Status:** `step_summary_max_kb` meters only `text_plan` bytes. Header, tables and per-resource `<details>` wrappers are unmetered (~33 KB fixed plus ~140 bytes per resource), so a render that hits the budget can exceed GitHub's 1 MiB step-summary cap and the runner drops the whole summary. Measured: a 1,120-resource tag-sweep plan with a 950 KB budget rendered at 1,135,880 bytes.

**What's needed:**
- Total-output budget tracked by the template formatter.
- Degradation ladder (collapse → context 0 → drop later text_plan sections → synthetic diffs → header + link).
- `truncated=` output for the composite actions instead of the consumer grepping for a marker.

## Overflow artifact

**Status:** consumers hand-roll a two-pass render to attach a download link when truncated.

**What's needed:**
- `--overflow-file PATH`: second in-process render with an unlimited budget (same template), plus `truncated=true` in `$GITHUB_OUTPUT` and a `::notice::` when the budgeted render was cut.
- `{{ overflow_url }}` helper emitting a placeholder token the action rewrites after `upload-artifact` returns `artifact-url`.
- `overflow-artifact` and `export-artifact` inputs on `report-plan`, so prepare + render + overflow + send is one composite call.

## Drift surface

**Status:** `resource_drift` in the plan JSON is ignored and drift text markers (`has changed`, `has been deleted`) are not parsed, so "Objects have changed outside of Terraform" never reaches a report.

**What's needed:** parse `resource_drift` into `Report.Drift` (address, action, changed keys); extend `markerRe` so drift text blocks are captured under a separate map; a `drift` block (table by default) and a "N resources changed outside Terraform" line in `plan_counts`.

## Nested block sets — AWS

**Status:** `core.ExpandNestedChanges` reports `azurerm_network_security_group.security_rule` and `azurerm_route_table.route` per element (added / removed / field / string_to_list / list_to_string / case_only / order) with CIDR list-vs-string and case normalisation, and `core.BuildRuleDiff` renders a per-rule terraform-style diff (`rule_delta` block). Object-style `azurerm_network_security_rule` / `azurerm_route` share the same grammar.

**What's needed:**
- AWS entries in `nestedSetSpecs` (`aws_security_group.ingress/egress`, `aws_route_table.route`) once an identity key is agreed (AWS rules have no `name`; a composite of protocol+ports+cidr is the likely key).

## Action / binary version pin

**Status:** composites default `version: latest`, so a SHA-pinned action ref still floats the binary.

**What's needed:** `VERSION` file bumped by the release workflow; `install-tfreport.sh` defaults to it when `TFREPORT_VERSION` is unset or `latest`, so a SHA-pinned action ref implies a pinned binary.
