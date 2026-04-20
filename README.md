# tfreport

Transform Terraform plans into human-readable reports for CI/CD pipelines.

## Features

- **Works for any provider, any resource** — pure plan-format consumer, zero provider-specific assumptions
- **Dual input** — combine plan JSON (structured diff, classification) with plan text (native per-resource output) for the richest reports
- **Plain-English summaries** — groups changes and generates human-readable descriptions
- **Multiple output targets** — markdown, GitHub PR body, PR comment, step summary, JSON
- **Provider presets** — bundled display names + per-attribute metadata for azurerm (54 resource types)
- **Attribute-level impact** — global and per-resource-type overrides (e.g., tags = none)
- **Preset generator** — `presetgen` tool parses provider docs to generate enriched presets
- **Team config** — `.tfreport.yml` for module descriptions, attribute overrides, impact rules
- **Single binary** — zero runtime dependencies, installs in seconds

## Quick Start

The richest output combines the two views of a plan — the structured JSON (for classification and diffs) and the native text output (for per-resource plan blocks that reviewers recognise at a glance).

```bash
# Easiest: the bundled wrapper derives both views from a plan.out and invokes tfreport
tfreport-from-plan plan.out --target github-step-summary

# Manual equivalent: do both terraform show calls yourself
terraform show -json     plan.out > plan.json
terraform show -no-color plan.out > plan.txt
tfreport --plan-file plan.json --text-plan-file plan.txt --target github-step-summary
```

JSON-only modes (work fine, but step-summary / pr-comment lose their per-resource text blocks):

```bash
# Pipe plan JSON via stdin
terraform show -json plan.out | tfreport --target github-pr-body

# Canonical interchange JSON for programmatic consumers
tfreport --plan-file plan.json --target json
```

## Install

**Prebuilt binary (recommended — includes `tfreport-from-plan`):** Download the archive for your platform from the [Releases page](https://github.com/BlackMesaLTD/tfreport/releases/latest) and extract both `tfreport` and `tfreport-from-plan` into your `$PATH`.

**Via `go install` (`tfreport` only):**

```bash
go install github.com/BlackMesaLTD/tfreport/cmd/tfreport@latest
```

The `tfreport-from-plan` wrapper is a short shell script — if you go the `go install` route, copy [`scripts/tfreport-from-plan.sh`](scripts/tfreport-from-plan.sh) from this repo into your `$PATH` as well.

## Output Targets

| Target | Description |
|--------|-------------|
| `markdown` | Plain markdown with tables and key changes (default) |
| `github-pr-body` | Cross-subscription summary with deploy checkboxes |
| `github-pr-comment` | Per-module detail with diff blocks |
| `github-step-summary` | Nested collapsible sections for GitHub Actions summary |
| `json` | Structured JSON for programmatic consumption (canonical interchange format) |

Every non-JSON target is composed from user-overridable Go templates. Filter sections, replace the template inline, or point at a file. See [docs/output-templates.md](docs/output-templates.md) for the block catalog.

## Example Output

```
# Terraform Plan Report

**4 resources** across 3 modules (1 create, 2 update, 1 delete)

## Key Changes

- ✅ New private endpoint: pe-web
- ❗ Removing route: legacy
- ⚠️ Tags updates across 2 subnets
```

## Configuration

Create `.tfreport.yml` in your repo root. See [docs/configuration.md](docs/configuration.md) for the full reference.

```yaml
presets:
  - azurerm

# Global attributes — apply to ALL resource types
global_attributes:
  tags:
    impact: none
    note: "Cosmetic only"

# Per-resource-type attribute overrides
resources:
  azurerm_virtual_network:
    attributes:
      name:
        impact: critical
        note: "Forces replacement"
      address_space:
        impact: high

modules:
  virtual_network:
    description: "Managed VNet with subnets, NSGs, and route tables"

impact_defaults:
  replace: critical
  delete: high
  update: medium
  create: low
```

### Impact Resolution Order

For update actions, attribute-level impact is resolved in this priority:

1. **Resource-specific** — `resources.<type>.attributes.<attr>.impact`
2. **Global attribute** — `global_attributes.<attr>.impact`
3. **Preset force_new** — if preset marks attribute as `force_new: true`
4. **Action default** — `impact_defaults.<action>`

If ALL changed attributes resolve to an impact, the highest is used. If ANY attribute lacks an override, falls back to action default.

### Backward Compatibility

The old `attributes:` key (without `global_` prefix) still works and is treated as `global_attributes:`.

## Preset Generator

The `presetgen` tool generates enriched preset JSON from Terraform provider documentation. See [docs/presetgen.md](docs/presetgen.md) for full usage.

```bash
# Install
go install github.com/BlackMesaLTD/tfreport/cmd/presetgen@latest

# Generate from provider docs
presetgen --provider azurerm \
  --docs-dir ./terraform-provider-azurerm/website/docs/r/ \
  --existing-preset azurerm.json \
  --output enriched-azurerm.json
```

## GitHub Actions

The composite action takes a terraform binary plan file and derives both JSON and text internally — you just need `hashicorp/setup-terraform` earlier in the workflow so `terraform` is on `PATH`:

```yaml
- uses: hashicorp/setup-terraform@v3
- run: terraform plan -out=plan.out
- uses: BlackMesaLTD/tfreport/.github/action@v0
  with:
    plan: plan.out
    target: github-step-summary
```

If you already produce the JSON yourself, `plan-file` remains supported (pass `text-plan-file` alongside for the full output):

```yaml
- uses: BlackMesaLTD/tfreport/.github/action@v0
  with:
    plan-file: plan.json
    text-plan-file: plan.txt
    target: github-pr-body
```

Or skip the composite action entirely and shell out:

```yaml
- name: Generate report
  run: |
    terraform show -json     plan.out > plan.json
    terraform show -no-color plan.out > plan.txt
    tfreport --plan-file plan.json --text-plan-file plan.txt \
             --target github-step-summary >> $GITHUB_STEP_SUMMARY
```

## CLI Reference

```
tfreport [flags]

Flags:
  -t, --target string            output target (default "markdown")
  -f, --plan-file string         read plan JSON from file instead of stdin
      --text-plan-file string    path to terraform text plan output (from `terraform show -no-color plan.out`)
      --report-file strings      read previously exported tfreport JSON (repeatable for multi-report aggregation)
      --label string             subscription/environment label (stored in JSON export)
  -c, --config string            path to .tfreport.yml config file
      --changed-only             show only changed resources (exclude no-ops)
  -q, --quiet                    suppress non-essential output
  -v, --version                  version for tfreport
  -h, --help                     help for tfreport
```

The `tfreport-from-plan` wrapper takes a terraform binary plan file and forwards remaining flags:

```
tfreport-from-plan <plan.out> [tfreport flags...]

Environment:
  TFREPORT_TERRAFORM_BIN   path to the terraform binary (default: terraform on PATH)
  TFREPORT_BIN             path to the tfreport binary  (default: tfreport on PATH)
```

```
presetgen [flags]

Flags:
      --provider string          provider name prefix, e.g. azurerm (required)
      --docs-dir string          path to provider docs/r/ directory (required)
  -o, --output string            output path for generated preset JSON (required)
      --schema-file string       terraform providers schema -json output (optional)
      --existing-preset string   merge with existing preset JSON (optional)
      --resources string         comma-separated resource types to include (optional)
      --version string           provider version string for the preset (optional)
```

## Architecture

```
terraform show -json     plan.out  ──┐
terraform show -no-color plan.out  ──┤
                                     ▼
                       ┌─────────────────────────────────┐
                       │ Core Engine                     │
                       │  Parser → Differ → Grouper      │
                       │  → Classifier → Summarizer      │
                       │  → Report (+ text plan blocks)  │
                       └───────────────┬─────────────────┘
                                       │
                          ┌────────────┼────────────┐
                          ▼            ▼            ▼
                       Provider    Team Config   Output
                       Presets     .tfreport.yml Formatters
                       (azurerm)   global_attrs   (5 targets)
                                   resources
```

The structured JSON powers grouping, diffing, classification, and summaries.
The text plan supplies the native per-resource blocks that reviewers recognise from their terminal — only rendered by `github-step-summary` and `github-pr-comment` targets, and only when a text plan is supplied.

## License

Apache 2.0
