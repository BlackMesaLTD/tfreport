package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// SubmoduleGroup renders the sub-modules of a given top-level module
// instance as nested collapsible <details> sections. Extracted from
// instance_detail so templates can compose it directly without opting into
// the full instance_detail wrapper.
//
// Args:
//
//	instance string (required)
//	    Top-level module name (e.g. `vnet`). Matches
//	    `core.TopLevelModuleName(mg.Path)`.
//
//	depth int (default: ctx.Output.SubmoduleDepth, else 1)
//	    Sub-module path segments to keep. `depth=1` collapses all
//	    nested depths into the first level; `depth=2` keeps two levels
//	    joined by ` > `.
//
//	format (diff|list; default "diff")
//	    Inner rendering for each sub-module group:
//	      diff  — ```diff fenced code block (one line per resource).
//	      list  — `- emoji `address` [attrs]` bullets.
//
// Single-instance focus: this block renders one instance's submodules.
// To iterate every instance, wrap in a `range` over
// `.Report.ModuleGroups` extracting `core.TopLevelModuleName` — or use
// the `instance_detail` block, which composes this one under the hood
// when `group_submodules=true`.
type SubmoduleGroup struct{}

func (SubmoduleGroup) Name() string { return "submodule_group" }

func (SubmoduleGroup) Render(ctx *BlockContext, args map[string]any) (string, error) {
	instance := ArgString(args, "instance", "")
	if instance == "" {
		return "", fmt.Errorf("submodule_group: 'instance' arg is required (top-level module name)")
	}
	depth := ArgInt(args, "depth", ctx.Output.SubmoduleDepth)
	if depth <= 0 {
		depth = 1
	}
	format := ArgString(args, "format", "diff")
	switch format {
	case "diff", "list":
	default:
		return "", fmt.Errorf("submodule_group: unknown format %q (valid: diff, list)", format)
	}

	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	// Collect module groups belonging to this instance.
	var instanceGroups []core.ModuleGroup
	for _, mg := range r.ModuleGroups {
		if core.TopLevelModuleName(mg.Path) == instance || mg.Name == instance {
			instanceGroups = append(instanceGroups, mg)
		}
	}
	if len(instanceGroups) == 0 {
		return "", nil
	}

	type subGroup struct {
		name    string
		changes []core.ResourceChange
	}
	subs := map[string]*subGroup{}
	var order []string
	for _, mg := range instanceGroups {
		rel := relativeSubmoduleName(instance, mg.Path, depth)
		sg, ok := subs[rel]
		if !ok {
			sg = &subGroup{name: rel}
			subs[rel] = sg
			order = append(order, rel)
		}
		sg.changes = append(sg.changes, mg.Changes...)
	}

	var b strings.Builder
	for _, n := range order {
		sg := subs[n]
		counts := countInstanceActions(sg.changes)
		summary := formatActionCountsShort(counts)
		fmt.Fprintf(&b, "<details><summary>%s (%s)</summary>\n\n", sg.name, summary)
		switch format {
		case "diff":
			writeSubmoduleDiff(&b, ctx, sg.changes)
		case "list":
			writeSubmoduleList(&b, sg.changes)
		}
		b.WriteString("</details>\n\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// writeSubmoduleDiff emits a synthetic diff code block. Uses the same
// action-symbol grammar as writeSyntheticDiff; text_plan composition
// stays in instance_detail where TextBudget coordination matters.
func writeSubmoduleDiff(b *strings.Builder, ctx *BlockContext, changes []core.ResourceChange) {
	hasContent := false
	for _, rc := range changes {
		if rc.Action == core.ActionRead {
			continue
		}
		if !hasContent {
			b.WriteString("```diff\n")
			hasContent = true
		}
		symbol := actionDiffSymbol(rc.Action)
		label := resourceLabel(ctx, rc)
		switch rc.Action {
		case core.ActionCreate, core.ActionDelete:
			fmt.Fprintf(b, "%s %s\n", symbol, label)
		default:
			attrStr := ""
			if len(rc.ChangedAttributes) > 0 {
				attrStr = fmt.Sprintf(" [%s]", formatAttrsInline(rc.ChangedAttributes))
			}
			fmt.Fprintf(b, "%s %s%s\n", symbol, label, attrStr)
		}
	}
	if hasContent {
		b.WriteString("```\n\n")
	}
}

func writeSubmoduleList(b *strings.Builder, changes []core.ResourceChange) {
	for _, rc := range changes {
		if rc.Action == core.ActionRead {
			continue
		}
		emoji := core.ActionEmoji(rc.Action)
		attrs := ""
		if len(rc.ChangedAttributes) > 0 {
			attrs = fmt.Sprintf(" [%s]", formatAttrsInline(rc.ChangedAttributes))
		}
		fmt.Fprintf(b, "- %s `%s`%s\n", emoji, rc.Address, attrs)
	}
	b.WriteString("\n")
}

// Doc describes submodule_group for cmd/docgen.
func (SubmoduleGroup) Doc() BlockDoc {
	return BlockDoc{
		Name:    "submodule_group",
		Summary: "Nested <details> collapsibles per sub-module of a given top-level module instance. Extracted from instance_detail's internal grouping.",
		Args: []ArgDoc{
			{Name: "instance", Type: "string", Default: "—", Description: "Required. Top-level module name (e.g. `vnet`)."},
			{Name: "depth", Type: "int", Default: "(ctx.Output.SubmoduleDepth, else 1)", Description: "Sub-module path segments to keep; joined with ` > ` for depth>1."},
			{Name: "format", Type: "string", Default: "diff", Description: "Inner rendering for each sub-module group: `diff` (fenced code block) or `list` (bullets)."},
		},
	}
}

func init() { defaultRegistry.Register(SubmoduleGroup{}) }
