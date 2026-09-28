# Configuration Reference

tfreport is configured via `.tfreport.yml` (or `.tfreport.yaml`) in the repo root, or via `--config` flag.

All fields are optional. The tool works with zero config.

## What Goes Where: Config vs. Templates

tfreport has two configuration surfaces. Knowing which one owns a given setting saves a lot of confusion.

**`.tfreport.yml` owns:**

- **Domain knowledge** — what resources mean, how impacts are classified, what module names describe. (`presets`, `modules`, `global_attributes`, `resources`, `impact_defaults`)
- **Global render defaults** — knobs that apply to every target unless overridden. (`output.max_resources_in_summary`, `output.code_format`, `output.step_summary_max_kb`, `output.group_submodules`, `output.submodule_depth`, `output.changed_attrs_display`)
- **Per-target overrides of those defaults** — e.g. `code_format: plain` just for `github-pr-comment`. (`output.targets.<name>.<knob>`)
- **Target wiring** — which template file or section filter applies to which target. (`output.targets.<name>.{template_file, sections}`)
- **Trivial inline templates** — one-liners like "add a banner above `.Title`". (`output.targets.<name>.template`)

**Templates (Go `text/template`) own:**

- **Section ordering and inclusion** — which blocks appear and in what order.
- **Target-specific grammar** — markdown headers vs. `<details>` blocks. Handled inside blocks automatically; templates just pick which blocks to invoke.
- **Conditional logic based on the report** — `{{ if eq .Report.MaxImpact "critical" }}…{{ end }}` banners.
- **Per-call overrides of config defaults** — `{{ text_plan "fence" "hcl" }}` beats `output.code_format: diff` for that one call; `{{ instance_detail "max" 10 }}` overrides `output.max_resources_in_summary`.
- **Reusable content fragments** — legal disclaimers, approval notes. Stored as standalone files and inlined via `{{ include "./.github/legal.md" }}`. Not in YAML.

**Both** (config = default, template = per-call override):

| Knob                       | YAML default                            | Template override                               |
|----------------------------|------------------------------------------|-------------------------------------------------|
| `code_format`              | `output.code_format`                     | `{{ text_plan "fence" "hcl" }}`                 |
| `group_submodules`         | `output.group_submodules`                | `{{ instance_detail "group_submodules" true }}` |
| `max_resources_in_summary` | `output.max_resources_in_summary`        | `{{ instance_detail "max" 10 }}` (also on `summary_table`, `changed_resources_table`) |
| `step_summary_max_kb`      | `output.step_summary_max_kb`             | *(shared budget; not per-call)*                 |

**Neither:** environment-specific values, secrets, or dynamic lookups. Both surfaces are checked-in-the-repo artifacts.

**Rule of thumb:** if you'd want the same setting on a fresh checkout by a teammate on a different machine, it's config-or-template. If it's a one-off, pass it on the command line.

## Full Schema

```yaml
presets: []string              # Provider presets to load
modules: map                   # Module descriptions
global_attributes: map         # Attribute overrides for ALL resource types
resources: map                 # Per-resource-type attribute overrides
impact_defaults: map           # Action-to-impact mapping
output: object                 # Output formatting + per-target templates
```

## presets

```yaml
presets:
  - azurerm                    # loads bundled preset
  - /path/to/custom.json       # loads from file path
```

Presets provide display names and per-attribute metadata (descriptions, force_new). Bundled presets are embedded in the binary.

**Bundled presets:** `azurerm` (54 resource types with display names), `aws`, `google`.

## modules

```yaml
modules:
  virtual_network:
    description: "Managed VNet with subnets, NSGs, and route tables"
  bootstrap:
    description: "Base resource group and prerequisites"
```

Module names match the short name extracted from module paths in terraform plan JSON. `module.virtual_network.azurerm_subnet.app` maps to module name `virtual_network`.

## global_attributes

```yaml
global_attributes:
  tags:
    impact: none
    note: "Cosmetic only"
  delegation:
    impact: high
    note: "Triggers NSG redeployment"
```

Global attributes apply to ALL resource types. For update actions, if ALL changed attributes have an impact override, the highest is used instead of the action default.

**Impact values:** `critical`, `high`, `medium`, `low`, `none`

## resources

```yaml
resources:
  azurerm_virtual_network:
    display_name: "virtual network"
    attributes:
      name:
        impact: critical
        note: "Forces replacement"
      address_space:
        impact: high
  azurerm_subnet:
    attributes:
      address_prefixes:
        impact: high
        note: "Changes IP allocation"
```

Per-resource-type overrides take priority over `global_attributes` for the matching resource type. The structure mirrors the preset JSON format so config and presets compose naturally.

## impact_defaults

```yaml
impact_defaults:
  replace: critical
  delete: high
  update: medium
  create: low
```

Maps terraform actions to impact levels. Used when no attribute-level override applies.

**Actions:** `replace`, `delete`, `update`, `create`, `read`
**Impacts:** `critical`, `high`, `medium`, `low`, `none`

## output

Global output options. For per-target customization (templates, section filters) see [docs/output-templates.md](output-templates.md).

```yaml
output:
  max_resources_in_summary: 50    # cap instances shown per step-summary section
  group_submodules: false         # nest sub-modules within each instance
  submodule_depth: 1              # depth of nesting when group_submodules is true
  step_summary_max_kb: 800        # text plan budget for step-summary (GitHub limit ~1024KB)
  code_format: diff               # code block fence: diff | hcl | plain
  changed_attrs_display: dash     # create/delete Changed column: dash | wordy | count | list
  collapse_unchanged: false       # fold runs of unchanged lines in text_plan blocks
  unchanged_context: 1            # context lines kept either side of a change when collapsing

  targets:
    github-pr-comment:
      sections:
        hide: [footer]              # drop named sections from default template
      code_format: plain            # override output.code_format for this target
    github-step-summary:
      template_file: ./my-template.tmpl   # replace default with external template
      step_summary_max_kb: 400      # tighter budget for this target only
      group_submodules: true        # override global group_submodules for this target
    markdown:
      template: |                   # replace default with inline template
        {{ .Title }}

        {{ key_changes "max" 5 }}
```

### Per-target knob overrides

Any knob under `output.*` can be overridden per target by setting the same
key under `output.targets.<name>.<knob>`. Supported overrides:

- `code_format`
- `max_resources_in_summary`
- `group_submodules`
- `submodule_depth`
- `step_summary_max_kb`
- `changed_attrs_display`
- `collapse_unchanged`
- `unchanged_context`
- `preserve_attributes`

Resolution order (highest wins):

1. Block argument in the template (e.g. `{{ text_plan "fence" "hcl" }}`)
2. `output.targets.<target>.<knob>` (per-target override)
3. `output.<knob>` (global default)
4. Hardcoded fallback (e.g. 50 for max, 800 KB for budget, `"diff"` for code_format, `"dash"` for changed_attrs_display)

### `preserve_attributes`

List of resource attribute paths to preserve on each `ResourceChange`, opt-in. Preserved values survive the JSON round-trip and are accessible in templates as `{{ index $rc.Preserved "<path>" }}`. Dotted paths walk nested maps.

```yaml
output:
  preserve_attributes:
    - id
    - location
    - tags.environment
```

**Safety model** (defence-in-depth):

- Sensitive-marked attrs (per terraform's `before_sensitive`/`after_sensitive`) are **never preserved**, regardless of the allowlist. A stderr warning is emitted when such a skip happens.
- Computed (known-after-apply) values are preserved as the literal string `(known after apply)`.
- Sensitive wins over computed: a sensitive computed attr is absent (not sentinelled).

CLI `--preserve key` overrides the config list when both are specified.

See also: `docs/output-templates.md` — the `.Preserved` section of the raw-data escape hatch for template patterns.

### `collapse_unchanged` / `unchanged_context`

Terraform's text plan echoes every unchanged sibling attribute around a
change (all nine tags when one tag is removed, every closing brace, its own
`# (N unchanged attributes hidden)` notes). On tag-sweep and NSG-heavy
plans those context lines are 55-60% of the bytes in a step summary.

`collapse_unchanged: true` folds each run of unchanged lines inside a
`text_plan` block into one marker, keeping `unchanged_context` lines either
side of every changed line (unified-diff style):

```diff
   # module.nsg["app"].azurerm_network_security_group.main will be updated in-place
!  resource "azurerm_network_security_group" "main" {
         # ... (2 unchanged lines hidden)
!      tags = {
-          "BusinessUnit" = "DTS" -> null
           "cost_centre" = "cc-0001"
           # ... (8 unchanged lines hidden)
```

Rules: the resource marker line is always kept; `-/+` replace headers and
`<=` data reads count as changes; a run is only folded when it saves at
least one line; the marker inherits the indentation of the first hidden
line. Collapsing runs before the byte budget is charged, so it stretches
`step_summary_max_kb` rather than only shrinking output. Per-block args
`collapse` and `context` on `text_plan` override the config for one call.
Measured on a 1,120-resource tag-sweep plan: `unchanged_context: 1` cut the
text-plan bytes by 30%, `0` by 50%.

### `changed_attrs_display`

Controls how the Changed column renders for **create** and **delete** resources — where the attribute list is noise because every attribute is either new or going away. Update and replace actions always show the keys list regardless of this setting.

| Value | Create cell | Delete cell |
|-------|-------------|-------------|
| `dash` (default) | `—` | `—` |
| `wordy` | `new` | `removed` |
| `count` | `3 attrs` | `2 attrs` |
| `list` (legacy) | `a, b, c` | `x, y, z` |

Applies to the `module_details`, `changed_resources_table`, and `modules_table` blocks. Each block accepts a `changed_attrs_display` arg that overrides the config value. For `modules_table`'s group-level `changed_attrs` union column, non-`list` modes also exclude create/delete resources' attrs from the union when update/replace resources are present.

### Template selection

The `output.targets.<name>.{template, template_file, sections}` fields are
mutually exclusive modes of customizing the rendered template:

- `template` — inline Go text/template (keep to ~5 lines; YAML escaping gets awkward)
- `template_file` — external file (resolved relative to the config file, same sandbox rules as `{{ include }}`)
- `sections` — keep/drop named sections from the default template (no template knowledge required)

Template-selection modes and knob overrides compose: you can use `sections.hide` *and* `code_format: plain` on the same target.

See [docs/output-templates.md](output-templates.md) for the block catalog and Sprig usage.

## Impact Resolution

For **update** actions with changed attributes, impact is resolved in this order:

1. `resources.<type>.attributes.<attr>.impact` (most specific)
2. `global_attributes.<attr>.impact`
3. Preset `force_new: true` on the attribute → `critical`
4. `impact_defaults.update` (least specific)

**Rule:** If ALL changed attributes resolve to an impact (steps 1-3), the **highest** impact is used. If ANY attribute lacks an override, the entire resolution falls back to step 4.

This prevents a `tags: none` override from masking a dangerous `address_prefixes` change that has no override.

### Block-set attributes (NSG rules, routes)

For `azurerm_network_security_group.security_rule` and
`azurerm_route_table.route`, tfreport reports changes per element instead
of one opaque `security_rule` key:

| Key | Meaning |
|---|---|
| `security_rule[<name>].added` / `.removed` | rule present only after / only before |
| `security_rule[<name>].<field>` | that field differs after normalisation (Description shows `old → new`) |
| `security_rule[<name>].[<fields>].string_to_list` | same members; those fields moved from string to list form |
| `security_rule[<name>].[<fields>].list_to_string` | the reverse (console save, bulk update script) |
| `security_rule[<name>].<field>.case_only` | letter case only |
| `security_rule.order` | identical set, elements reordered |

Normalisation merges `source_address_prefix` with `source_address_prefixes`
(and the destination / port pairs), compares `protocol`, `access`,
`direction`, `next_hop_type` case-insensitively, and treats empty strings,
empty lists and nulls as equal. `azurerm_network_security_rule` and
`azurerm_route` (object-style modules, one resource per rule) get the same
equivalence on their own attributes, and on update their keys are prefixed
with the rule name (`security_rule[allow-443].priority`, `security_rule[allow-443].[destination_address_prefixes,source_address_prefixes].string_to_list`) so both module
styles share one grammar. Impact overrides on the resource's own attribute
(`priority`) still apply to the prefixed key.

The `rule_delta` block renders the same pairing as a terraform-style diff
per rule (only differing fields, verdict comment, unchanged counts); see
[docs/blocks.md](blocks.md#rule_delta).

Impact lookup tries the full key, then the base attribute, so
`resources.azurerm_network_security_group.attributes.security_rule.impact: high`
covers every `security_rule[...]` key. the cosmetic verdict keys (`string_to_list`, `list_to_string`, `case_only`, `rewritten`, `order`) default to
`none`; override them explicitly if you want cosmetic rewrites to count:

```yaml
resources:
  azurerm_network_security_group:
    attributes:
      security_rule: { impact: high, note: "Traffic policy" }
      security_rule.order: { impact: low }
```

## Default Config

When no config file is found, these defaults are used:

```yaml
impact_defaults:
  replace: critical
  delete: high
  update: medium
  create: low
output:
  max_resources_in_summary: 50
  code_format: diff
```
