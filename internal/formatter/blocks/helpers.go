package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
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

// Valid values for the changed_attrs_display arg / OutputOptions.ChangedAttrsDisplay
// field. Centralized so blocks and tests share one source of truth.
const (
	ChangedAttrsDash  = "dash"  // "—" on create/delete
	ChangedAttrsWordy = "wordy" // "new" / "removed" on create/delete
	ChangedAttrsCount = "count" // "N attrs" on create/delete
	ChangedAttrsList  = "list"  // legacy: always render the keys-list
)

// validChangedAttrsMode reports whether mode is one of the four accepted
// values (or empty, which means "use default"). Returns nil on success and a
// typed error on unknown input matching the `unknown column %q (valid: …)`
// grammar used elsewhere.
func validChangedAttrsMode(blockName, mode string) error {
	switch mode {
	case "", ChangedAttrsDash, ChangedAttrsWordy, ChangedAttrsCount, ChangedAttrsList:
		return nil
	}
	return fmt.Errorf("%s: unknown changed_attrs_display %q (valid: dash, wordy, count, list)", blockName, mode)
}

// renderChangedCell produces the content for a "Changed" / "Changed
// Attributes" cell given the resource's action, its changed attributes,
// and the display mode. Update/replace always delegate to formatter (so
// callers keep their own cell formatting — backticked keys, inline joined
// with descriptions, etc.). Create, delete, read, and no-op honor mode:
//
//   - dash  (default) → "—"
//   - wordy           → "new" for create, "removed" for delete, "—" for read/no-op
//   - count           → "N attrs"
//   - list            → delegate to formatter (legacy full keys-list)
//
// Empty mode is treated as dash. When update/replace formatter output is
// empty (rare: an update with zero changed attrs), falls back to "—" so
// the cell is never blank.
func renderChangedCell(
	action core.Action,
	attrs []core.ChangedAttribute,
	mode string,
	formatter func([]core.ChangedAttribute) string,
) string {
	if mode == "" {
		mode = ChangedAttrsDash
	}
	if action == core.ActionUpdate || action == core.ActionReplace {
		s := formatter(attrs)
		if s == "" {
			return "—"
		}
		return s
	}
	switch mode {
	case ChangedAttrsWordy:
		switch action {
		case core.ActionCreate:
			return "new"
		case core.ActionDelete:
			return "removed"
		default:
			return "—"
		}
	case ChangedAttrsCount:
		return fmt.Sprintf("%d attrs", len(attrs))
	case ChangedAttrsList:
		s := formatter(attrs)
		if s == "" {
			return "—"
		}
		return s
	default: // ChangedAttrsDash
		return "—"
	}
}

// resolveChangedAttrsMode picks the effective display mode for a block call.
// Precedence: per-block arg > ctx.Output.ChangedAttrsDisplay > "dash".
func resolveChangedAttrsMode(ctx *BlockContext, argMode string) string {
	if argMode != "" {
		return argMode
	}
	if ctx != nil && ctx.Output.ChangedAttrsDisplay != "" {
		return ctx.Output.ChangedAttrsDisplay
	}
	return ChangedAttrsDash
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
