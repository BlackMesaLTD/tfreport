package blocks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// ModuleDetails renders one section per module group. The section wrapper
// is target-aware (collapsible <details> on GitHub targets, flat H3 on
// markdown) and the inner body is chosen by `format`:
//
//	format=table (default)  — full resource table (columns configurable)
//	format=diff             — fenced ```diff code block, one line per resource
//	format=list             — markdown bullet list, one bullet per resource
//
// Args:
//
//	format (table|diff|list; default target-dependent)
//	    Switches the inner rendering style.
//	    Default: `diff` for github-pr-comment, else `table`.
//	    The legacy `per_resource=true` arg is retained as an alias for
//	    `format=diff` (deprecated; slated for removal one release after
//	    the arg ships).
//
//	columns csv (default "resource,action,changed")
//	    Table-mode only. Valid IDs: resource, address, action, changed,
//	    impact, force_new. Unknown IDs return a typed error.
//
//	actions csv (default "")
//	    Filter: keep only resources whose action is in the set. Empty
//	    means "all actions".
//
//	impact csv (default "")
//	    Filter: keep only resources whose Impact is in the set.
//
//	max int (default 0 = unlimited)
//	    Cap resources per module section. Extra rows truncate with a
//	    per-format marker.
//
//	where string (default "")
//	    HCL predicate evaluated per resource with `self` bound to the
//	    tree node. Composes AND with `actions` and `impact`. A module
//	    section disappears when its last surviving resource is filtered
//	    out. Example:
//
//	        where: self.is_import || contains(["critical","high"], self.impact)
type ModuleDetails struct{}

func (ModuleDetails) Name() string { return "module_details" }

// moduleDetailsColumns is the column registry for format=table. Render
// functions take (ctx, rc, mg) so columns have everything they need.
var moduleDetailsColumns = []string{"resource", "address", "action", "changed", "impact", "force_new"}

var moduleDetailsHeadings = map[string]string{
	"resource":  "Resource",
	"address":   "Address",
	"action":    "Action",
	"changed":   "Changed Attributes",
	"impact":    "Impact",
	"force_new": "Force-new",
}

func (ModuleDetails) Render(ctx *BlockContext, args map[string]any) (string, error) {
	// `format` supersedes the legacy `per_resource` bool. When
	// per_resource=true is passed explicitly, treat it as format=diff for
	// one release (plan decision).
	format := ArgString(args, "format", "")
	if format == "" {
		if ArgBool(args, "per_resource", ctx.Target == "github-pr-comment") {
			format = "diff"
		} else {
			format = "table"
		}
	}
	switch format {
	case "table", "diff", "list":
	default:
		return "", fmt.Errorf("module_details: unknown format %q (valid: table, diff, list)", format)
	}

	cols := defaultCols(ArgCSV(args, "columns"), []string{"resource", "action", "changed"})
	if format == "table" {
		if err := validateColumns("module_details", cols, toSet(moduleDetailsColumns)); err != nil {
			return "", err
		}
	}

	actions := ArgCSV(args, "actions")
	actionSet := map[core.Action]struct{}{}
	for _, a := range actions {
		actionSet[core.Action(a)] = struct{}{}
	}
	impactFilter := parseImpactFilterSet(ArgCSV(args, "impact"))
	max := ArgInt(args, "max", 0)

	whereExpr, err := parseWhereArg(args, "module_details")
	if err != nil {
		return "", err
	}

	changedMode := ArgString(args, "changed_attrs_display", "")
	if err := validChangedAttrsMode("module_details", changedMode); err != nil {
		return "", err
	}
	changedMode = resolveChangedAttrsMode(ctx, changedMode)

	r := currentReport(ctx)
	if r == nil || len(r.ModuleGroups) == 0 {
		return "", nil
	}

	var nodeIdx map[string]*core.Node
	if whereExpr != nil {
		nodeIdx = resourceNodeIndex(ctx, r)
	}

	collapse := canCollapse(ctx.Target)
	var b strings.Builder

	for _, mg := range r.ModuleGroups {
		kept := filterModuleChanges(mg.Changes, actionSet, impactFilter)
		if whereExpr != nil {
			filtered := kept[:0:0]
			for _, rc := range kept {
				keep, err := evalResourceWhere(whereExpr, nodeIdx, rc, "module_details")
				if err != nil {
					return "", err
				}
				if keep {
					filtered = append(filtered, rc)
				}
			}
			kept = filtered
		}
		if len(kept) == 0 {
			continue
		}
		totalKept := len(kept)
		truncated := 0
		if max > 0 && totalKept > max {
			truncated = totalKept - max
			kept = kept[:max]
		}

		writeModuleHeader(&b, ctx, mg, collapse)
		switch format {
		case "table":
			writeModuleResourceTable(&b, ctx, mg, kept, cols, changedMode)
		case "diff":
			writeModuleDiffBlock(&b, ctx, kept, changedMode)
		case "list":
			writeModuleListBlock(&b, ctx, kept, changedMode)
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "\n_... %d more resources_\n\n", truncated)
		}
		writeModuleFooter(&b, collapse)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// filterModuleChanges applies action + impact filters. Empty filters keep
// everything (so default behavior is unchanged).
func filterModuleChanges(changes []core.ResourceChange, actions map[core.Action]struct{}, impacts map[core.Impact]struct{}) []core.ResourceChange {
	if len(actions) == 0 && impacts == nil {
		return changes
	}
	out := make([]core.ResourceChange, 0, len(changes))
	for _, rc := range changes {
		if len(actions) > 0 {
			if _, ok := actions[rc.Action]; !ok {
				continue
			}
		}
		if impacts != nil {
			if _, ok := impacts[rc.Impact]; !ok {
				continue
			}
		}
		out = append(out, rc)
	}
	return out
}

func writeModuleHeader(b *strings.Builder, ctx *BlockContext, mg core.ModuleGroup, collapse bool) {
	impact := core.MaxImpactForGroup(mg)
	emoji := core.ImpactEmoji(impact)

	title := fmt.Sprintf("%s **%s**", emoji, mg.Name)
	if mg.Description != "" {
		title += fmt.Sprintf(" — %s", mg.Description)
	}
	title += fmt.Sprintf(" (%d resources: %s)", len(mg.Changes), actionSummaryLine(mg.ActionCounts))

	if collapse {
		fmt.Fprintf(b, "<details><summary>%s</summary>\n\n", title)
	} else {
		fmt.Fprintf(b, "### %s\n\n", title)
	}
}

func writeModuleFooter(b *strings.Builder, collapse bool) {
	if collapse {
		b.WriteString("</details>\n\n")
	} else {
		b.WriteString("\n")
	}
}

func writeModuleResourceTable(b *strings.Builder, ctx *BlockContext, mg core.ModuleGroup, changes []core.ResourceChange, cols []string, changedMode string) {
	headings := mapSlice(cols, func(id string) string { return moduleDetailsHeadings[id] })
	writeColumnHeader(b, headings)
	for _, rc := range changes {
		b.WriteString("|")
		for _, col := range cols {
			fmt.Fprintf(b, " %s |", renderModuleDetailsCell(ctx, rc, mg, col, changedMode))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func renderModuleDetailsCell(ctx *BlockContext, rc core.ResourceChange, mg core.ModuleGroup, col, changedMode string) string {
	switch col {
	case "resource":
		return "`" + shortAddress(rc.Address, mg.Path) + "`"
	case "address":
		return "`" + rc.Address + "`"
	case "action":
		return fmt.Sprintf("%s %s", core.ActionEmoji(rc.Action), rc.Action)
	case "changed":
		return renderChangedCell(rc.Action, rc.ChangedAttributes, changedMode, formatAttrsInline)
	case "impact":
		return formatImpactWithNote(ctx, rc)
	case "force_new":
		if ctx.ForceNewResolver == nil {
			return "—"
		}
		for _, a := range rc.ChangedAttributes {
			if fn, ok := ctx.ForceNewResolver(rc.ResourceType, a.Key); ok && fn {
				return "✓"
			}
		}
		return "—"
	}
	return ""
}

func writeModuleDiffBlock(b *strings.Builder, ctx *BlockContext, changes []core.ResourceChange, changedMode string) {
	b.WriteString("```diff\n")
	for _, rc := range changes {
		symbol := actionDiffSymbol(rc.Action)
		// Only update/replace (or list mode) keep the [attrs] suffix — aligns
		// with instance_detail's writeSyntheticDiff grammar.
		attrs := ""
		if shouldShowInlineAttrs(rc.Action, changedMode) && len(rc.ChangedAttributes) > 0 {
			attrs = fmt.Sprintf(" [%s]", formatAttrsInline(rc.ChangedAttributes))
		}
		label := core.ResourceDisplayLabel(rc)
		fmt.Fprintf(b, "%s %s: %s%s\n", symbol, rc.ResourceType, label, attrs)
	}
	b.WriteString("```\n\n")
}

func writeModuleListBlock(b *strings.Builder, ctx *BlockContext, changes []core.ResourceChange, changedMode string) {
	for _, rc := range changes {
		emoji := core.ActionEmoji(rc.Action)
		label := core.ResourceDisplayLabel(rc)
		attrs := ""
		if shouldShowInlineAttrs(rc.Action, changedMode) && len(rc.ChangedAttributes) > 0 {
			attrs = fmt.Sprintf(" [%s]", formatAttrsInline(rc.ChangedAttributes))
		}
		fmt.Fprintf(b, "- %s `%s`%s\n", emoji, rc.Address, attrs)
		_ = label // reserved for future "human-readable name" column variant
	}
	b.WriteString("\n")
}

// shouldShowInlineAttrs decides whether the "[attr, attr]" suffix in diff
// and list formats should be appended. Update/replace always carry it;
// create/delete carry it only in "list" mode (legacy). Other actions
// (read, no-op) never carry it.
func shouldShowInlineAttrs(action core.Action, mode string) bool {
	if action == core.ActionUpdate || action == core.ActionReplace {
		return true
	}
	return mode == ChangedAttrsList
}

// Doc describes module_details for cmd/docgen.
func (ModuleDetails) Doc() BlockDoc {
	cols := make([]ColumnDoc, 0, len(moduleDetailsColumns))
	for _, id := range moduleDetailsColumns {
		cols = append(cols, ColumnDoc{
			ID:          id,
			Heading:     moduleDetailsHeadings[id],
			Description: moduleDetailsColumnDescriptions[id],
		})
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].ID < cols[j].ID })

	return BlockDoc{
		Name:    "module_details",
		Summary: "One section per module group (collapsible <details> on GitHub targets, flat H3 on markdown) wrapping a configurable body: resource table (default), diff block, or bullet list.",
		Args: []ArgDoc{
			{Name: "format", Type: "string", Default: "(diff for pr-comment, else table)", Description: "One of `table`, `diff`, `list`."},
			{Name: "per_resource", Type: "bool", Default: "(legacy)", Description: "Deprecated alias: `true` maps to `format=diff`. Remove once callers migrate."},
			{Name: "columns", Type: "csv", Default: "resource,action,changed", Description: "Table-mode columns. Ignored for format=diff/list."},
			{Name: "actions", Type: "csv", Default: "(all)", Description: "Filter: keep only resources whose action is in the set."},
			{Name: "impact", Type: "csv", Default: "(all)", Description: "Filter: keep only resources whose Impact is in the set."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap resources per module section; extras collapse into `… N more resources`."},
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate evaluated per resource (`self` bound to the tree node). Composes AND with `actions` and `impact`. Modules disappear when every resource is filtered out. E.g. `self.is_import || contains([\"critical\",\"high\"], self.impact)`."},
			{Name: "changed_attrs_display", Type: "string", Default: "(cfg.Output.ChangedAttrsDisplay or `dash`)", Description: "How the `changed` column / `[attrs]` suffix renders for create/delete rows: `dash` (—), `wordy` (new/removed), `count` (N attrs), `list` (legacy full keys-list). Update/replace always show keys-list."},
		},
		Columns: cols,
	}
}

var moduleDetailsColumnDescriptions = map[string]string{
	"resource":  "Address relative to the module (module-prefix stripped), backticked.",
	"address":   "Full terraform address, backticked.",
	"action":    "Action emoji + action name.",
	"changed":   "Changed attribute keys + optional descriptions, comma-joined.",
	"impact":    "Impact emoji + level + optional note.",
	"force_new": "`✓` when any changed attribute is preset-marked force_new; `—` otherwise.",
}

func init() { defaultRegistry.Register(ModuleDetails{}) }
