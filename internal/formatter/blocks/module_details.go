package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// ModuleDetails renders the per-module section used by the markdown target
// (flat H3 + resource table) and by pr-body/pr-comment (collapsible
// <details> wrapper around the same content).
//
// Args:
//
//	per_resource bool (default false) — when true (pr-comment style), emits
//	    a ```diff block with one line per resource instead of the full
//	    resource table.
type ModuleDetails struct{}

func (ModuleDetails) Name() string { return "module_details" }

func (ModuleDetails) Render(ctx *BlockContext, args map[string]any) (string, error) {
	perResource := ArgBool(args, "per_resource", ctx.Target == "github-pr-comment")

	r := currentReport(ctx)
	if r == nil || len(r.ModuleGroups) == 0 {
		return "", nil
	}

	collapse := canCollapse(ctx.Target)
	var b strings.Builder

	for _, mg := range r.ModuleGroups {
		writeModuleHeader(&b, ctx, mg, collapse)
		if perResource {
			writeModuleDiffBlock(&b, ctx, mg)
		} else {
			writeModuleResourceTable(&b, ctx, mg)
		}
		writeModuleFooter(&b, collapse)
	}
	return strings.TrimRight(b.String(), "\n"), nil
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

func writeModuleResourceTable(b *strings.Builder, ctx *BlockContext, mg core.ModuleGroup) {
	b.WriteString("| Resource | Action | Changed Attributes |\n")
	b.WriteString("|----------|--------|--------------------|\n")
	for _, rc := range mg.Changes {
		attrs := formatAttrsInline(rc.ChangedAttributes)
		if attrs == "" {
			attrs = "—"
		}
		fmt.Fprintf(b, "| `%s` | %s %s | %s |\n",
			shortAddress(rc.Address, mg.Path),
			core.ActionEmoji(rc.Action), rc.Action, attrs)
	}
	b.WriteString("\n")
}

func writeModuleDiffBlock(b *strings.Builder, ctx *BlockContext, mg core.ModuleGroup) {
	b.WriteString("```diff\n")
	for _, rc := range mg.Changes {
		symbol := actionDiffSymbol(rc.Action)
		attrs := ""
		if len(rc.ChangedAttributes) > 0 {
			attrs = fmt.Sprintf(" [%s]", formatAttrsInline(rc.ChangedAttributes))
		}
		label := core.ResourceDisplayLabel(rc)
		fmt.Fprintf(b, "%s %s: %s%s\n", symbol, rc.ResourceType, label, attrs)
	}
	b.WriteString("```\n\n")
}

func init() { defaultRegistry.Register(ModuleDetails{}) }
