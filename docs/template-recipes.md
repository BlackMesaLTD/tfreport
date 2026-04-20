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

        {{ if eq (printf "%s" .Report.MaxImpact) "critical" -}}
        ⛔ **This plan contains critical-impact changes (replace or force-new).**
        Read the "Destructive" section carefully before approving.
        {{- end }}

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
only appears when warranted.

**Gaps exposed:** none — but every newbie template re-invents the
glossary. *Could be offered as `{{ glossary }}` block (opt-in, hidden
behind a `level="beginner"` arg) to save copy-paste.*

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
          {{- if or (gt (action_count "delete") 0) (gt (action_count "replace") 0) }}
          ⛔ **Investigate before applying.**
          {{- else }}
          ✅ Clean greenfield.
          {{- end }}

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

        {{ range $mg := .Report.ModuleGroups }}{{ range $rc := $mg.Changes }}{{ if $rc.IsImport }}- `{{ $rc.Address }}`
        {{ end }}{{ end }}{{ end }}

        </details>

        {{ .Footer }}
```

**Renders:** a small "category" table at the top, real changes separated
from pure imports, imports collapsed into a dropdown. Now uses the first-
class `IsImport` flag instead of misinterpreting `read` action.

**Gaps exposed:** none — `IsImport`, `action_count`, `import_count`, and
action-filtered `changed_resources_table` all shipped. This recipe was
the P0 motivator for the first two.

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
