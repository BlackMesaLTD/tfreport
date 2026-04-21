package blocks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// SummaryTable renders the top-level resource count table. Supported groupings:
//
//	group="module_type"   — two-level (module source type → instances). Used by
//	                        step-summary. Requires report.ModuleSources.
//	group="module"        — flat per-module rows. Used by markdown / pr-body.
//	group="subscription"  — per-report rows. Used by pr-body / pr-comment when
//	                        multi-report; produces the cross-sub summary.
//
// Optional args:
//
//	hide_empty bool (default false)  — drop rows with zero non-read resources
//	max        int  (default 0)      — cap the number of rows; 0 = unlimited.
//	                                   The `action` grouping always shows all
//	                                   five actions (max has no effect).
type SummaryTable struct{}

func (SummaryTable) Name() string { return "summary_table" }

func (SummaryTable) Render(ctx *BlockContext, args map[string]any) (string, error) {
	group := ArgString(args, "group", defaultSummaryGroup(ctx))
	hideEmpty := ArgBool(args, "hide_empty", false)
	max := ArgInt(args, "max", 0)

	switch group {
	case "module_type":
		return renderModuleTypeTable(ctx, hideEmpty, max), nil
	case "module":
		return renderModuleTable(ctx, hideEmpty, max), nil
	case "subscription":
		return renderSubscriptionTable(ctx, hideEmpty, max), nil
	case "action":
		return renderActionTable(ctx, hideEmpty), nil
	case "resource_type":
		return renderResourceTypeTable(ctx, hideEmpty, max), nil
	default:
		return "", fmt.Errorf("summary_table: unknown group %q (valid: module, module_type, subscription, action, resource_type)", group)
	}
}

// renderActionTable produces a table with one row per action type.
func renderActionTable(ctx *BlockContext, hideEmpty bool) string {
	r := currentReport(ctx)
	if r == nil {
		return ""
	}

	order := []core.Action{core.ActionCreate, core.ActionUpdate, core.ActionDelete, core.ActionReplace, core.ActionRead}

	var b strings.Builder
	b.WriteString("| Action | Count | Impact |\n")
	b.WriteString("|--------|-------|--------|\n")
	for _, a := range order {
		c := r.ActionCounts[a]
		if hideEmpty && c == 0 {
			continue
		}
		if c == 0 {
			fmt.Fprintf(&b, "| %s %s | 0 | — |\n", core.ActionEmoji(a), a)
			continue
		}
		fmt.Fprintf(&b, "| %s %s | %d | %s |\n",
			core.ActionEmoji(a), a, c, defaultActionImpact(a))
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderResourceTypeTable produces a table with one row per resource type,
// aggregated across all module groups. Uses display names when available.
func renderResourceTypeTable(ctx *BlockContext, hideEmpty bool, max int) string {
	r := currentReport(ctx)
	if r == nil {
		return ""
	}

	type row struct {
		typeName  string
		count     int
		actions   map[core.Action]int
		imports   int
	}
	rows := map[string]*row{}
	var order []string
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			rr, ok := rows[rc.ResourceType]
			if !ok {
				rr = &row{typeName: rc.ResourceType, actions: map[core.Action]int{}}
				rows[rc.ResourceType] = rr
				order = append(order, rc.ResourceType)
			}
			rr.count++
			rr.actions[rc.Action]++
			if rc.IsImport {
				rr.imports++
			}
		}
	}

	sort.Strings(order)

	kept := make([]string, 0, len(order))
	for _, t := range order {
		rr := rows[t]
		if hideEmpty && rr.count == 0 {
			continue
		}
		kept = append(kept, t)
	}
	total := len(kept)
	truncated := false
	if max > 0 && total > max {
		kept = kept[:max]
		truncated = true
	}

	var b strings.Builder
	b.WriteString("| Resource Type | Count | Actions |\n")
	b.WriteString("|---------------|-------|---------|\n")
	for _, t := range kept {
		rr := rows[t]
		name := displayName(ctx, t)
		fmt.Fprintf(&b, "| %s (`%s`) | %d | %s |\n",
			name, t, rr.count, describeActions(rr.actions, rr.imports))
	}
	if truncated {
		fmt.Fprintf(&b, "\n_... %d more resource types_\n", total-max)
	}
	return strings.TrimRight(b.String(), "\n")
}

// describeActions renders the Actions cell for a resource-type row. When all
// tracked changes are imports/no-ops, substitutes a descriptive label
// ("♻️ N import-only") instead of leaving the cell blank.
func describeActions(actions map[core.Action]int, imports int) string {
	if breakdown := actionBreakdownEmoji(actions); breakdown != "" {
		if imports > 0 {
			return fmt.Sprintf("%s · ♻️ %d imported", breakdown, imports)
		}
		return breakdown
	}
	if imports > 0 {
		return fmt.Sprintf("♻️ %d import-only", imports)
	}
	if n := actions[core.ActionNoOp]; n > 0 {
		return fmt.Sprintf("— %d no-op", n)
	}
	return "—"
}

// defaultActionImpact returns the natural impact for an action (used in the
// action summary table). This is informational, not a substitute for per-
// resource impact resolution.
func defaultActionImpact(a core.Action) string {
	switch a {
	case core.ActionReplace:
		return "🔴 critical"
	case core.ActionDelete:
		return "🔴 high"
	case core.ActionUpdate:
		return "🟡 medium"
	case core.ActionCreate, core.ActionRead:
		return "🟢 low"
	default:
		return "—"
	}
}

func defaultSummaryGroup(ctx *BlockContext) string {
	if len(ctx.Reports) > 1 {
		return "subscription"
	}
	if ctx.Target == "github-step-summary" {
		r := currentReport(ctx)
		if r != nil && len(r.ModuleSources) > 0 {
			return "module_type"
		}
	}
	return "module"
}

// renderModuleTypeTable produces the two-level module-type summary
// (step-summary format).
func renderModuleTypeTable(ctx *BlockContext, hideEmpty bool, max int) string {
	r := currentReport(ctx)
	if r == nil {
		return ""
	}

	type row struct {
		typeName     string
		description  string
		instances    map[string]struct{}
		total        int
		read         int
		actionCounts map[core.Action]int
		maxImpact    core.Impact
	}

	rowsByType := make(map[string]*row)
	var order []string

	for _, mg := range r.ModuleGroups {
		topLevel := core.TopLevelModuleName(mg.Path)
		tname := core.ResolveModuleType(topLevel, r.ModuleSources, mg.Name)

		rr, ok := rowsByType[tname]
		if !ok {
			rr = &row{
				typeName:     tname,
				instances:    map[string]struct{}{},
				actionCounts: map[core.Action]int{},
			}
			rowsByType[tname] = rr
			order = append(order, tname)
		}

		if rr.description == "" {
			if desc := ctx.ModuleTypeDescriptions[tname]; desc != "" {
				rr.description = desc
			} else if mg.Description != "" {
				rr.description = mg.Description
			}
		}

		inst := topLevel
		if inst == "" {
			inst = mg.Name
		}
		rr.instances[inst] = struct{}{}

		for a, c := range mg.ActionCounts {
			if a == core.ActionRead {
				rr.read += c
			}
			rr.actionCounts[a] += c
			rr.total += c
		}

		if imp := core.MaxImpactForGroup(mg); core.ImpactSeverity(imp) > core.ImpactSeverity(rr.maxImpact) {
			rr.maxImpact = imp
		}
	}

	var rows []*row
	for _, t := range order {
		rr := rowsByType[t]
		if hideEmpty && rr.total-rr.read == 0 {
			continue
		}
		rows = append(rows, rr)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		si := core.ImpactSeverity(rows[i].maxImpact)
		sj := core.ImpactSeverity(rows[j].maxImpact)
		if si != sj {
			return si > sj
		}
		return rows[i].typeName < rows[j].typeName
	})

	total := len(rows)
	truncated := false
	if max > 0 && total > max {
		rows = rows[:max]
		truncated = true
	}

	hasDesc := false
	for _, rr := range rows {
		if rr.description != "" {
			hasDesc = true
			break
		}
	}

	var b strings.Builder
	if hasDesc {
		b.WriteString("| Module Type | Description | Instances | Resources | Actions |\n")
		b.WriteString("|-------------|-------------|-----------|-----------|--------|\n")
	} else {
		b.WriteString("| Module Type | Instances | Resources | Actions |\n")
		b.WriteString("|-------------|-----------|-----------|--------|\n")
	}
	for _, rr := range rows {
		nonRead := rr.total - rr.read
		actions := actionBreakdownEmoji(rr.actionCounts)
		if hasDesc {
			desc := rr.description
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |\n",
				rr.typeName, desc, len(rr.instances), nonRead, actions)
		} else {
			fmt.Fprintf(&b, "| %s | %d | %d | %s |\n",
				rr.typeName, len(rr.instances), nonRead, actions)
		}
	}
	if truncated {
		fmt.Fprintf(&b, "\n_... %d more module types_\n", total-max)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderModuleTable produces a flat per-module table (pr-body / markdown).
func renderModuleTable(ctx *BlockContext, hideEmpty bool, max int) string {
	r := currentReport(ctx)
	if r == nil {
		return ""
	}

	kept := make([]core.ModuleGroup, 0, len(r.ModuleGroups))
	for _, mg := range r.ModuleGroups {
		if hideEmpty && len(mg.Changes) == 0 {
			continue
		}
		kept = append(kept, mg)
	}
	total := len(kept)
	truncated := false
	if max > 0 && total > max {
		kept = kept[:max]
		truncated = true
	}

	var b strings.Builder
	b.WriteString("| Module | Resources | Actions |\n")
	b.WriteString("|--------|-----------|---------|\n")
	for _, mg := range kept {
		fmt.Fprintf(&b, "| %s | %d | %s |\n",
			mg.Name, len(mg.Changes), actionSummaryLine(mg.ActionCounts))
	}
	if truncated {
		fmt.Fprintf(&b, "\n_... %d more modules_\n", total-max)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderSubscriptionTable produces a per-subscription cross-report table
// (pr-body / pr-comment in multi mode).
func renderSubscriptionTable(ctx *BlockContext, hideEmpty bool, max int) string {
	reports := allReports(ctx)
	if len(reports) == 0 {
		return ""
	}

	kept := make([]*core.Report, 0, len(reports))
	for _, r := range reports {
		if hideEmpty && r.TotalResources == 0 {
			continue
		}
		kept = append(kept, r)
	}
	total := len(kept)
	truncated := false
	if max > 0 && total > max {
		kept = kept[:max]
		truncated = true
	}

	var b strings.Builder
	if ctx.Target == "github-pr-comment" {
		b.WriteString("| Subscription | Impact | Add | Update | Delete | Replace |\n")
		b.WriteString("|--------------|--------|-----|--------|--------|---------|\n")
		for _, r := range kept {
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d |\n",
				reportLabel(r), r.MaxImpact,
				r.ActionCounts[core.ActionCreate],
				r.ActionCounts[core.ActionUpdate],
				r.ActionCounts[core.ActionDelete],
				r.ActionCounts[core.ActionReplace])
		}
	} else {
		b.WriteString("| Subscription | Resources | Impact | Actions |\n")
		b.WriteString("|--------------|-----------|--------|---------|\n")
		for _, r := range kept {
			fmt.Fprintf(&b, "| %s | %d | %s %s | %s |\n",
				reportLabel(r), r.TotalResources,
				core.ImpactEmoji(r.MaxImpact), r.MaxImpact,
				actionSummaryLine(r.ActionCounts))
		}
	}
	if truncated {
		fmt.Fprintf(&b, "\n_... %d more subscriptions_\n", total-max)
	}
	return strings.TrimRight(b.String(), "\n")
}

func init() { defaultRegistry.Register(SummaryTable{}) }
