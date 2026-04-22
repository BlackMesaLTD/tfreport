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
- **State preservation** — checkboxes, radio selections, and free-text notes survive PR re-renders ([docs](docs/state-preservation.md))
- **Single binary** — zero runtime dependencies, installs in seconds

## Quick Start

The richest output combines the two views of a plan — the structured JSON (for classification and diffs) and the native text output (for per-resource plan blocks that reviewers recognise at a glance).

```bash
# Easiest: the bundled wrapper derives both views from a plan.out and invokes tfreport
tfreport-from-plan plan.out --target github-step-summary

# Manual equivalent: do both terraform show calls yourself
terraform show -json     plan.out > plan.show.json
terraform show -no-color plan.out > plan.show.txt
tfreport --plan-file plan.show.json --text-plan-file plan.show.txt --target github-step-summary
```

Filenames use a `.show.` infix so they don't collide with the streaming `terraform plan -json` log that some workflows also name `plan.json`. Pick whatever names you like — tfreport doesn't care, the inputs are just flags.

JSON-only modes (work fine, but step-summary / pr-comment lose their per-resource text blocks):

```bash
# Pipe plan JSON via stdin
terraform show -json plan.out | tfreport --target github-pr-body

# Canonical interchange JSON for programmatic consumers
tfreport --plan-file plan.show.json --target json
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

Seven composite actions under `.github/action/`. Start with one of the two presets; reach for primitives only when composing something unusual.

### Presets (recommended — one step per journey)

| Path | When to use |
|---|---|
| `action/report-plan` | Single Terraform plan → rendered + sent to GitHub |
| `action/report-matrix` | Multi-subscription matrix → download artifacts, aggregate, sent to GitHub |

### Primitives (mix-and-match for unusual flows)

| Path | Stage | Job |
|---|---|---|
| `action` | render | Render a single plan to a file / step output |
| `action/prepare` | upload | Per-sub matrix leg — export report JSON + upload as an artifact |
| `action/download` | download | Download artifacts by pattern + emit an inventory (`count`, `reports`) |
| `action/aggregate` | render | Combine N local report JSONs into one rendered output |
| `action/send` | send | Write a rendered snippet to step summary / PR body / sticky PR comment |

The pipeline shape:

```
Single plan:    render → send
Matrix:         prepare (per sub) → download → aggregate → send
```

Presets and primitives are each self-contained — no composite action references any sibling composite. Shared shell/python logic (binary install, custom-input parsing, the send logic) lives in `scripts/`. Pinning `@vX.Y.Z` on an outer action is the only version coordination needed.

### Single plan — `report-plan` preset

```yaml
- uses: hashicorp/setup-terraform@v3
- run: terraform plan -out=plan.out
- uses: BlackMesaLTD/tfreport/.github/action/report-plan@v0
  with:
    plan: plan.out
    target: github-pr-body
    pr-body-marker: TFREPORT
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

Variants: pass `plan-file` (+ optional `text-plan-file`) instead of `plan` if you have the plan derivations already. Add `step-summary: 'true'` and `pr-comment-marker: TFREPORT` in any combination to hit multiple destinations from one call.

### Multi-subscription matrix — `prepare` + `report-matrix` preset

**Inside the plan matrix** (one step per leg) — `tfreport-prepare` exports a per-sub report JSON and uploads it as an artifact:

```yaml
- uses: BlackMesaLTD/tfreport/.github/action/prepare@v0
  with:
    plan-file: ./subscriptions/${{ matrix.subscription }}/plan.show.json
    text-plan-file: ./subscriptions/${{ matrix.subscription }}/plan.show.txt
    label: ${{ matrix.subscription }}
    config: .tfreport.yml
    # Optional: attach per-sub metadata that downstream templates can read
    # as {{ $r.Custom.<key> }} (e.g. subscription GUID, plan-workflow run ID).
    custom: |
      sub_id: ${{ matrix.ID }}
      run_id: ${{ github.run_id }}
```

Default artifact name is `tfreport-<label>` (override with `artifact-name`).

**In a downstream job** — one step does the rest:

```yaml
- uses: BlackMesaLTD/tfreport/.github/action/report-matrix@v0
  with:
    target: github-pr-body
    config: .tfreport.yml
    pr-body-marker: TFREPORT
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

`artifact-pattern` defaults to `tfreport-*` matching what `prepare` uploads. Empty-state is handled automatically — if zero matching artifacts exist, the preset writes its `empty-message` default (`_No infrastructure changes detected in this PR._`, overridable) and still sends it.

### `send` — destinations in detail

Three optional destinations; any combination in a single call. Every preset exposes these inputs; so does `send` directly for power users with pre-rendered markdown.

| Input | Effect |
|---|---|
| `step-summary: 'true'` | Appends to `$GITHUB_STEP_SUMMARY`. No token needed. |
| `pr-body-marker: FOO` | Splices between `<!-- BEGIN_FOO -->` and `<!-- END_FOO -->` in the PR description. Appends the markers at the end if absent. Needs `github-token`. |
| `pr-comment-marker: BAR` | Upserts a sticky PR comment identified by `<!-- BAR -->` at the start of the comment body. Creates on first run, edits the same comment thereafter. Needs `github-token`. |

PR number is auto-derived from `$GITHUB_EVENT_PATH` (both `pull_request` and `issue` events). Override with `pr-number` for unusual cases. Outside PR context, PR destinations log a warning and skip — non-fatal.

Job permissions needed for PR destinations: `pull-requests: write`.

### Large renders

Step output `report` is capped at ~1 MB by GitHub. `output-file` is always canonical — prefer it for step-summary renders across many subs. Presets always write to a temp file internally so `send` never hits the cap regardless.

### Bare shell equivalent

Skip the composite action entirely and shell out:

```yaml
- name: Generate report
  run: |
    terraform show -json     plan.out > plan.show.json
    terraform show -no-color plan.out > plan.show.txt
    tfreport --plan-file plan.show.json --text-plan-file plan.show.txt \
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
      --custom key=value         arbitrary metadata stored on the report (repeatable).
                                 Accessible in templates as {{ $r.Custom.<key> }} and survives JSON round-trip.
      --previous-body-file path  previously-rendered body (e.g. fetched PR body).
                                 When set, preserve regions carry prior content forward.
                                 Use `-` for stdin. See docs/state-preservation.md.
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
