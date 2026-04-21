# Template Design Guide

Template design in tfreport is a "run it 1,000 times, get 1,000 different
results" activity. There's no objectively-correct template for a given
plan — the right output depends on *who's reading* and *why they're
reading*. This guide is the methodology for doing this well, and the
running roadmap of gaps discovered along the way.

## Why template design is iterative

The same plan JSON feeds five personas (on-call SRE, first-week engineer,
architect, power user, compliance reviewer) and a dozen scenarios
(greenfield, import, surgical change, fleet bump, drift detection).
Each combination wants a different slice of the same data.

Baking a single "good" default that works for everyone is a losing game:
it ends up mediocre for all five personas. What we can do instead is
ship:

1. **Sensible defaults** for the most common cases (single-report review).
2. **A vocabulary of blocks** that covers 80% of the variations
   declaratively.
3. **An escape hatch** into raw data + Sprig for the remaining 20%.
4. **A curated recipe library** ([docs/template-recipes.md](template-recipes.md))
   that teams can fork, adapt, and contribute back to.

Treat template design like code. Version it. Review it. Iterate on it
when workflows change.

---

## The five-step design loop

Use this loop any time a team asks "can we make the PR comment show X?"

### Step 1 — Name the reader and the task

Write down, literally:

> *"[Persona] is reading this to decide [task] in [time budget]."*

Examples:

- "On-call SRE is reading this to decide 'safe to merge' in < 90 seconds."
- "Architect is reading this to decide 'does this change network
  segmentation' in 2-3 minutes, once a week."
- "First-week engineer is reading this to understand what the change is
  doing, in 10-15 minutes, with Slack open."

If you can't write this sentence, stop. You're designing for nobody.

### Step 2 — Enumerate the questions that reader has

Concrete questions, in priority order. For the SRE:

1. Will anything be destroyed or replaced?
2. Are the destructive changes expected (from the PR title/body)?
3. Is there a force-new attribute change I might have missed?
4. How many subscriptions are affected?

For the architect:

1. Does this cross an environment boundary?
2. Does the blast radius touch anything customer-facing?
3. Are any modules being version-bumped implicitly?

### Step 3 — Map questions to blocks

For each question, pick the block (or blocks) that answer it:

| Question                                      | Block                                      |
|-----------------------------------------------|--------------------------------------------|
| Will anything be destroyed/replaced?          | `changed_resources_table "actions" "delete,replace"` |
| What's the overall risk shape?                | `risk_histogram`                            |
| Which modules are touched?                    | `summary_table "group" "module_type"`       |
| What's the natural-language summary?          | `key_changes` (filter by impact: `key_changes "impact" "critical,high"`) |
| What does the raw terraform diff look like?   | `text_plan`                                 |
| How many of each action?                      | `action_count "delete"` (preferred) or `.Report.ActionCounts.<action>` (raw-map access) |
| Are multiple subscriptions uniform?           | `fleet_homogeneity`                         |
| Are many resources changing identically?      | `diff_groups`                               |

If a question keeps sending you to "Sprig loop over raw data," that's
**evidence of a gap** — file it in the roadmap below.

### Step 4 — Write it, render it, iterate

Save a plan fixture once:

```bash
terraform show -json plan.out > /tmp/plan.json
```

Then edit `.tfreport/your-template.tmpl` and re-render:

```bash
tfreport --plan-file /tmp/plan.json --target github-pr-comment
```

Test against three sizes (small, medium, empty). A template that's
beautiful for 4 resources may be unreadable for 900 — and vice versa.

### Step 5 — Commit it, review it, contribute the recipe

Commit the template into the repo where the tfreport config lives. Treat
template changes like code changes: PR, review, CI. If the template is
broadly applicable, add it to
[docs/template-recipes.md](template-recipes.md) with the persona and
scenario annotations.

---

## Decision flowchart: declarative, Sprig-assisted, or power-user?

Three zones of template complexity:

```
┌───────────────────────────────────────────────────────────────┐
│ 🟢 Declarative zone                                           │
│   Only block calls and property references.                   │
│   Reads top-to-bottom; no control flow, no aggregations.      │
│                                                               │
│   {{ .Title }}                                                │
│   {{ summary_table "group" "module" }}                        │
│   {{ key_changes "max" 10 }}                                  │
└───────────────────────────────────────────────────────────────┘
                          │
                          │  "I need a conditional banner"
                          │  "I want to count something"
                          ▼
┌───────────────────────────────────────────────────────────────┐
│ 🟡 Sprig-assisted zone                                        │
│   Blocks + light {{ if }} / {{ range }} over Report           │
│   properties + Sprig helpers (default, eq, add, repeat, len). │
│                                                               │
│   {{ if eq .Report.MaxImpact "critical" }}                    │
│   > ⛔ Critical-impact plan — review carefully.               │
│   {{ end }}                                                   │
└───────────────────────────────────────────────────────────────┘
                          │
                          │  "I'm building my own summary from scratch"
                          │  "I need maps, dicts, multi-pass aggregation"
                          ▼
┌───────────────────────────────────────────────────────────────┐
│ 🔴 Power-user zone                                            │
│   Deep .Report iteration, dict/set/index, multi-loop          │
│   aggregations. Reads like small programs.                    │
│                                                               │
│   {{- $byType := dict -}}                                     │
│   {{- range $mg := .Report.ModuleGroups -}}                   │
│     {{- range $rc := $mg.Changes -}}                          │
│       {{- $k := $rc.ResourceType -}}                          │
│       {{- $_ := set $byType $k (add (default 0 (index $byType │
│             $k)) 1) -}}                                       │
│     {{- end -}}                                               │
│   {{- end }}                                                  │
└───────────────────────────────────────────────────────────────┘
```

**When to stay in 🟢 zone:** you're picking and ordering existing blocks.
No aggregation, no conditional prose. ~80% of real-world templates live
here.

**When to step up to 🟡 zone:** you need one-off conditional output
(warning banners, "if N > 0 show this"). The aggregation is shallow
(count a thing, compare a thing). Code review passes a 30-second smell
test.

**When to drop to 🔴 zone:** you're doing what a block *should* do but
can't. Every time this happens, **file a gap**. The goal is for the power-
user zone to be a transient hack, not a permanent pattern — each
well-motivated 🔴 recipe should eventually promote a 🟢 block.

### Why not just make everything 🟢?

Because the space of "things every team might want" is infinite, and
baking every variation into a block makes the block catalog unnavigable.
Pareto analysis:

- **10 blocks cover ~80% of needs.** We have those.
- **The next 10 blocks cover ~15%.** Worth shipping, carefully scoped.
  These are the named gaps below.
- **The remaining ~5% is bespoke per team.** Not a good ROI to promote to
  blocks; template-level Sprig is the right answer.

The risk isn't "missing power." It's "drowning users in block choices."
Every block we ship needs to carry its weight in multiple recipes.

---

## Gap roadmap (discovered via recipe design)

Gaps identified while designing the templates in
[docs/template-recipes.md](template-recipes.md). P0–P2 rows have all
shipped; P3 rows remain explicitly deferred.

### Shipped

| Proposal | Shipped as | Notes |
|----------|-----------|-------|
| ~~`ActionImport`~~ → `ResourceChange.IsImport` bool | `core.ResourceChange.IsImport` | Chose bool over enum because terraform plan JSON models `importing` as an orthogonal modifier on the `actions` list. A `create+import` is a real combination that a single enum would collapse. |
| `action_count "<action>"` template helper | `{{ action_count "delete" }}` | Aggregates across all reports. `{{ import_count }}` is a companion for imports. |
| `summary_table "group" "action"` + `"resource_type"` | `{{ summary_table "group" "action" }}`, `{{ summary_table "group" "resource_type" }}` | Both groupings live alongside the existing `module` / `module_type` / `subscription` options. |
| `risk_histogram` block | `{{ risk_histogram }}` | Three styles: `bar` (default), `table`, `inline`. `max_bar` caps bar length. |
| `diff_groups` block | `{{ diff_groups }}` | Fingerprint = sha1(action + sorted attr keys + before/after JSON). Collapses above `threshold` (default 2). Slice-order sensitive (see below). |
| `fleet_homogeneity` block | `{{ fleet_homogeneity }}` | 3 styles; fingerprint strategy chooseable (`key_changes` default, `action_counts`). Returns empty in single-report mode. |
| `key_changes "impact"` filter | `{{ key_changes "impact" "critical,high" }}` | Required restructuring `Report.KeyChanges` from `[]string` to `[]KeyChange{Text, Impact}`. |
| `sample` template helper | `{{ sample 5 .Slice }}` | Reflection-based, works with any `[]T`. |
| `impact_is` / `action_is` predicates | `{{ if impact_is "critical" .MaxImpact }}…{{ end }}` | Stringifies both args; avoids the `(printf "%s" …)` wart. |
| `glossary` block | `{{ glossary }}` | Opt-in only; never in defaults. Args: `include` csv (`actions`, `impacts`, `imports`), `level` (`beginner`, `intermediate`). |
| `modules_table` block with `columns` csv | `{{ modules_table "report" $r "columns" "module_type,module,changed_attrs" }}` | The prototype for pluggable-column tables. 8 supported column IDs; per-column render funcs isolated via moduleColumns map. |
| `per_report` block (GAP-A) | `{{ per_report "report" $r "show" "key_changes" }}` | Declarative replacement for hand-rolled `{{ range .Reports }}<details>…{{ end }}` loops in *.multi.tmpl. Target-aware grammar (markdown H2 vs. GitHub `<details>`). Retired the biggest freestyle-markdown surface in tfreport. |
| `columns` csv + typed-error validation on 5 tables (GAP-B/C/D/H) | `summary_table`, `changed_resources_table`, `module_details`, `diff_groups`, `deploy_checklist`, `risk_histogram` all accept `columns` csv. | Shared validation scaffolding in `blocks/columns.go`. Defaults preserve pre-refactor output; unknown IDs return `unknown column "X" (valid: …)`. |
| Multi-axis filters on `changed_resources_table` (GAP-C) | `impact`, `modules`, `module_types`, `resource_types`, `is_import` csv filters. | Combined with existing `actions` + `max`. Case-insensitive matching on module filters. |
| `module_details` `format` + filters (GAP-D) | `format=table\|diff\|list`; `actions`, `impact`, `max` filters. | `per_resource=true` kept as deprecated alias for `format=diff` for one release. |
| `imports_list` block (GAP-F) | `{{ imports_list format="table" columns="address,module" }}` | Retires the `{{ range $mg := .Report.ModuleGroups }}{{ range $rc }}{{ if $rc.IsImport }}…{{ end }}{{ end }}{{ end }}` hand-roll in the bulk-import recipe. |
| `banner` block (GAP-G) | `{{ banner if_impact="critical,high" style="alert" text="…" }}` | OR semantics across triggers (`if_impact` csv, `if_action_gt="delete:0,replace:0"`). No-triggers = always on. Returns empty when no match \u2014 safe to include unconditionally. |
| `attribute_diff` block (GAP-I) | `{{ attribute_diff addresses="…" format="list" }}` | Per-attribute key/old/new rendering in table, list, or inline form. Handles computed (`(known after apply)`) and truncation. |
| `submodule_group` block (GAP-E) | `{{ submodule_group instance="vnet" depth=2 format="diff" }}` | Extracted from instance_detail's internal `writeSubmoduleGrouped`. Standalone for recipes that want nested sub-module dropdowns without the whole instance_detail wrapper. |
| `count_where` / `resources` helpers (GAP-J) | `{{ count_where "module" "vnet" "impact" "high,medium" }}`; `{{ range resources "action" "delete" }}…{{ end }}` | Predicate-based counting + filtered iteration. Multi-predicate AND; csv values on action/impact/module* use OR. Generalization of `action_count` / `import_count`. |
| `Block.Doc()` interface + `cmd/docgen` | `make docs` regenerates `docs/blocks.md` from registry. | Structured metadata per block (args, columns, examples). CI auto-commits regenerated docs on PR via `.github/workflows/docs.yml`. Drift-proof reference. |
| `Report.Custom` pass-through metadata | `--custom key=value` CLI flag; `{{ $r.Custom.<key> }}` in templates. | User-supplied string/string map attached at prepare time, survives JSON round-trip. Closes the gap of "I need subscription ID / workflow URL / owner name in the template but those aren't in the plan." Flat string/string by design — nested structures encoded as JSON strings if needed. |
| Self-contained composite actions | No composite action under `.github/action/` references any sibling composite. | Shared binary-install / custom-parse / send logic lives in `scripts/`. Eliminates the release-time coordination burden of bumping internal `@vX.Y.Z` refs in lockstep — composite `uses:` can't use expressions, so we stopped using sibling composites entirely. |
| Sensitive-value masking (`ChangedAttribute.Sensitive`) | Values flagged by terraform's `before_sensitive`/`after_sensitive` are replaced with `(sensitive)` sentinel inside `Diff()` — before any formatter sees them. | Closed existing P0 TODO. Belt-and-braces: even downstream code that forgets to check `.Sensitive` sees the sentinel. Test fixtures use `LEAK_CANARY_*` sentinels to prove values never leak in JSON, stderr, or Diff output. |
| Opt-in attribute preservation (`$rc.Preserved.<path>`) | `--preserve <path>` (repeatable); `preserve_attributes:` action input on the 3 report-producing composites; `output.preserve_attributes` config. Dotted paths walk nested maps. | Lets templates display specific attrs (`id`, `location`, `tags.env`) without exposing Before/After raw. Sensitive-gated: sensitive attrs are absent (not sentinelled), with a stderr warning naming path+address but NEVER value. Computed values serialize as `(known after apply)`. |

### Explicitly deferred

| Proposal | Rationale |
|----------|-----------|
| Per-resource "why this impact?" trace (P3) | Chains through three resolvers; user-facing value unclear at this cost. Impact resolution order is documented in `docs/configuration.md` under *Impact Resolution* instead. |
| Cost / capacity estimator hook (P3) | Out of scope for plan JSON. Belongs in a separate `infracost-adapter` post-processor that consumes tfreport's JSON target. |

### Slice-order caveat (`diff_groups`)

The fingerprint preserves slice order: `tags=[a,b,c]` and `tags=[b,a,c]`
produce different fingerprints even if semantically equivalent. This
matches terraform's own equality semantics (slice order matters to
terraform). Revisit if users report false negatives.

---

## How this guide stays alive

The gap table is the most important artifact. Every new recipe should
update it:

1. When you add a recipe to [docs/template-recipes.md](template-recipes.md),
   and any block or helper call made you reach into raw data or Sprig
   aggregation, note it in the recipe's "Gaps exposed" section.
2. If the gap is already in the roadmap, bump its priority count.
3. If it's new, add a P2 row with the recipe as the first citation.
4. When a block lands, remove the row and update the recipe to use it.

This is how we stay honest about "declarative vs power-user mode" — the
roadmap forces us to either promote frequent power-user patterns to
blocks or to explicitly decide they'll stay as templates.

---

## See also

- [docs/output-templates.md](output-templates.md) — the block reference,
  default templates, and config syntax.
- [docs/template-recipes.md](template-recipes.md) — the ready-made
  template library.
- [docs/configuration.md](configuration.md) — `.tfreport.yml` schema.
