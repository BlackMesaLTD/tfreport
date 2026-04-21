# Output Templates

tfreport composes output via user-overridable Go `text/template`s. Every
markdown-flavor target (`markdown`, `github-pr-body`, `github-pr-comment`,
`github-step-summary`) ships a default template that you can customize by:

1. **Filtering sections** — keep/drop named blocks from the default template
   (no template knowledge required).
2. **Replacing the template inline** or **pointing at a file**.
3. **Dropping to raw data** for fully custom output via the exposed report
   struct + Sprig functions.

The `json` target is the canonical interchange format and is not
customizable.

> **Looking for ready-made templates?** See
> [docs/template-recipes.md](template-recipes.md) for eight curated
> recipes organized by persona (SRE, architect, newbie, power user) and
> scenario (greenfield, import, surgical change, fleet bump).
>
> **Designing templates for your team?** See
> [docs/template-design-guide.md](template-design-guide.md) for the
> repeatable five-step design loop and the gap roadmap.
>
> **Looking for the authoritative block reference?** See
> [docs/blocks.md](blocks.md) — auto-generated from `Block.Doc()`
> metadata via `make docs`. Lists every registered block's args, columns
> (where applicable), and example renderings. The sections below in
> this file are the narrative companion; blocks.md is the truth.

---

## Table of Contents

- [Three composition tiers](#three-composition-tiers)
- [Block reference (with examples)](#block-reference)
  - [`title`](#title) · [`plan_counts`](#plan_counts) · [`key_changes`](#key_changes) · [`summary_table`](#summary_table)
  - [`module_details`](#module_details) · [`instance_detail`](#instance_detail) · [`text_plan`](#text_plan)
  - [`changed_resources_table`](#changed_resources_table) · [`deploy_checklist`](#deploy_checklist) · [`footer`](#footer)
  - [`risk_histogram`](#risk_histogram) · [`diff_groups`](#diff_groups) · [`fleet_homogeneity`](#fleet_homogeneity) · [`glossary`](#glossary)
- [Template helpers](#template-helpers) (`action_count`, `import_count`, `sample`, `impact_is`, `action_is`)
- [Raw data (`.Report`, `.Reports`)](#raw-data-escape-hatch)
- [Sprig functions](#sprig-functions)
- [`{{ include "path" }}`](#include-path)
- [Configuring a target](#configuring-a-target)
- [Worked examples](#worked-examples)
- [Default templates (reference)](#default-templates-reference)
- [Template design constraints](#template-design-constraints)

---

## Three Composition Tiers

### Tier 1 — Pre-rendered properties

Zero-arg template variables. Target-grammar aware (e.g. `.Title` renders
differently for step-summary vs markdown).

| Variable                 | Block name           | What you get                                         |
|--------------------------|----------------------|------------------------------------------------------|
| `{{ .Title }}`           | `title`              | Plan title header                                    |
| `{{ .PlanCounts }}`      | `plan_counts`        | "Plan: 2 to add, 3 to change, 1 to destroy."         |
| `{{ .KeyChanges }}`      | `key_changes`        | Plain-English summary bullets                        |
| `{{ .SummaryTable }}`    | `summary_table`      | Resource-count table (module/module_type/subscription) |
| `{{ .DeployChecklist }}` | `deploy_checklist`   | `- [ ] **label** (impact)` checkboxes (multi-report) |
| `{{ .CrossSubTable }}`   | `summary_table`      | Per-subscription table (multi-report only)           |
| `{{ .Footer }}`          | `footer`             | Credit + data-source read count                      |

### Tier 2 — Parameterized functions

Template functions with `key=value` arguments (space-separated in Go
template syntax). Use these when you want the same block rendered
differently than the property form.

### Tier 3 — Raw-data escape hatch

`{{ .Target }}`, `{{ .Report }}`, `{{ .Reports }}` expose the underlying
`core.Report` struct for fully custom output — see
[Raw data escape hatch](#raw-data-escape-hatch).

---

## Block Reference

Every example below shows: **template input** → **rendered output** (run
against `testdata/small_plan.json`, which has 1 create, 2 update, 1 delete).

### `title`

Target-aware plan header.

**Template:**
```tmpl
{{ .Title }}
```

**Rendered per target:**

| Target              | Output                                              |
|---------------------|-----------------------------------------------------|
| `markdown`          | `# Terraform Plan Report`                            |
| `github-pr-body`    | `## Infrastructure Change Summary`                   |
| `github-pr-comment` | `### Terraform Plan — 4 resources`                   |
| `github-step-summary` | `### ❗ Terraform Plan detected changes to infrastructure.` |

Multi-report: pr-comment header becomes `### Terraform Plan — 2 subscriptions, 8 resources`.

**No arguments.**

---

### `plan_counts`

Terraform-style verb summary.

**Template:**
```tmpl
{{ .PlanCounts }}
```

**Rendered:**
```
Plan: 1 to add, 2 to change, 1 to destroy.
```

Empty plan renders `No changes detected.` — so you don't need `{{ if }}`
guards in your template.

**No arguments.**

---

### `key_changes`

Plain-English summary bullets. Prepends a `## Key Changes` header on the
`markdown` target and `**Key changes:**` on `github-pr-body`. Otherwise
emits just the bullets (you supply the context).

**Template (property form):**
```tmpl
{{ .KeyChanges }}
```

**Rendered (markdown target):**
```
## Key Changes

- ✅ New private endpoint: pe-web
- ❗ Removing route: legacy-route
- ⚠️ Tags updates across 2 subnets
```

**Parameterized form — cap to the top N:**
```tmpl
{{ key_changes "max" 2 }}
```
→
```
## Key Changes

- ✅ New private endpoint: pe-web
- ❗ Removing route: legacy-route
- _... 1 more changes_
```

| Arg   | Type  | Default | Meaning                              |
|-------|-------|---------|--------------------------------------|
| `max` | int   | `0`     | Maximum bullets (`0` = unlimited). Adds a `... N more changes` tail when truncating. |

---

### `summary_table`

Top-level resource-count table. Three groupings:

#### `group="module"` — flat per-module rows

**Template:**
```tmpl
{{ summary_table "group" "module" }}
```
→
```
| Module | Resources | Actions |
|--------|-----------|---------|
| privatelink | 1 | 1 create |
| routes | 1 | 1 delete |
| virtual_network | 2 | 2 update |
```

#### `group="module_type"` — two-level (module source type → instances)

Used by `github-step-summary`. Requires `Report.ModuleSources` (populated
from the plan's `configuration.root_module.module_calls`).

**Template:**
```tmpl
{{ summary_table "group" "module_type" }}
```
→
```
| Module Type | Instances | Resources | Actions |
|-------------|-----------|-----------|--------|
| routes | 1 | 1 | ❗ 1 delete |
| virtual_network | 1 | 2 | ⚠️ 2 update |
| privatelink | 1 | 1 | ✅ 1 create |
```

Rows are sorted by severity (destructive first), then alphabetical.

If `.tfreport.yml` provides `module_descriptions_file`, a `Description`
column is inserted automatically.

#### `group="subscription"` — per-report rows (multi-report only)

**Template:**
```tmpl
{{ summary_table "group" "subscription" }}
```
→ (multi-report, pr-body grammar)
```
| Subscription | Resources | Impact | Actions |
|--------------|-----------|--------|---------|
| prod-east | 4 | ❗ high | 1 create, 2 update, 1 delete |
| prod-west | 4 | ❗ high | 1 create, 2 update, 1 delete |
```

The `github-pr-comment` target uses a more compact variant:
```
| Subscription | Impact | Add | Update | Delete | Replace |
```

| Arg          | Type   | Default (target-dependent) | Meaning                       |
|--------------|--------|-----------------------------|-------------------------------|
| `group`      | string | `subscription` when multi; `module_type` for step-summary with sources; else `module` | `module` · `module_type` · `subscription` · `action` · `resource_type` |
| `hide_empty` | bool   | `false`                     | Drop rows with zero non-read resources |
| `max`        | int    | `0`                         | Cap rows; `0` = unlimited. Appends `_... N more …_`. `action` grouping ignores this. |

#### `group="action"` — action breakdown

```tmpl
{{ summary_table "group" "action" }}
```
→
```
| Action | Count | Impact |
|--------|-------|--------|
| ✅ create | 1 | 🟢 low |
| ⚠️ update | 2 | 🟡 medium |
| ❗ delete | 1 | 🔴 high |
| ❗ replace | 0 | — |
| ♻️ read | 0 | — |
```

#### `group="resource_type"` — aggregated across modules

```tmpl
{{ summary_table "group" "resource_type" }}
```
→
```
| Resource Type | Count | Actions |
|---------------|-------|---------|
| route (`azurerm_route`) | 1 | ❗ 1 delete |
| subnet (`azurerm_subnet`) | 2 | ⚠️ 2 update |
```

---

### `module_details`

Per-module section — the "what changed in this module" section used by the
flat targets (`markdown`, `github-pr-body`) and the diff-block variant for
`github-pr-comment`.

#### Per-module resource table (default)

**Template:**
```tmpl
{{ module_details }}
```
→ (markdown target)
```
### ✅ **privatelink** (1 resources: 1 create)

| Resource | Action | Changed Attributes |
|----------|--------|--------------------|
| `azurerm_private_endpoint.web` | ✅ create | location, name, tags |


### ❗ **routes** (1 resources: 1 delete)

| Resource | Action | Changed Attributes |
|----------|--------|--------------------|
| `azurerm_route.legacy` | ❗ delete | address_prefix, name, next_hop_type |
```

On `github-pr-body`, each module is wrapped in a `<details>` collapsible.

#### `per_resource=true` — diff-block (pr-comment default)

**Template:**
```tmpl
{{ module_details "per_resource" "true" }}
```
→
```
<details><summary>✅ **privatelink** (1 resources: 1 create)</summary>

```diff
+ azurerm_private_endpoint: pe-web [location, name, tags]
```

</details>
```

| Arg            | Type | Default (target-dependent) | Meaning                                                   |
|----------------|------|-----------------------------|-----------------------------------------------------------|
| `per_resource` | bool | `true` for `github-pr-comment`, `false` elsewhere | Emit a `\`\`\`diff` block instead of the full resource table |

---

### `instance_detail`

Per-module-instance collapsibles — the workhorse of `github-step-summary`.
Each top-level module instance becomes a `<details>` section with an
optional changed-resources impact table, text-plan (if supplied via
`--text-plan-file`) or synthetic diff fallback.

**Template:**
```tmpl
{{ instance_detail }}
```
→
```
<details><summary>❗ routes — Terraform Plan (1 delete)</summary>

**Changed resources:**

| Resource | Name | Changed | Impact |
|----------|------|---------|--------|
| route | legacy-route | `address_prefix`, `name`, `next_hop_type` | 🔴 high |

```diff
- route: legacy-route
```

</details>
```

**Show only the diff block, skip the impact table:**
```tmpl
{{ instance_detail "show" "diff" }}
```

**Nest sub-modules as their own dropdowns:**
```tmpl
{{ instance_detail "group_submodules" "true" }}
```
(useful when an instance includes many sub-modules and you want per-submodule grouping)

| Arg                | Type   | Default                           | Meaning                                 |
|--------------------|--------|-----------------------------------|-----------------------------------------|
| `show`             | csv    | `impact_table,diff`               | Which inner sections to include         |
| `group_submodules` | bool   | inherits `output.group_submodules` | Nest sub-modules as dropdowns           |
| `max`              | int    | inherits `output.max_resources_in_summary` (fallback 50) | Cap instances shown. `0` = unlimited. |

Instance ordering: descending impact, then alphabetical. Capped by
`output.max_resources_in_summary` (default 50); overflow renders as
`_... N more instances_`.

---

### `text_plan`

Native terraform plan text blocks, budget-aware. Only produces output when
you passed `--text-plan-file plan.txt` to tfreport (or the report was loaded
from a JSON with `text_plan_blocks` populated).

**Template:**
```tmpl
{{ text_plan }}
```
→
````
```diff
  # module.virtual_network.azurerm_subnet.app will be updated in-place
  ~ resource "azurerm_subnet" "app" {
        name                  = "app-subnet"
      ~ tags                  = {
          - "BusinessUnit" = "DTS" -> null
            ...
        }
    }
```
````

**Filter to specific addresses:**
```tmpl
{{ text_plan "addresses" "module.virtual_network.azurerm_subnet.app,module.routes.azurerm_route.legacy" }}
```

**Change the fence:**
```tmpl
{{ text_plan "fence" "hcl" }}
```
produces an ```` ```hcl ```` block with Terraform syntax highlighting
instead of the diff-format green/red.

**Budget behavior:** consumes bytes from `output.step_summary_max_kb` (default
800 KB). Once the budget is exhausted, subsequent calls return empty
string. When a single call would exceed the remaining budget, the block is
truncated at the last newline boundary with a `# ... truncated (output
size limit)` marker appended.

| Arg         | Type   | Default                       | Meaning                                     |
|-------------|--------|-------------------------------|---------------------------------------------|
| `addresses` | csv    | empty (all blocks)            | Restrict to these resource addresses        |
| `fence`     | string | inherits `output.code_format` | `diff` / `hcl` / `plain`                    |

---

### `changed_resources_table`

Per-resource impact table — the `| Resource | Name | Changed | Impact |`
table used inside instance dropdowns. Notes from `.tfreport.yml` resource
or global attribute entries appear inline in the Impact column.

**Template:**
```tmpl
{{ changed_resources_table }}
```
→
```
**Changed resources:**

| Resource | Name | Changed | Impact |
|----------|------|---------|--------|
| subnet | app-subnet | `tags` | 🟡 medium — _Cosmetic only_ |
| route | legacy-route | `address_prefix`, `name` | 🔴 high |
```

**Include create + read rows too:**
```tmpl
{{ changed_resources_table "actions" "all" }}
```

**Just deletes/replaces (the high-impact stuff):**
```tmpl
{{ changed_resources_table "actions" "delete,replace" }}
```

| Arg       | Type   | Default                 | Meaning                                              |
|-----------|--------|-------------------------|------------------------------------------------------|
| `actions` | csv    | `update,delete,replace` | Filter rows by action; `all` is a shorthand for every action |
| `max`     | int    | `0`                     | Cap rows; `0` = unlimited. Appends `_... N more resources_`. |

---

### `deploy_checklist`

Multi-report GitHub-style checkboxes — one task per subscription.

**Template:**
```tmpl
{{ .DeployChecklist }}
```
→ (two subscriptions)
```
### Deploy Checklist
- [ ] **prod-east** (high) — 1 create, 2 update, 1 delete
- [ ] **prod-west** (medium) — 3 update
```

Meaningful only in multi-report mode. Single-report renders a one-item list.

**No arguments.**

---

### `footer`

Credit line and optional data-source read count. Empty when there's nothing
to announce (no reads, markdown target).

**Template:**
```tmpl
{{ .Footer }}
```
→ (on github-pr-body with 3 data-source reads)
```
<sub>♻️ 3 data source reads not shown</sub>
<sub>Generated by tfreport</sub>
```

Markdown target emits only the read-count footnote (no credit line).

**No arguments.**

---

### `risk_histogram`

Impact distribution across every resource in scope.

**Template:**
```tmpl
{{ risk_histogram }}
```
→
```
| Impact | Count | Bar |
|--------|-------|-----|
| 🔴 critical | 0 |  |
| 🔴 high | 1 | █ |
| 🟡 medium | 2 | ██ |
| 🟢 low | 1 | █ |
```

**Inline style** (single line):
```tmpl
{{ risk_histogram "style" "inline" }}
```
→ `🔴 0 · 🔴 1 · 🟡 2 · 🟢 1`

| Arg            | Values                   | Default  |
|----------------|--------------------------|----------|
| `style`        | `bar`, `table`, `inline` | `bar`    |
| `include_none` | `true`, `false`          | `false`  |
| `max_bar`      | int                      | `40`     |

---

### `diff_groups`

Deduplicates resources by change fingerprint. "50 NSGs all changing
`tags` identically" collapses into one row with count 50; genuinely
distinct changes remain individually listed.

**Template:**
```tmpl
{{ diff_groups }}
```
→
```
**Deduplicated changes:**

| Pattern | Count | Sample |
|---------|-------|--------|
| ⚠️ update [tags] | 2 | `module.virtual_network.azurerm_subnet.app` |

_1 resource with unique changes:_

- ❗ `module.routes.azurerm_route.legacy` [address_prefix, name, next_hop_type]
```

| Arg         | Type   | Default                   | Meaning                                                |
|-------------|--------|---------------------------|--------------------------------------------------------|
| `threshold` | int    | `2`                       | Only collapse groups with >= threshold members         |
| `actions`   | csv    | `update,delete,replace`   | Which actions participate (imports/reads usually skip) |

**Fingerprint:** sha1(action + sorted attribute keys + per-attribute
before/after JSON). Slice order matters (see design guide caveat).

---

### `fleet_homogeneity`

Multi-report-only. Answers "are all these reports identical?" When yes:
one unified summary + labels. When no: majority pattern + outlier list.

**Template:**
```tmpl
{{ fleet_homogeneity }}
```

**Homogeneous (default `summary` style):**
```
✅ **Fleet uniform** — all 12 subscriptions show identical changes:

- ✅ New private endpoint: pe-web
- ⚠️ Tags updates across 2 subnets

<sub>Applies to: sub-a, sub-b, sub-c, sub-d, sub-e, sub-f, sub-g, sub-h (+ 4 more)</sub>
```

**Divergent:**
```
⚠️ **Fleet divergent** — 1 of 3 subscriptions differ from the majority pattern.

**Majority (2 subs):** 1 create, 2 update, 1 delete

**Outliers:**
- **sub-c** — 4 create, 6 update, 2 delete
```

| Arg           | Values                          | Default       |
|---------------|---------------------------------|---------------|
| `style`       | `summary`, `banner`, `table`    | `summary`     |
| `fingerprint` | `key_changes`, `action_counts`  | `key_changes` |

Single-report mode: returns empty string.

---

### `glossary`

Opt-in reference section defining tfreport's actions + impacts + imports.
Never included in default templates.

**Template:**
```tmpl
{{ glossary }}
```
→
```
> 💡 **What do these words mean?**
>
> **Actions** (what terraform will do to each resource)
> - ✅ **create** — brand-new resources (safe)
> - ⚠️ **update** — modifying an existing resource in place (usually safe)
> - ❗ **delete** — removing a resource (data loss risk)
> - ❗ **replace** — destroy-then-recreate (brief outage on that resource)
> - ♻️ **read** — data source refresh (no change to infra)
>
> **Impact** (reviewer priority)
> - 🔴 **critical / high** — destructive or force-new
> - 🟡 **medium** — in-place update
> - 🟢 **low** — create or read (additive)
> - ⚪ **none** — no-op or cosmetic (e.g. tags)
```

| Arg       | Values                              | Default             |
|-----------|-------------------------------------|---------------------|
| `include` | csv of `actions`, `impacts`, `imports` | `actions,impacts` |
| `level`   | `beginner`, `intermediate`          | `beginner`          |

---

## Template Helpers

Non-block helpers registered in the engine funcmap — callable directly
from templates.

### `action_count "<action>"`

Aggregated count across all reports in scope. Typo-safe (unknown actions
return 0, no panic).

```tmpl
{{ action_count "delete" }}       {{/* returns int */}}
{{ if gt (action_count "replace") 0 }}⛔ Replacements planned.{{ end }}
```

### `import_count`

Counts resources with `IsImport=true` across all reports in scope.

```tmpl
{{ import_count }} resources being imported
```

### `sample N slice`

Returns the first N elements of any slice. When `len(slice) <= N`, returns
the original slice unchanged.

```tmpl
{{ range $mg := sample 3 .Report.ModuleGroups -}}
- {{ $mg.Name }}
{{ end -}}
```

### `impact_is "wanted" got` / `action_is "wanted" got`

Stringifies both arguments and compares. Removes the `(printf "%s" …)`
wart that plain `{{ eq }}` requires against typed strings.

```tmpl
{{ if impact_is "critical" .Report.MaxImpact }}
> ⛔ Critical-impact plan.
{{ end }}
```

### `action_emoji` / `impact_emoji` / `resource_label`

Data formatters callable inside raw-data loops.

```tmpl
{{ range $rc := $mg.Changes }}
{{ action_emoji $rc.Action }} {{ resource_label $rc }}
{{ end }}
```

---

## Raw-data Escape Hatch

When the blocks don't do what you want, drop into the underlying structs:

| Variable    | Type             | When populated                    |
|-------------|------------------|-----------------------------------|
| `.Target`   | `string`         | Always                            |
| `.Report`   | `*core.Report`   | Single-report mode only           |
| `.Reports`  | `[]*core.Report` | Multi-report mode only            |

**`core.Report` fields:**

```go
Label          string                // subscription label
ModuleGroups   []ModuleGroup         // resources grouped by module path
KeyChanges     []KeyChange           // {Text, Impact} pairs from summarizer
TotalResources int
ActionCounts   map[Action]int        // "create" → 5, "delete" → 2, ...
MaxImpact      Impact                // "critical" | "high" | "medium" | "low" | "none"
ModuleSources  map[string]string     // top-level module call → source URL
TextPlanBlocks map[string]string     // address → native text plan block
DisplayNames   map[string]string     // resource_type → human name
```

**`core.ResourceChange` fields** (iterated via `$mg.Changes`):

```go
Address           string
ModulePath        string
ResourceType      string
ResourceName      string
Action            Action             // "create" | "update" | "delete" | "replace" | "read" | "no-op"
Impact            Impact
IsImport          bool               // true when terraform's `importing` marker is set
DisplayLabel      string             // pre-computed display label
ChangedAttributes []ChangedAttribute // {Key, Description, OldValue, NewValue, Computed}
```

See `internal/core/types.go` for the full schema including `ModuleGroup`
and `ResourceChange`.

**Example — roll your own summary:**

```tmpl
### {{ .Report.TotalResources }} changes across {{ len .Report.ModuleGroups }} modules

{{ range $mg := .Report.ModuleGroups -}}
- **{{ $mg.Name }}** ({{ len $mg.Changes }} resources)
{{ end }}
```
→
```
### 4 changes across 3 modules

- **privatelink** (1 resources)
- **routes** (1 resources)
- **virtual_network** (2 resources)
```

---

## Sprig Functions

The [Sprig](https://masterminds.github.io/sprig/) library is registered.
Roughly 100 helpers are available; useful picks:

| Function             | Purpose                                              |
|----------------------|------------------------------------------------------|
| `default "fallback" .X` | Fallback when `.X` is empty                        |
| `eq`, `ne`, `lt`, `gt`  | Comparisons inside `{{ if }}`                      |
| `upper`, `lower`, `title`, `trim` | String case / whitespace            |
| `join ", " .List`    | Join a list with a separator                          |
| `contains "sub" .Str` | Substring check                                      |
| `printf "%02d" .N`   | Formatted output                                      |
| `now \| date "2006-01-02"` | Current date / formatting                        |

**Example — conditionally add a warning banner:**

```tmpl
{{ if eq .Report.MaxImpact "critical" }}
> ⛔ **This plan contains critical-impact changes. Deploy with care.**
{{ end }}

{{ .Title }}
```

---

## `{{ include "path" }}`

Inlines an external file, sandboxed to the directory of your
`.tfreport.yml`. Absolute paths, `../` traversal, and symlink escapes are
refused.

**Template:**
```tmpl
{{ .Title }}

{{ .KeyChanges }}

---

{{ include "./.github/tfreport-footer.md" }}
```

Assuming `./.github/tfreport-footer.md` contains a legal disclaimer, it's
inlined into every rendered output.

---

## Authoring Workflow

The fastest path from "I want custom output" to "shipped":

**1. Copy a default as your starting point.** The four single-report
defaults are inlined below under [Default Templates](#default-templates-reference).
Paste into a `.tmpl` file anywhere under your repo — templates are
sandboxed to the config-file directory, so `./.tfreport/step-summary.tmpl`
or `./ci/pr-comment.tmpl` are both fine.

**2. Wire it up.** Add one stanza to `.tfreport.yml`:

```yaml
output:
  targets:
    github-step-summary:
      template_file: ./.tfreport/step-summary.tmpl
```

**3. Iterate locally.** Save a plan fixture once:

```bash
terraform show -json plan.out > /tmp/plan.json
```

Then re-render on every edit:

```bash
tfreport --plan-file /tmp/plan.json --target github-step-summary
```

Parse errors surface with line numbers (`template parse: template:
tfreport:7: unexpected EOF`). Runtime errors (like a bad arg to
`summary_table`) surface the block name.

**4. Reach for the block catalog.** Start with zero-arg properties
(`{{ .Title }}`, `{{ .SummaryTable }}`) for 80% of what you want. Reach
for parameterized functions (`{{ summary_table "group" "module" }}`,
`{{ key_changes "max" 5 }}`) when you want something the default doesn't
do. Drop to raw data (`{{ range .Report.ModuleGroups }}`) only when
neither covers your case.

**5. Test against multiple fixtures.** A template that looks great for a
small plan may be noisy on a 200-resource plan. Test against
`testdata/small_plan.json`, `medium_plan.json`, and an empty plan
(`{"format_version":"1.2","resource_changes":[],"configuration":{"root_module":{}}}`)
before shipping.

### Where to put your templates

- **Inline (`template:` in YAML)** — up to ~5 lines. Anything longer
  gets awkward with YAML escaping.
- **External file (`template_file: ./path`)** — anything larger. Path is
  resolved relative to `.tfreport.yml`; absolute paths and `../`
  traversal are refused.
- Convention: we recommend `./.tfreport/<target>.tmpl` so custom templates
  are colocated with the config. But anywhere under the repo is fine.

### Don't start from scratch unless you have to

Most real-world customizations are additions or subtractions from a
default:

| If you want to…                          | Use                                    |
|------------------------------------------|----------------------------------------|
| Hide one section                          | `sections.hide: [footer]`              |
| Show only specific sections               | `sections.show: [title, key_changes]`  |
| Add a line or banner                      | Inline `template:` wrapping `.Title` etc. |
| Change grouping of the summary table      | `{{ summary_table "group" "module" }}` instead of `{{ .SummaryTable }}` |
| Show top-N changes only                   | `{{ key_changes "max" 10 }}`           |
| Inject a legal disclaimer                 | `{{ include "./.github/legal.md" }}`   |
| Fully custom output                       | Write from scratch using `.Report`     |

---

## Configuring a Target

Add an `output.targets.<name>` block to `.tfreport.yml`. Three mutually
exclusive modes:

### Mode A — Section filter (no template knowledge required)

Drop blocks from the default template:

```yaml
output:
  targets:
    github-pr-comment:
      sections:
        hide: [footer]
    markdown:
      sections:
        show: [title, key_changes, footer]
```

- `show` keeps only named sections; order follows the default template
  (show is a filter, not a reorder).
- `hide` keeps everything except the named sections.
- `show` and `hide` are mutually exclusive.
- Unknown block names are silently ignored (forward-compatible with future
  blocks).

### Mode B — Inline template

```yaml
output:
  targets:
    github-pr-body:
      template: |
        {{ .Title }}

        ## Priority changes
        {{ key_changes "max" 10 }}

        {{ .DeployChecklist }}

        {{ include "./.github/approval-notes.md" }}
```

### Mode C — Template from file

```yaml
output:
  targets:
    github-step-summary:
      template_file: ./.tfreport/step-summary.tmpl
```

Path is relative to the config file's directory; same sandbox rules as
`include`.

---

## Worked Examples

### 1. "Just the highlights" PR comment

```yaml
output:
  targets:
    github-pr-comment:
      template: |
        ### {{ .Report.TotalResources }} resource change{{ if ne .Report.TotalResources 1 }}s{{ end }}

        {{ range $kc := .Report.KeyChanges }}- {{ $kc.Text }}
        {{ end }}
```

### 2. Step-summary with a banner for destructive plans

```yaml
output:
  targets:
    github-step-summary:
      template: |
        {{ if eq .Report.MaxImpact "critical" }}
        > ⛔ **This plan replaces or destroys resources. Human review required.**
        {{ end }}

        {{ .Title }}
        {{ .PlanCounts }}

        {{ summary_table "group" "module_type" }}

        {{ instance_detail "show" "impact_table,diff" }}

        {{ .Footer }}
```

### 3. PR body with a deploy checklist + custom intro

```yaml
output:
  targets:
    github-pr-body:
      template: |
        {{ include "./.github/terraform-intro.md" }}

        {{ .CrossSubTable }}

        {{ .DeployChecklist }}

        {{ range $r := .Reports }}
        <details><summary>{{ $r.Label }} — {{ $r.TotalResources }} changes, {{ $r.MaxImpact }} impact</summary>

        {{ range $kc := $r.KeyChanges }}- {{ $kc }}
        {{ end }}
        </details>
        {{ end }}

        {{ .Footer }}
```

### 4. Hide the footer credit on PR comments

```yaml
output:
  targets:
    github-pr-comment:
      sections:
        hide: [footer]
```

### 5. Minimal markdown — title + key changes only

```yaml
output:
  targets:
    markdown:
      sections:
        show: [title, key_changes]
```

---

## Default Templates (Reference)

These are the templates shipped with the binary (from
`internal/formatter/templates/*.tmpl`). Showing them here so you don't have
to navigate the source tree to remember what the defaults look like.

### `markdown.tmpl`

```tmpl
{{/* block: title */}}
{{ .Title }}

{{/* block: plan_counts */}}
**{{ .Report.TotalResources }} resources** — {{ .PlanCounts }}

{{/* block: key_changes */}}
{{ .KeyChanges }}

{{/* block: module_details */}}
## Modules

{{ module_details }}

{{/* block: footer */}}
{{ .Footer }}
```

### `github-pr-body.tmpl`

```tmpl
{{/* block: title */}}
{{ .Title }}

{{/* block: summary_table */}}
{{ .SummaryTable }}

{{/* block: key_changes */}}
{{ .KeyChanges }}

{{/* block: module_details */}}
{{ module_details }}

{{/* block: footer */}}
{{ .Footer }}
```

### `github-pr-comment.tmpl`

```tmpl
{{/* block: title */}}
{{ .Title }}

{{/* block: key_changes */}}
{{ .KeyChanges }}

{{/* block: module_details */}}
{{ module_details }}

{{/* block: footer */}}
{{ .Footer }}
```

### `github-step-summary.tmpl`

```tmpl
{{/* block: title */}}
{{ .Title }}
{{/* block: plan_counts */}}
{{ .PlanCounts }}

{{/* block: summary_table */}}
{{ .SummaryTable }}

---

{{/* block: instance_detail */}}
{{ instance_detail }}

{{/* block: footer */}}
{{ .Footer }}
```

Multi-report variants (`*.multi.tmpl`) exist for every target; they iterate
`{{ range $r := .Reports }}` and use `{{ .CrossSubTable }}` /
`{{ .DeployChecklist }}` at the top.

---

## Template Design Constraints

### Section markers and control flow

The default templates mark each section with `{{/* block: NAME */}}`. These
markers are how `sections.show/hide` identifies sections. When you write
your own template, you can include markers for future readers — they're
rendered as empty strings.

**Constraint:** section markers must not split Go template flow control
(`{{ if }}` / `{{ range }}` / `{{ end }}`) across section boundaries. If
you hide a section, everything between its marker and the next marker is
removed; an unmatched `{{ if }}` would fail to parse.

The default templates follow this rule by relying on blocks returning
empty strings for empty data instead of wrapping sections in `{{ if }}`.

### Block names

Block names in `sections.show` / `sections.hide` must match exactly one of:
`title`, `plan_counts`, `key_changes`, `summary_table`, `module_details`,
`instance_detail`, `deploy_checklist`, `changed_resources_table`,
`text_plan`, `footer`.

Unknown names are silently ignored so configs stay forward-compatible when
new blocks are added.

### `json` target is not customizable

The `json` target is the canonical tfreport interchange format — its
structure is stable so other tools can consume it. You can still filter
what ends up in the report (e.g. via `--changed-only`), but you can't
override how it's serialized.
