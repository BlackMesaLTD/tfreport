# TODO — Unimplemented Features

Tracking features that are designed but not yet implemented.

## Preset force_new → Classifier Pipeline

**Status:** ForceNewResolver exists but is not wired into the impact classification chain.

`presets.ForceNewResolver()` returns a `func(resourceType, attrName string) (bool, bool)` that checks if an attribute has `force_new: true` in the preset. This should be step 3 in the impact resolution order:

1. `resources.<type>.attributes.<attr>.impact` (config, implemented)
2. `global_attributes.<attr>.impact` (config, implemented)
3. **Preset `force_new: true` → `critical` impact** (NOT wired)
4. `impact_defaults.<action>` (implemented)

**What's needed:**
- Compose `cfg.AttributeImpact` with `ForceNewResolver` into a chained resolver in `internal/cli/root.go`
- When config has no override but preset has `force_new: true`, return `critical`
- Add test coverage for the chained resolution

**Files:** `internal/cli/root.go:79-83`, `internal/presets/loader.go:48-59`

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

## Config Display Name Override Merging

**Status:** `ResourceConfig.DisplayName` is parsed from config but not merged into the display names map passed to the summarizer.

`config.ResourceDisplayName()` exists but `internal/cli/root.go` only pulls display names from presets, not from config `resources.<type>.display_name` overrides.

**What's needed:**
- After building `opts.DisplayNames` from presets, overlay config display names
- Config should take priority over preset display names

**File:** `internal/cli/root.go:86-96`

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

## Sensitive Value Handling

**Status:** `BeforeSensitive` and `AfterSensitive` are parsed but not used.

The parser extracts sensitivity markers from plan JSON, but the differ and formatters don't mask or annotate sensitive values.

**What's needed:**
- Check sensitivity in `Diff()` — mark `ChangedAttribute.Sensitive` field
- Formatters should show `(sensitive)` instead of actual values
- Prevent accidental secret exposure in PR comments

## GitHub Action — presetgen

**Status:** The composite action only ships `tfreport`, not `presetgen`.

**What's needed:**
- Either bundle presetgen in the same release binary
- Or add a separate action for preset generation workflows
- Update `.goreleaser.yml` to build and release presetgen alongside tfreport

## CI Workflows

**Status:** `.goreleaser.yml` exists but `.github/workflows/` CI pipeline needs validation.

**What's needed:**
- Verify `ci.yml` runs `go test -race ./...` on PRs
- Verify `release.yml` triggers goreleaser on tags
- Add golangci-lint step to CI
