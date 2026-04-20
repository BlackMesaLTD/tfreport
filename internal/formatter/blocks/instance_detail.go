package blocks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tfreport/tfreport/internal/core"
)

// InstanceDetail renders per-module-instance detail sections: collapsible
// headers wrapping an optional changed-resources table + text-plan (or
// synthetic diff fallback). This is the step-summary workhorse.
//
// Args:
//
//	show csv  — which inner sections to include. Any subset of
//	            "impact_table,diff". Default: "impact_table,diff".
//	group_submodules bool — override ctx.Output.GroupSubmodules for this call.
//	max int — cap instances shown. Default: ctx.Output.MaxResourcesInSummary
//	           (falls back to 50 when unset). 0 means unlimited.
//
// Grammar: nested <details><blockquote> for github-step-summary; flat H3
// sections for markdown; flat <details> for pr-body/pr-comment.
type InstanceDetail struct{}

func (InstanceDetail) Name() string { return "instance_detail" }

func (InstanceDetail) Render(ctx *BlockContext, args map[string]any) (string, error) {
	show := ArgCSV(args, "show")
	if len(show) == 0 {
		show = []string{"impact_table", "diff"}
	}
	showSet := make(map[string]bool, len(show))
	for _, s := range show {
		showSet[s] = true
	}

	groupSubs := ArgBool(args, "group_submodules", ctx.Output.GroupSubmodules)

	instances := collectInstances(ctx)
	if len(instances) == 0 {
		return "", nil
	}

	maxShown := ArgInt(args, "max", ctx.Output.MaxResourcesInSummary)
	if _, explicit := args["max"]; !explicit && maxShown <= 0 {
		maxShown = 50
	}

	var b strings.Builder
	collapse := canCollapse(ctx.Target)

	for i, inst := range instances {
		if maxShown > 0 && i >= maxShown {
			fmt.Fprintf(&b, "\n_... %d more instances_\n", len(instances)-maxShown)
			break
		}

		writeInstanceHeader(&b, ctx, inst, collapse)

		if showSet["impact_table"] {
			trt := ChangedResourcesTable{}
			if out, _ := trt.Render(withChanges(ctx, inst.allChanges), map[string]any{"actions": "update,delete,replace"}); out != "" {
				b.WriteString(out)
				b.WriteString("\n\n")
			}
		}

		if showSet["diff"] {
			if groupSubs && len(inst.groups) > 1 {
				writeSubmoduleGrouped(&b, ctx, inst)
			} else {
				writeInstanceDiff(&b, ctx, inst.allChanges)
			}
		}

		if inst.readCount > 0 {
			fmt.Fprintf(&b, "\n<sub>♻️ %d data source reads not shown</sub>\n", inst.readCount)
		}

		writeInstanceFooter(&b, collapse)
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// instanceData groups a top-level module call's resources across sub-modules.
type instanceData struct {
	name         string
	groups       []core.ModuleGroup
	allChanges   []core.ResourceChange
	totalRes     int
	readCount    int
	actionCounts map[core.Action]int
	maxImpact    core.Impact
}

// collectInstances groups the report's ModuleGroups into top-level instances,
// sorted by descending impact then alphabetically.
func collectInstances(ctx *BlockContext) []*instanceData {
	r := currentReport(ctx)
	if r == nil {
		return nil
	}

	insts := map[string]*instanceData{}
	var order []string
	for _, mg := range r.ModuleGroups {
		name := topLevelModuleName(mg.Path)
		if name == "" {
			name = mg.Name
		}
		d, ok := insts[name]
		if !ok {
			d = &instanceData{name: name, actionCounts: map[core.Action]int{}}
			insts[name] = d
			order = append(order, name)
		}
		d.groups = append(d.groups, mg)
		d.allChanges = append(d.allChanges, mg.Changes...)
		for a, c := range mg.ActionCounts {
			if a == core.ActionRead {
				d.readCount += c
			}
			d.actionCounts[a] += c
			d.totalRes += c
		}
		if imp := core.MaxImpactForGroup(mg); core.ImpactSeverity(imp) > core.ImpactSeverity(d.maxImpact) {
			d.maxImpact = imp
		}
	}

	out := make([]*instanceData, 0, len(order))
	for _, n := range order {
		out = append(out, insts[n])
	}
	sort.SliceStable(out, func(i, j int) bool {
		si := core.ImpactSeverity(out[i].maxImpact)
		sj := core.ImpactSeverity(out[j].maxImpact)
		if si != sj {
			return si > sj
		}
		return out[i].name < out[j].name
	})
	return out
}

func writeInstanceHeader(b *strings.Builder, ctx *BlockContext, inst *instanceData, collapse bool) {
	emoji := core.ImpactEmoji(inst.maxImpact)
	if emoji == "" {
		emoji = "✅"
	}

	counts := countInstanceActions(inst.allChanges)
	summary := formatActionCountsShort(counts)
	nonRead := inst.totalRes - inst.readCount

	if collapse {
		fmt.Fprintf(b, "<details><summary>%s %s — Terraform Plan (%s)</summary>\n\n", emoji, inst.name, summary)
	} else {
		fmt.Fprintf(b, "### %s %s (%d resources)\n\n", emoji, inst.name, nonRead)
	}
}

func writeInstanceFooter(b *strings.Builder, collapse bool) {
	if collapse {
		b.WriteString("</details>\n\n")
	} else {
		b.WriteString("\n")
	}
}

// writeInstanceDiff renders the text-plan block (or synthetic fallback) for
// a set of changes.
func writeInstanceDiff(b *strings.Builder, ctx *BlockContext, changes []core.ResourceChange) {
	addrs := make([]string, 0, len(changes))
	for _, c := range changes {
		addrs = append(addrs, c.Address)
	}
	scoped := withChanges(ctx, changes)

	tp := TextPlan{}
	// Limit to just these addresses.
	r := currentReport(scoped)
	var filter []string
	if r != nil {
		for _, a := range addrs {
			if _, ok := r.TextPlanBlocks[a]; ok {
				filter = append(filter, a)
			}
		}
	}

	var text string
	if len(filter) > 0 {
		// Temporarily shrink the report to just these addresses by filtering
		// via args.
		csv := strings.Join(filter, ",")
		out, err := tp.Render(scoped, map[string]any{"addresses": csv})
		if err == nil {
			text = out
		}
	}

	if text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
		return
	}

	// Fallback: synthetic diff block.
	writeSyntheticDiff(b, ctx, changes)
}

// writeSyntheticDiff produces a ```diff code block with one line per change.
func writeSyntheticDiff(b *strings.Builder, ctx *BlockContext, changes []core.ResourceChange) {
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

// writeSubmoduleGrouped renders each sub-module as a nested dropdown.
func writeSubmoduleGrouped(b *strings.Builder, ctx *BlockContext, inst *instanceData) {
	depth := ctx.Output.SubmoduleDepth
	if depth <= 0 {
		depth = 1
	}

	type subGroup struct {
		name    string
		changes []core.ResourceChange
	}
	subs := map[string]*subGroup{}
	var order []string
	for _, mg := range inst.groups {
		rel := relativeSubmoduleName(inst.name, mg.Path, depth)
		sg, ok := subs[rel]
		if !ok {
			sg = &subGroup{name: rel}
			subs[rel] = sg
			order = append(order, rel)
		}
		sg.changes = append(sg.changes, mg.Changes...)
	}

	for _, n := range order {
		sg := subs[n]
		counts := countInstanceActions(sg.changes)
		summary := formatActionCountsShort(counts)
		fmt.Fprintf(b, "<details><summary>%s (%s)</summary>\n\n", sg.name, summary)
		writeInstanceDiff(b, ctx, sg.changes)
		b.WriteString("</details>\n\n")
	}
}

// relativeSubmoduleName extracts the sub-module path relative to an instance,
// truncated to `depth`.
func relativeSubmoduleName(instName, groupPath string, depth int) string {
	prefix := "module." + instName + "."
	rest := groupPath
	switch {
	case strings.HasPrefix(groupPath, prefix):
		rest = groupPath[len(prefix):]
	case strings.HasPrefix(groupPath, "module."+instName+"["):
		bracketPrefix := "module." + instName + "["
		idx := strings.Index(groupPath[len(bracketPrefix):], "]")
		if idx >= 0 {
			after := len(bracketPrefix) + idx + 1
			if after < len(groupPath) && groupPath[after] == '.' {
				rest = groupPath[after+1:]
			} else {
				return "(root)"
			}
		}
	default:
		return "(root)"
	}

	var segments []string
	for rest != "" {
		if !strings.HasPrefix(rest, "module.") {
			break
		}
		rest = rest[len("module."):]
		next := strings.Index(rest, ".module.")
		if next >= 0 {
			segments = append(segments, rest[:next])
			rest = rest[next+1:]
		} else {
			segments = append(segments, rest)
			rest = ""
		}
	}
	if len(segments) == 0 {
		return "(root)"
	}
	if depth > 0 && len(segments) > depth {
		segments = segments[:depth]
	}
	return strings.Join(segments, " > ")
}

type actionTally struct {
	creates, updates, deletes, replaces int
}

func countInstanceActions(changes []core.ResourceChange) actionTally {
	var t actionTally
	for _, c := range changes {
		switch c.Action {
		case core.ActionCreate:
			t.creates++
		case core.ActionUpdate:
			t.updates++
		case core.ActionDelete:
			t.deletes++
		case core.ActionReplace:
			t.replaces++
		}
	}
	return t
}

func formatActionCountsShort(t actionTally) string {
	var parts []string
	if t.replaces > 0 {
		parts = append(parts, fmt.Sprintf("%d replace", t.replaces))
	}
	if t.deletes > 0 {
		parts = append(parts, fmt.Sprintf("%d delete", t.deletes))
	}
	if t.updates > 0 {
		parts = append(parts, fmt.Sprintf("%d update", t.updates))
	}
	if t.creates > 0 {
		parts = append(parts, fmt.Sprintf("%d create", t.creates))
	}
	return strings.Join(parts, ", ")
}

// withChanges returns a shallow-copy ctx with Report replaced by a synthetic
// single-group report containing only the supplied changes. Used so
// downstream blocks (ChangedResourcesTable, TextPlan) see a scoped view.
func withChanges(ctx *BlockContext, changes []core.ResourceChange) *BlockContext {
	r := currentReport(ctx)
	if r == nil {
		return ctx
	}
	addrSet := make(map[string]bool, len(changes))
	for _, c := range changes {
		addrSet[c.Address] = true
	}

	filtered := make([]core.ModuleGroup, 0, len(r.ModuleGroups))
	for _, mg := range r.ModuleGroups {
		var kept []core.ResourceChange
		for _, c := range mg.Changes {
			if addrSet[c.Address] {
				kept = append(kept, c)
			}
		}
		if len(kept) > 0 {
			cp := mg
			cp.Changes = kept
			filtered = append(filtered, cp)
		}
	}
	newR := *r
	newR.ModuleGroups = filtered

	cp := *ctx
	cp.Report = &newR
	cp.Reports = nil
	return &cp
}

func init() { defaultRegistry.Register(InstanceDetail{}) }
