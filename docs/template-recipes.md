# Template Recipes

Ready-made templates for common personas and scenarios. Each recipe states
who it's for, the scenario it fits, the template, a sample of what it
produces, and — where applicable — what gap in tfreport made it harder than
it should have been. See
[docs/template-design-guide.md](template-design-guide.md) for the gaps
roadmap.

- [Persona: Senior Terraform engineer — "just the diff, nothing else"](#senior-engineer--just-the-diff-nothing-else)
- [Persona: Systems architect — impact heatmap + blast-radius lens](#architect--impact-heatmap--blast-radius-lens)
- [Persona: Terraform newbie — guided walkthrough](#newbie--guided-walkthrough)
- [Persona: SRE on-call reviewer — approval sheet](#sre-on-call--approval-sheet)
- [Scenario: Greenfield environment deploy (900+ resources, all create)](#greenfield-900-resource-deploy)
- [Scenario: Bulk import (600–900 resources, focus on "what changed")](#bulk-import-600900-resources)
- [Scenario: Surgical NSG / route table change (1–3 resources)](#surgical-nsg--route-change)
- [Scenario: Fleet template bump (same change × N subscriptions)](#fleet-template-bump)

---

## Senior engineer — "just the diff, nothing else"

**Who:** ten-year Terraform engineer who can read `~` and `+` in their
sleep. Wants density. Hates emoji chrome, collapsibles that have to be
clicked, and plain-English summaries.

**Scenario:** everyday review. Any plan size under ~50 resources.

```yaml
output:
  targets:
    github-pr-comment:
      template: |
        ### {{ .Report.TotalResources }} changes · {{ .Report.MaxImpact }}

        {{ changed_resources_table "actions" "all" }}

        {{ text_plan }}
```

**Renders:**

```
### 4 changes · high

**Changed resources:**

| Resource | Name | Changed | Impact |
|----------|------|---------|--------|
| private_endpoint | pe-web | name, tags | 🟢 low |
| route | legacy-route | address_prefix, name, next_hop_type | 🔴 high |
| subnet | app-subnet | tags | 🟡 medium |
| subnet | db-subnet | tags | 🟡 medium |
```

…followed by the native `terraform plan` text (when `--text-plan-file` is
passed). No banner, no key-changes prose, no collapsibles — one table,
one diff.

**Gaps:** none. All existing blocks.

---

## Architect — impact heatmap + blast-radius lens

**Who:** systems architect who only reads PRs when something destructive
is going down. Cares about *shape* of change (how much is create vs
destroy vs replace) more than the specific resources.

**Scenario:** any plan; gives architect a "should I look at this?" at-a-glance.

```yaml
output:
  targets:
    github-pr-body:
      template: |
        # Change shape · {{ .Report.MaxImpact }}

        **{{ .Report.TotalResources }} resources** — {{ .PlanCounts }}

        ## Risk histogram
        {{ risk_histogram }}

        ## Destructive changes only
        {{ changed_resources_table "actions" "delete,replace" }}

        ## Module-type footprint
        {{ summary_table "group" "module_type" }}
```

**Renders:** (against a mixed plan)

```
# Change shape · high

**4 resources** — Plan: 1 to add, 2 to change, 1 to destroy.

## Risk histogram
| Impact | Count | Bar |
|--------|-------|-----|
| 🔴 critical | 0 |  |
| 🔴 high     | 1 | █ |
| 🟡 medium   | 2 | ██ |
| 🟢 low      | 1 | █ |

## Destructive changes only
**Changed resources:**
| Resource | Name | Changed | Impact |
|----------|------|---------|--------|
| route | legacy-route | address_prefix, name, next_hop_type | 🔴 high |
...
```

**Gaps exposed:** none — this was one of the recipes that motivated
`risk_histogram` to ship. The previous 15-line Sprig counter collapsed
into one line.

---

## Newbie — guided walkthrough

**Who:** first-week engineer. Doesn't know what "force-new" means, isn't
sure whether `replace` is destructive. Wants emoji, explanations, and
pointers to the docs.

**Scenario:** any plan, onboarding-friendly.

```yaml
output:
  targets:
    github-pr-comment:
      template: |
        ## 📋 Plan Summary

        This PR is planning to change **{{ .Report.TotalResources }} resources**.
        Overall impact: **{{ .Report.MaxImpact }}**.

        > 💡 **What do these words mean?**
        > - **create** 🟢 — brand-new resources (safe)
        > - **update** 🟡 — modifying an existing resource in place (usually safe)
        > - **delete** ❗ — removing a resource (data loss risk)
        > - **replace** ❗ — destroy-then-recreate (brief outage on that resource)
        > - **read** ♻️ — data source refresh (no change to infra)

        ## ✨ What's changing

        {{ .KeyChanges }}

        {{ banner "if_impact" "critical" "style" "alert" "text" "This plan contains critical-impact changes (replace or force-new). Read the \"Destructive\" section carefully before approving." }}

        ## 🗂️ Resources grouped by module

        {{ summary_table "group" "module" }}

        ## 🔍 Everything that changes

        {{ module_details }}

        ## 📚 Need help?
        - [Terraform plan docs]({{ default "https://developer.hashicorp.com/terraform/cli/commands/plan" "" }})
        - Ask in `#terraform-help` on Slack if anything looks weird.

        {{ .Footer }}
```

**Renders:** a friendly, verbose walkthrough. The critical-impact banner
only appears when warranted (returns empty when `MaxImpact != critical`,
so you can include it unconditionally).

**Gaps exposed:** the `{{ banner }}` block replaced the multi-line
`{{ if eq (printf "%s" …) }}` dance. Teams who also want the inline
glossary can swap the prose for `{{ glossary "level" "beginner" }}`.

---

## SRE on-call — approval sheet

**Who:** reviewer paged at 2 AM. Wants yes/no, green/red. Fast.

**Scenario:** any PR, but with a hard time budget.

```yaml
output:
  targets:
    github-pr-comment:
      template: |
        ## Approval sheet · {{ .Report.MaxImpact }}

        - [ ] I reviewed **{{ action_count "delete" }} deletes**
        - [ ] I reviewed **{{ action_count "replace" }} replaces**
        - [ ] Key changes look expected

        ### 🔴 Destructive (must review)
        {{ changed_resources_table "actions" "delete,replace" }}

        ### ⚠️ Key changes (critical + high impact)
        {{ key_changes "impact" "critical,high" "max" 10 }}

        <details><summary>Full plan ({{ .Report.TotalResources }} resources)</summary>

        {{ module_details }}

        </details>
```

**Renders:** destructive changes up top, time-boxed to critical+high
impact, full plan collapsed until they choose to open it.

**Gaps exposed:** none — this recipe motivated both `action_count` and
the `key_changes impact=…` filter. Both shipped.

---

## Greenfield 900-resource deploy

**Who:** anyone reviewing an environment bootstrap.

**Scenario:** 900+ resources, 100% create. Nobody wants a 900-row table.
Reviewer needs: resource-type breakdown, module-type breakdown,
confirmation nothing destructive snuck in.

```yaml
output:
  targets:
    github-step-summary:
      template: |
        {{ .Title }}
        {{ .PlanCounts }}

        ## 👀 Sanity checks

        - **Zero deletes or replaces expected** — we saw
          **{{ action_count "delete" }} deletes**,
          **{{ action_count "replace" }} replaces**.

        {{ banner "if_action_gt" "delete:0,replace:0" "style" "alert" "text" "Investigate before applying." }}

        ## 🧱 Resource-type breakdown
        {{ summary_table "group" "resource_type" }}

        ## 📦 Module-type distribution
        {{ summary_table "group" "module_type" }}

        ## 🔍 Sample resources (first 5 per module)

        {{ range $mg := sample 3 .Report.ModuleGroups }}
        <details><summary>{{ $mg.Name }} ({{ len $mg.Changes }} resources)</summary>

        {{ range $rc := sample 5 $mg.Changes }}- `{{ $rc.Address }}`
        {{ end }}

        </details>
        {{ end }}

        {{ .Footer }}
```

**Renders:** two useful tables (resource type + module type), a binary
"did anything destructive slip in" check, and sampled resource addresses
instead of the full 900 rows.

**Gaps exposed:** none — `summary_table group="resource_type"`,
`action_count`, and `sample` all shipped to retire the dict/set/index
Sprig loop and the index-guarded range.

---

## Bulk import (600–900 resources)

**Who:** engineer reviewing an import PR where most rows are
`terraform import` shims.

**Scenario:** one-time-ish import of an existing environment. The signal
reviewers want is *what's changing*, not *what's being imported* — imports
are "state attach with no infra change"; changes are real mutations.

```yaml
output:
  targets:
    github-pr-body:
      template: |
        # Import: {{ .Report.TotalResources }} resources

        | Category | Count | Action needed |
        |----------|-------|---------------|
        | ♻️ Imports (state-only) | {{ import_count }} | None — just wires state |
        | 🟡 Real updates | {{ action_count "update" }} | Review each |
        | ❗ Replaces | {{ action_count "replace" }} | ⛔ Review carefully |

        ## What's *actually* changing

        {{ if eq (action_count "update") 0 }}
        ✅ No real updates — pure import PR. Safe to approve after
        skimming the import list.
        {{ else }}
        ### Updates
        {{ changed_resources_table "actions" "update" }}

        ### Replaces
        {{ changed_resources_table "actions" "replace" }}
        {{ end }}

        <details><summary>All imports ({{ import_count }})</summary>

        {{ imports_list }}

        </details>

        {{ .Footer }}
```

**Renders:** a small "category" table at the top, real changes separated
from pure imports, imports collapsed into a dropdown. Now uses the first-
class `IsImport` flag instead of misinterpreting `read` action.

**Gaps exposed:** none. `imports_list` retired the nested
`{{ range }}{{ if $rc.IsImport }}…{{ end }}{{ end }}` hand-roll; the
rest was already block-backed.

---

## Surgical NSG / route change

**Who:** anyone touching a CSV row or a single attribute.

**Scenario:** 1–3 resources updated. Reviewer just wants to see the diff.

```yaml
output:
  targets:
    github-pr-comment:
      sections:
        show: [title, module_details]
```

Alternatively, if you want the diff with nothing else:

```yaml
output:
  targets:
    github-pr-comment:
      template: |
        {{ module_details "per_resource" "true" }}
```

**Renders:**

```
<details><summary>⚠️ **virtual_network** (1 resources: 1 update)</summary>

```diff
! azurerm_subnet: app-subnet [tags]
```

</details>
```

**Gaps exposed:** none. This is the case the default is closest to; the
recipe is mainly about *removing noise*, not adding features.

---

## Fleet template bump

**Who:** reviewer when one template change has been applied to N
subscriptions and the plan is expected to be identical across them.

**Scenario:** NSG rule added in a shared CSV → all 12 subscriptions need
to plan/apply it. If the plan is *not* identical across subs, something
went wrong locally.

```yaml
output:
  targets:
    github-pr-body:
      template: |
        # Fleet plan · {{ len .Reports }} subscriptions

        {{ .CrossSubTable }}

        {{ fleet_homogeneity }}

        {{ .DeployChecklist }}

        {{ .Footer }}
```

**Renders (homogeneous case):** `fleet_homogeneity` renders a single
unified list of key changes once, appended with "<sub>Applies to: …</sub>".

**Renders (divergent case):** an alarm banner, majority action summary,
and a per-outlier line.

**Gaps exposed:** none — `fleet_homogeneity` replaces the 15-line Sprig
cross-report zip-and-compare. `diff_groups` is available as a separate
block for per-resource dedup when that's what you actually want.

---

## Matrix deploy checklist (tickbox summary + details)

**Who:** change-control reviewer who screenshots the PR body summary and
pastes it into a change ticket; also the on-call SRE who wants one-click
navigation from the PR to the workflow run.

**Scenario:** multi-subscription matrix plan (10+ subs). The reviewer
needs: (1) a tickbox line per sub (for the change-control screenshot),
(2) a link to the plan workflow run per sub, (3) the subscription ID
next to the name for post-incident forensics, and (4) the per-sub module
details in a collapsible dropdown for the actual review.

**Prerequisite:** the `prepare` step attaches sub metadata via `custom:`
(available from `@v0.0.5+`):

```yaml
- uses: BlackMesaLTD/tfreport/.github/action/prepare@v0.0.5
  with:
    plan-file: ./subscriptions/${{ matrix.subscription }}/plan.show.json
    text-plan-file: ./subscriptions/${{ matrix.subscription }}/plan.show.txt
    label: ${{ matrix.subscription }}
    config: .tfreport.yml
    custom: |
      sub_id: ${{ matrix.subscription_id }}
      workflow_url: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
```

**The template:**

```yaml
output:
  targets:
    github-pr-body:
      template: |
        <!-- BEGIN_SUB_SUMMARY -->
        {{- range $r := .Reports }}
        {{- $subID       := index $r.Custom "sub_id"       | default "—" -}}
        {{- $workflowURL := index $r.Custom "workflow_url" | default "" -}}
        {{- $label       := $r.Label | default "default" }}
        - [ ] {{ $label }} - `{{ $subID }}` ~ {{ if $workflowURL -}}
          [**Summary:**]({{ $workflowURL }})
        {{- else -}}
          **Summary:**
        {{- end }} `{{ $r.MaxImpact }}`
        {{- end }}
        <!-- END_SUB_SUMMARY -->

        {{ range $r := .Reports }}
        <details>
        <summary><code>{{ $r.Label }}</code> — {{ $r.TotalResources }} resource change{{ if ne $r.TotalResources 1 }}s{{ end }}</summary>

        {{ modules_table "report" $r "columns" "module_type,module,changed_attrs" }}
        </details>

        {{ end -}}
```

**Renders:**

```
<!-- BEGIN_SUB_SUMMARY -->
- [ ] sub-alpha - `00000000-0000-0000-0000-000000000001` ~ [**Summary:**](https://github.com/example-org/example-repo/actions/runs/11111111) `high`
- [ ] sub-beta  - `00000000-0000-0000-0000-000000000002` ~ [**Summary:**](https://github.com/example-org/example-repo/actions/runs/22222222) `medium`
<!-- END_SUB_SUMMARY -->

<details><summary><code>sub-alpha</code> — 4 resource changes</summary>
| Module type | Module | Changed attributes |
| `virtual_network` | `vnet` | `tags` |
…
</details>
```

The tickboxes live OUTSIDE the `<details>` wrapper because GitHub markdown
doesn't render interactive checkboxes inside collapsibles. The `|
default "—"` fallbacks mean the template still renders cleanly when a
report is loaded locally (replaying a saved JSON) with no env context.

**Gaps exposed:** none — `Report.Custom` + the `index … | default`
pattern replaces what was previously a purpose-built shell script that
munged workflow YAML and report JSON together. Every piece of metadata
the template uses came from the prepare step's `custom:` input.

---

## Matrix deploy checklist with state preservation

**Who:** same change-control reviewer as above, but on a multi-day PR
where new commits keep pushing fresh renders and wiping the reviewer's
ticks.

**Pain:** every new commit regenerates the PR body, and the `- [ ]`
line goes back to empty — the reviewer has to re-tick on every push.

**Fix:** swap the raw `- [ ]` for the `preserve` template helper. Ticks
survive re-renders because the reconciler keys off a stable `id` — order
changes and new subscriptions don't lose state.

**The template** (diff from the recipe above):

```yaml
output:
  targets:
    github-pr-body:
      template: |
        <!-- BEGIN_SUB_SUMMARY -->
        {{- range $r := .Reports }}
        {{- $subID       := index $r.Custom "sub_id"       | default "—" -}}
        {{- $workflowURL := index $r.Custom "workflow_url" | default "" -}}
        {{- $label       := $r.Label | default "default" }}
        - {{ preserve (printf "deploy:%s" $subID) "checkbox" }} {{ $label }} - `{{ $subID }}` ~ {{ if $workflowURL -}}
          [**Summary:**]({{ $workflowURL }})
        {{- else -}}
          **Summary:**
        {{- end }} `{{ $r.MaxImpact }}`
        {{- end }}
        <!-- END_SUB_SUMMARY -->
```

Only the checkbox cell lives inside the preserve region — the
workflow-URL tail is generator-owned and refreshes on every render.

**CI wiring** — one extra step per job: fetch the current PR body and
pass it to tfreport via `--previous-body-file`:

```yaml
- name: Fetch prior PR body
  run: |
    python3 scripts/gh_api.py --fetch-body \
      --github-token ${{ secrets.GITHUB_TOKEN }} \
      --output old-body.md

- uses: BlackMesaLTD/tfreport/.github/action@main
  with:
    plan-file: plan.json
    previous-body-file: old-body.md   # ← the new line
    target: github-pr-body
    output-file: new-snippet.md

- uses: BlackMesaLTD/tfreport/.github/action/send@main
  with:
    content-file: new-snippet.md
    pr-body-marker: SUB_SUMMARY
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

**Note — don't confuse the two `preserve`s:**

- `preserve` (template helper) — shown above, wraps a state-preserving
  region. This is the state-preservation primitive.
- `--preserve` (CLI flag) — documented further down in this file,
  opt-in allowlist for preserving raw attribute values on serialised
  reports. Unrelated feature, same word.

See [docs/state-preservation.md](./state-preservation.md) for the full
primitive: all four kinds (`checkbox`, `radio`, `text`, `block`), the
`preserve_begin` / `preserve_end` paired form, the `prior` escape hatch,
and the `output.preserve_strict` config knob.

**Alternative — built-in `deploy_checklist preserve="true"`**:

```yaml
output:
  targets:
    github-pr-body:
      template: |
        {{ .Title }}
        {{ deploy_checklist "preserve" true }}
```

Shorter, but gives up the workflow-URL tail and per-sub details
formatting shown in the recipe above. Use the hand-rolled form when you
need custom grammar; use `deploy_checklist preserve="true"` when you
don't.

---

## Where to go next

All of the above templates fall into one of three zones:

| Zone | What it looks like | When to escalate |
|------|-------------------|------------------|
| 🟢 Declarative | Just block calls + properties. Reads top-to-bottom. | Default. Most teams stay here. |
| 🟡 Sprig-assisted | Blocks + light filter/count loops + Sprig helpers. | When you need conditional prose or small aggregations. |
| 🔴 Power-user | Deep `.Report` iteration, `dict`, `set`, `index`, `add` math. | When a block is missing and you can't wait for it to ship. |

Everything in the "🔴 power-user" zone represents a **gap** we should
consider filling. See [docs/template-design-guide.md](template-design-guide.md)
for the gap roadmap and a heuristic for deciding when to escalate vs wait
for a block.

---

## Resource ID in the PR body summary

**Who:** reviewer who wants to jump from a PR comment straight to an Azure
portal / AWS console resource. Needs the resource ID visible inline,
without clicking through to a step summary.

**Scenario:** surgical change to 1–3 specific resources; reviewer already
knows what's changing and just wants the IDs for investigation.

**Prerequisite:** the `prepare` step opts in the `id` attribute via
`preserve_attributes`. Available from `@v0.1.0+`:

```yaml
- uses: BlackMesaLTD/tfreport/.github/action/prepare@v0.1.0
  with:
    plan-file: ./subscriptions/${{ matrix.subscription }}/plan.show.json
    text-plan-file: ./subscriptions/${{ matrix.subscription }}/plan.show.txt
    label: ${{ matrix.subscription }}
    config: .tfreport.yml
    preserve_attributes: |
      id
      location
```

**The template:**

```yaml
output:
  targets:
    github-pr-body:
      template: |
        {{- range $r := .Reports }}
        ## {{ $r.Label }} — {{ $r.TotalResources }} changes
        {{ range $mg := $r.ModuleGroups -}}{{ range $rc := $mg.Changes }}
        - `{{ $rc.Address }}` · id: `{{ index $rc.Preserved "id" | default "—" }}` · location: `{{ index $rc.Preserved "location" | default "—" }}`
        {{- end }}{{ end }}
        {{- end }}
```

**Renders (example):**

```
## sub-alpha — 2 changes

- `module.vnet.azurerm_subnet.app` · id: `/subscriptions/aaa/…/subnets/app` · location: `uksouth`
- `module.vnet.azurerm_subnet.new` · id: `(known after apply)` · location: `uksouth`
```

On the create row, `id` is computed so the template renders the
`(known after apply)` sentinel automatically. If the resource had a
sensitive attribute on the allowlist (e.g. a provider marks `id` as
sensitive for some secret-holding resource type), that attr would be
absent and the `| default "—"` fallback kicks in cleanly — no secret leak.

**Gaps exposed:** none — `--preserve` with the sensitivity gate is the
purpose-built tool for this recipe. Before it existed, exposing IDs
required an out-of-band `jq` step in the workflow that munged plan JSON
and passed values via `custom:` (one-value-per-subscription, one-attr-per-
resource), which scaled poorly beyond a couple of IDs.

---

## Newer blocks worth knowing

Beyond the blocks the recipes above reference, tfreport ships a handful
that unlocked the last mile of declarative templating. The full reference
is auto-generated in [docs/blocks.md](blocks.md); the ones most worth
calling out:

| Block | What it does | Typical recipe fit |
|-------|--------------|--------------------|
| `{{ per_report "report" $r }}` | Renders one "report card" with target-aware wrapping (H2 on markdown, `<details>` on GitHub). | Multi-report templates — replaces the `{{ range .Reports }}<details>…{{ end }}` hand-roll. |
| `{{ banner if_impact="…" style="alert" text="…" }}` | Conditional callout; emits empty when triggers miss. | Any recipe that wanted a one-line warning/confirmation. |
| `{{ imports_list format="table" }}` | Enumerates `IsImport=true` resources with pluggable columns. | Bulk-import recipe. |
| `{{ attribute_diff addresses="…" format="list" }}` | Per-attribute `key → old → new`. | Surgical-change recipe when `text_plan` is overkill. |
| `{{ submodule_group instance="vnet" }}` | Nested `<details>` per sub-module. | Instance-focused recipes outside `instance_detail`. |
| `{{ count_where "module" "vnet" "impact" "high,medium" }}` | Multi-predicate resource count. | Replaces `action_count` when you need more than one axis. |
| `{{ resources "action" "delete" }}` | Filtered `[]ResourceChange` for `range`. | Any recipe that wants custom iteration without `.Report.ModuleGroups` nesting. |

Most table-producing blocks (`summary_table`, `changed_resources_table`,
`module_details`, `modules_table`, `diff_groups`, `deploy_checklist`,
`risk_histogram`) now accept a `columns` csv for column selection; check
`docs/blocks.md` for each block's valid column IDs.
