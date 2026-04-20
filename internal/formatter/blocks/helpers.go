package blocks

import (
	"fmt"
	"strings"

	"github.com/tfreport/tfreport/internal/core"
)

// actionSummaryLine renders action counts in canonical order: create, update,
// delete, replace, read. Used by multiple blocks (summary_table, module_table,
// title) so centralized here.
func actionSummaryLine(counts map[core.Action]int) string {
	var parts []string
	order := []core.Action{core.ActionCreate, core.ActionUpdate, core.ActionDelete, core.ActionReplace, core.ActionRead}
	for _, a := range order {
		if c, ok := counts[a]; ok && c > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c, a))
		}
	}
	return strings.Join(parts, ", ")
}

// planCountsLine renders the "Plan: N to add, N to change, N to destroy" line
// using terraform's verbs (add/change/destroy/replace/read).
func planCountsLine(counts map[core.Action]int) string {
	var parts []string
	if c := counts[core.ActionCreate]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d to add", c))
	}
	if c := counts[core.ActionUpdate]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d to change", c))
	}
	if c := counts[core.ActionDelete]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d to destroy", c))
	}
	if c := counts[core.ActionReplace]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d to replace", c))
	}
	if c := counts[core.ActionRead]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d to read", c))
	}
	return strings.Join(parts, ", ")
}

// actionBreakdownEmoji renders action counts with emoji prefixes, destructive
// first. Used in table cells for visual priority (step-summary pattern).
func actionBreakdownEmoji(counts map[core.Action]int) string {
	var parts []string
	order := []core.Action{core.ActionReplace, core.ActionDelete, core.ActionUpdate, core.ActionCreate}
	for _, a := range order {
		if c, ok := counts[a]; ok && c > 0 {
			emoji := core.ActionEmoji(a)
			parts = append(parts, fmt.Sprintf("%s %d %s", emoji, c, a))
		}
	}
	return strings.Join(parts, ", ")
}

// actionDiffSymbol returns the diff-block symbol for an action (+/-/!/#).
func actionDiffSymbol(action core.Action) string {
	switch action {
	case core.ActionCreate:
		return "+"
	case core.ActionDelete:
		return "-"
	case core.ActionUpdate, core.ActionReplace:
		return "!"
	case core.ActionRead:
		return "#"
	default:
		return " "
	}
}

// overallEmoji returns the header emoji for a report's max impact.
func overallEmoji(impact core.Impact) string {
	switch impact {
	case core.ImpactCritical, core.ImpactHigh:
		return "❗"
	case core.ImpactMedium:
		return "⚠️"
	default:
		return "✅"
	}
}

// impactCircle returns the colored-circle emoji for an impact level.
func impactCircle(impact core.Impact) string {
	switch impact {
	case core.ImpactCritical, core.ImpactHigh:
		return "🔴"
	case core.ImpactMedium:
		return "🟡"
	case core.ImpactLow:
		return "🟢"
	default:
		return "⚪"
	}
}

// shortAddress strips the module-path prefix from an address for per-module
// rendering.
func shortAddress(address, modulePath string) string {
	if modulePath == "" || modulePath == "(root)" {
		return address
	}
	return strings.TrimPrefix(address, modulePath+".")
}

// displayName returns the human-readable name for a resource type using the
// context's DisplayNames map, falling back to the provider-stripped form
// ("azurerm_subnet" → "subnet").
func displayName(ctx *BlockContext, resourceType string) string {
	if ctx.DisplayNames != nil {
		if n, ok := ctx.DisplayNames[resourceType]; ok {
			return n
		}
	}
	parts := strings.SplitN(resourceType, "_", 2)
	if len(parts) == 2 {
		return strings.ReplaceAll(parts[1], "_", " ")
	}
	return resourceType
}

// resourceLabel formats a resource as "displayType: actualName" when a display
// name is available, or "type.name" otherwise.
func resourceLabel(ctx *BlockContext, rc core.ResourceChange) string {
	typeName := displayName(ctx, rc.ResourceType)
	actual := core.ResourceDisplayLabel(rc)
	if typeName != rc.ResourceType {
		return fmt.Sprintf("%s: %s", typeName, actual)
	}
	return fmt.Sprintf("%s.%s", rc.ResourceType, actual)
}

// formatAttrWithDesc formats an attribute as "key (description)" when the
// attribute carries a description, else just "key".
func formatAttrWithDesc(attr core.ChangedAttribute) string {
	if attr.Description != "" {
		return fmt.Sprintf("%s (%s)", attr.Key, attr.Description)
	}
	return attr.Key
}

// formatAttrsInline joins multiple changed attributes comma-separated.
func formatAttrsInline(attrs []core.ChangedAttribute) string {
	parts := make([]string, len(attrs))
	for i, a := range attrs {
		parts[i] = formatAttrWithDesc(a)
	}
	return strings.Join(parts, ", ")
}

// formatAttrsKeysOnly returns backtick-quoted, comma-separated attribute
// keys (no descriptions). Used in the changed-resources impact table.
func formatAttrsKeysOnly(attrs []core.ChangedAttribute) string {
	keys := core.ChangedAttributeKeys(attrs)
	return "`" + strings.Join(keys, "`, `") + "`"
}

// formatImpactWithNote renders impact as "🔴 high" with an optional " — _note_"
// suffix pulled from the NoteResolver for the first changed attribute that
// has a note.
func formatImpactWithNote(ctx *BlockContext, rc core.ResourceChange) string {
	out := fmt.Sprintf("%s %s", impactCircle(rc.Impact), rc.Impact)
	if ctx.NoteResolver == nil {
		return out
	}
	for _, a := range rc.ChangedAttributes {
		if note := ctx.NoteResolver(rc.ResourceType, a.Key); note != "" {
			out += fmt.Sprintf(" — _%s_", note)
			break
		}
	}
	return out
}

// codeFence returns the opening fence for a text-plan code block based on
// ctx.Output.CodeFormat.
func codeFence(ctx *BlockContext) string {
	switch ctx.Output.CodeFormat {
	case "diff":
		return "```diff"
	case "terraform", "hcl":
		return "```hcl"
	default:
		return "```"
	}
}

// canCollapse reports whether the target uses <details> collapsibles. Markdown
// target renders flat; GitHub targets use collapsibles.
func canCollapse(target string) bool {
	switch target {
	case "github-pr-body", "github-pr-comment", "github-step-summary":
		return true
	default:
		return false
	}
}

// currentReport returns the single report being rendered. In multi-report
// mode it returns the first report (used rarely; most blocks should iterate
// ctx.Reports explicitly).
func currentReport(ctx *BlockContext) *core.Report {
	if ctx.Report != nil {
		return ctx.Report
	}
	if len(ctx.Reports) > 0 {
		return ctx.Reports[0]
	}
	return nil
}

// allReports returns every report the context contains, normalized to a
// single-element slice in single-report mode for uniform iteration.
func allReports(ctx *BlockContext) []*core.Report {
	if len(ctx.Reports) > 0 {
		return ctx.Reports
	}
	if ctx.Report != nil {
		return []*core.Report{ctx.Report}
	}
	return nil
}

// reportLabel returns the report's label or "default".
func reportLabel(r *core.Report) string {
	if r != nil && r.Label != "" {
		return r.Label
	}
	return "default"
}

// totalResources sums TotalResources across all reports in ctx.
func totalResources(ctx *BlockContext) int {
	n := 0
	for _, r := range allReports(ctx) {
		n += r.TotalResources
	}
	return n
}
