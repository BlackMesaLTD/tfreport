package blocks

import (
	"fmt"
	"strings"

	"github.com/tfreport/tfreport/internal/core"
)

// ChangedResourcesTable renders the per-resource impact table used inside
// step-summary instance dropdowns:
//
//	| Resource | Name | Changed | Impact |
//	|----------|------|---------|--------|
//
// Args:
//
//	actions csv   — filter by action (default: "update,delete,replace");
//	                "all" includes create and read.
//	max int       — cap number of rows (default: 0 = unlimited). When the
//	                cap is hit, a "_... N more_" marker row is appended.
type ChangedResourcesTable struct{}

func (ChangedResourcesTable) Name() string { return "changed_resources_table" }

func (ChangedResourcesTable) Render(ctx *BlockContext, args map[string]any) (string, error) {
	actionsArg := ArgString(args, "actions", "update,delete,replace")
	wanted := parseActionFilter(actionsArg)
	max := ArgInt(args, "max", 0)

	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	var rows []core.ResourceChange
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			if _, ok := wanted[rc.Action]; !ok {
				continue
			}
			rows = append(rows, rc)
		}
	}
	if len(rows) == 0 {
		return "", nil
	}

	total := len(rows)
	truncated := false
	if max > 0 && total > max {
		rows = rows[:max]
		truncated = true
	}

	var b strings.Builder
	b.WriteString("**Changed resources:**\n\n")
	b.WriteString("| Resource | Name | Changed | Impact |\n")
	b.WriteString("|----------|------|---------|--------|\n")
	for _, rc := range rows {
		typeName := displayName(ctx, rc.ResourceType)
		name := core.ResourceDisplayLabel(rc)
		changed := formatAttrsKeysOnly(rc.ChangedAttributes)
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			typeName, name, changed, formatImpactWithNote(ctx, rc))
	}
	if truncated {
		fmt.Fprintf(&b, "\n_... %d more resources_\n", total-max)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// parseActionFilter converts a csv action filter into a set. The literal
// "all" expands to every action.
func parseActionFilter(csv string) map[core.Action]struct{} {
	allActions := map[core.Action]struct{}{
		core.ActionCreate:  {},
		core.ActionUpdate:  {},
		core.ActionDelete:  {},
		core.ActionReplace: {},
		core.ActionRead:    {},
	}
	if csv == "" || csv == "all" {
		return allActions
	}
	set := make(map[core.Action]struct{})
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		set[core.Action(p)] = struct{}{}
	}
	return set
}

func init() { defaultRegistry.Register(ChangedResourcesTable{}) }
