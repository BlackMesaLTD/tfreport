package blocks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// ChangedResourcesTable renders a per-resource impact table:
//
//	| Resource | Name | Changed | Impact |   (default)
//
// Columns are pluggable via the `columns` csv arg, and the row set can be
// narrowed with several predicates (action, impact, module, module_type,
// resource_type, is_import).
//
// Args:
//
//	columns csv (default "resource_type,name,changed,impact")
//	    See the Columns doc table for every valid ID.
//
//	actions csv (default "update,delete,replace")
//	    Filter by action. Use "all" to include create and read.
//
//	impact csv (default "")
//	    Filter: keep rows whose Impact is in the set (e.g. "critical,high").
//
//	modules csv (default "")
//	    Filter: keep rows whose top-level module call name matches.
//
//	module_types csv (default "")
//	    Filter: keep rows whose resolved module type matches.
//
//	resource_types csv (default "")
//	    Filter: keep rows whose ResourceType matches exactly.
//
//	is_import (default "")
//	    Filter: "true" keeps only imports, "false" keeps only non-imports,
//	    empty keeps both.
//
//	max int (default 0 = unlimited)
//	    Cap rows; appends "_... N more resources_" when truncated.
type ChangedResourcesTable struct{}

func (ChangedResourcesTable) Name() string { return "changed_resources_table" }

// Column registry for changed_resources_table.
var changedResourcesColumns = []string{
	"resource_type", "name", "address", "module", "module_type",
	"changed", "impact", "action", "force_new", "is_import", "notes",
}

var changedResourcesHeadings = map[string]string{
	"resource_type": "Resource",
	"name":          "Name",
	"address":       "Address",
	"module":        "Module",
	"module_type":   "Module Type",
	"changed":       "Changed",
	"impact":        "Impact",
	"action":        "Action",
	"force_new":     "Force-new",
	"is_import":     "Import",
	"notes":         "Notes",
}

// changedResourcesRow carries both the resource and its enclosing module
// group; columns like `module`, `module_type` need mg context.
type changedResourcesRow struct {
	rc core.ResourceChange
	mg core.ModuleGroup
}

func (ChangedResourcesTable) Render(ctx *BlockContext, args map[string]any) (string, error) {
	cols := defaultCols(ArgCSV(args, "columns"),
		[]string{"resource_type", "name", "changed", "impact"})
	if err := validateColumns("changed_resources_table", cols, toSet(changedResourcesColumns)); err != nil {
		return "", err
	}

	actions := parseActionFilter(ArgString(args, "actions", "update,delete,replace"))
	impactFilter := parseImpactFilterSet(ArgCSV(args, "impact"))
	moduleFilter := toCaseInsensitiveSet(ArgCSV(args, "modules"))
	moduleTypeFilter := toCaseInsensitiveSet(ArgCSV(args, "module_types"))
	resourceTypeFilter := toSet(ArgCSV(args, "resource_types"))
	isImportFilter := ArgString(args, "is_import", "")
	max := ArgInt(args, "max", 0)

	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	var rows []changedResourcesRow
	for _, mg := range r.ModuleGroups {
		topLevel := core.TopLevelModuleName(mg.Path)
		modType := core.ResolveModuleType(topLevel, r.ModuleSources, mg.Name)

		// Module / module_type filters apply to the group as a whole.
		if len(moduleFilter) > 0 && !matchesFilter(moduleFilter, topLevel, mg.Name) {
			continue
		}
		if len(moduleTypeFilter) > 0 {
			if _, ok := moduleTypeFilter[strings.ToLower(modType)]; !ok {
				continue
			}
		}

		for _, rc := range mg.Changes {
			if _, ok := actions[rc.Action]; !ok {
				continue
			}
			if impactFilter != nil {
				if _, ok := impactFilter[rc.Impact]; !ok {
					continue
				}
			}
			if len(resourceTypeFilter) > 0 {
				if _, ok := resourceTypeFilter[rc.ResourceType]; !ok {
					continue
				}
			}
			switch isImportFilter {
			case "true":
				if !rc.IsImport {
					continue
				}
			case "false":
				if rc.IsImport {
					continue
				}
			}
			rows = append(rows, changedResourcesRow{rc: rc, mg: mg})
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
	headings := mapSlice(cols, func(id string) string { return changedResourcesHeadings[id] })
	writeColumnHeader(&b, headings)
	for _, row := range rows {
		b.WriteString("|")
		for _, col := range cols {
			fmt.Fprintf(&b, " %s |", renderChangedResourceCell(ctx, row, col))
		}
		b.WriteString("\n")
	}
	if truncated {
		fmt.Fprintf(&b, "\n_... %d more resources_\n", total-max)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func renderChangedResourceCell(ctx *BlockContext, row changedResourcesRow, col string) string {
	rc := row.rc
	mg := row.mg
	switch col {
	case "resource_type":
		return displayName(ctx, rc.ResourceType)
	case "name":
		return core.ResourceDisplayLabel(rc)
	case "address":
		return "`" + rc.Address + "`"
	case "module":
		if mg.Name == "" {
			return "(root)"
		}
		return "`" + mg.Name + "`"
	case "module_type":
		topLevel := core.TopLevelModuleName(mg.Path)
		r := currentReport(ctx)
		return core.ResolveModuleType(topLevel, r.ModuleSources, mg.Name)
	case "changed":
		return formatAttrsKeysOnly(rc.ChangedAttributes)
	case "impact":
		return formatImpactWithNote(ctx, rc)
	case "action":
		return fmt.Sprintf("%s %s", core.ActionEmoji(rc.Action), rc.Action)
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
	case "is_import":
		if rc.IsImport {
			return "♻️ yes"
		}
		return "—"
	case "notes":
		if ctx.NoteResolver == nil {
			return "—"
		}
		var notes []string
		for _, a := range rc.ChangedAttributes {
			if note := ctx.NoteResolver(rc.ResourceType, a.Key); note != "" {
				notes = append(notes, note)
			}
		}
		if len(notes) == 0 {
			return "—"
		}
		return strings.Join(notes, "; ")
	}
	return ""
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

// parseImpactFilterSet is like parseImpactFilter (key_changes.go) but
// operates on a []string already parsed from csv.
func parseImpactFilterSet(items []string) map[core.Impact]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[core.Impact]struct{}, len(items))
	for _, s := range items {
		out[core.Impact(s)] = struct{}{}
	}
	return out
}

// toCaseInsensitiveSet lowercases every entry so filter matching ignores case.
func toCaseInsensitiveSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, s := range items {
		out[strings.ToLower(s)] = struct{}{}
	}
	return out
}

// matchesFilter reports whether any of the supplied candidates (lowercased)
// appears in the filter set. Used for module filter which matches against
// either top-level name or mg.Name.
func matchesFilter(set map[string]struct{}, candidates ...string) bool {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, ok := set[strings.ToLower(c)]; ok {
			return true
		}
	}
	return false
}

// Doc describes changed_resources_table for cmd/docgen.
func (ChangedResourcesTable) Doc() BlockDoc {
	cols := make([]ColumnDoc, 0, len(changedResourcesColumns))
	for _, id := range changedResourcesColumns {
		cols = append(cols, ColumnDoc{
			ID:          id,
			Heading:     changedResourcesHeadings[id],
			Description: changedResourcesColumnDescriptions[id],
		})
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].ID < cols[j].ID })

	return BlockDoc{
		Name:    "changed_resources_table",
		Summary: "Per-resource impact table with pluggable columns and multi-axis filtering.",
		Args: []ArgDoc{
			{Name: "columns", Type: "csv", Default: "resource_type,name,changed,impact", Description: "Columns to render; see Columns table below."},
			{Name: "actions", Type: "csv", Default: "update,delete,replace", Description: "Filter by action. Use `all` to include create and read."},
			{Name: "impact", Type: "csv", Default: "(all)", Description: "Filter: keep rows whose Impact is in the set (e.g. `critical,high`)."},
			{Name: "modules", Type: "csv", Default: "(all)", Description: "Filter: keep rows whose top-level module call name matches (case-insensitive)."},
			{Name: "module_types", Type: "csv", Default: "(all)", Description: "Filter: keep rows whose resolved module type matches."},
			{Name: "resource_types", Type: "csv", Default: "(all)", Description: "Filter: keep rows whose ResourceType matches exactly."},
			{Name: "is_import", Type: "string", Default: "(both)", Description: "`true` keeps only imports, `false` only non-imports, empty keeps both."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap number of rows; truncated rows collapse into `… N more resources`."},
		},
		Columns: cols,
	}
}

var changedResourcesColumnDescriptions = map[string]string{
	"resource_type": "Display name for the resource type (e.g. `subnet`).",
	"name":          "Resource display label (pre-computed from Before/After `name` attr).",
	"address":       "Full terraform address in backticks (e.g. `module.vnet.azurerm_subnet.app`).",
	"module":        "Module call name (backticked).",
	"module_type":   "Resolved module type from source URL.",
	"changed":       "Changed attribute keys (backticked, comma-joined).",
	"impact":        "Impact emoji + level + optional note.",
	"action":        "Action emoji + action name.",
	"force_new":     "`✓` when any changed attribute is preset-marked force_new; `—` otherwise. Requires ctx.ForceNewResolver.",
	"is_import":     "`♻️ yes` for `rc.IsImport=true`; `—` otherwise.",
	"notes":         "Config-provided attribute notes for any changed attribute; `—` if none.",
}

func init() { defaultRegistry.Register(ChangedResourcesTable{}) }
