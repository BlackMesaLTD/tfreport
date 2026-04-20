# tfreport

Transform Terraform plan JSON into human-readable reports for CI/CD pipelines.

## Features

- **Plan JSON is the only required input** — works for any provider, any resource, zero config
- **Plain-English summaries** — groups changes and generates human-readable descriptions
- **Multiple output targets** — markdown, GitHub PR body, PR comment, step summary, JSON
- **Provider presets** — bundled display names + per-attribute metadata for azurerm (54 resource types)
- **Attribute-level impact** — global and per-resource-type overrides (e.g., tags = none)
- **Preset generator** — `presetgen` tool parses provider docs to generate enriched presets
- **Team config** — `.tf-report.yml` for module descriptions, attribute overrides, impact rules
- **Single binary** — zero runtime dependencies, installs in seconds

## Quick Start

```bash
# Install
go install github.com/tfreport/tfreport/cmd/tfreport@latest

# Basic usage — pipe plan JSON, get markdown
terraform show -json plan.out | tfreport

# GitHub PR body with deploy checkboxes
terraform show -json plan.out | tfreport --target github-pr-body

# From file
tfreport --plan-file plan.json --target github-step-summary

# Structured JSON output
tfreport --plan-file plan.json --target json
```

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

Create `.tf-report.yml` in your repo root. See [docs/configuration.md](docs/configuration.md) for the full reference.

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
go install github.com/tfreport/tfreport/cmd/presetgen@latest

# Generate from provider docs
presetgen --provider azurerm \
  --docs-dir ./terraform-provider-azurerm/website/docs/r/ \
  --existing-preset azurerm.json \
  --output enriched-azurerm.json
```

## GitHub Actions

```yaml
- name: Generate report
  run: |
    terraform show -json plan.out | tfreport --target github-step-summary >> $GITHUB_STEP_SUMMARY
```

Or use the composite action:

```yaml
- uses: tfreport/tfreport/.github/action@v0
  with:
    plan-file: plan.json
    target: github-pr-body
```

## CLI Reference

```
tfreport [flags]

Flags:
  -t, --target string      output target (default "markdown")
  -f, --plan-file string   read plan JSON from file instead of stdin
  -c, --config string      path to .tf-report.yml config file
      --changed-only       show only changed resources (exclude no-ops)
  -q, --quiet              suppress non-essential output
  -v, --version            version for tfreport
  -h, --help               help for tfreport
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
terraform show -json plan.out
    │
    ▼
┌─────────────────────────────────┐
│ Core Engine                     │
│  Parser → Differ → Grouper     │
│  → Classifier → Summarizer     │
│  → Report                      │
└───────────────┬─────────────────┘
                │
    ┌───────────┼───────────────┐
    ▼           ▼               ▼
 Provider    Team Config     Output
 Presets     .tf-report.yml  Formatters
 (azurerm)   global_attrs    (5 targets)
             resources
```

## License

Apache 2.0
