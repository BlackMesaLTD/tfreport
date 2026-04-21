package blocks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// ModulesTable renders a flat one-row-per-module-group markdown table.
// Pick columns, optionally cap rows, done — no Sprig gymnastics needed in
// the template.
//
// Args:
//
//	columns (string, default "module,changed_attrs")
//	    Comma-separated column IDs. Supported:
//	      module_type    Module type derived from the source URL
//	      module         Module call name (ModuleGroup.Name)
//	      module_path    Full module path (e.g. "module.vnet.module.subnet")
//	      description    Team-supplied module description (from config)
//	      resources      Count of resource changes in the group
//	      actions        Action summary (e.g. "2 update, 1 create")
//	      impact         Worst impact across the group
//	      changed_attrs  Union of all changed attribute keys in the group
//
//	report (*core.Report, optional)
//	    Explicit report to render against. Required when the template is
//	    looping `range .Reports` — pass `$r` so the block knows which
//	    subscription's modules to render. Absent → uses the context's
//	    current report (the single-report case or the first of many).
//
//	max (int, default 0 = no limit)
//	    Cap the table at this many rows. Extra rows collapse into a single
//	    "…" row.
//
//	empty (string, default "—")
//	    Cell value for empty/missing data.
type ModulesTable struct{}

func (ModulesTable) Name() string { return "modules_table" }

func (ModulesTable) Render(ctx *BlockContext, args map[string]any) (string, error) {
	var r *core.Report
	if v, ok := args["report"]; ok && v != nil {
		if rr, ok := v.(*core.Report); ok {
			r = rr
		} else {
			return "", fmt.Errorf("modules_table: 'report' arg must be a *core.Report, got %T", v)
		}
	}
	if r == nil {
		r = currentReport(ctx)
	}
	if r == nil || len(r.ModuleGroups) == 0 {
		return "", nil
	}

	cols := ArgCSV(args, "columns")
	if len(cols) == 0 {
		cols = []string{"module", "changed_attrs"}
	}
	max := ArgInt(args, "max", 0)
	empty := ArgString(args, "empty", "—")

	changedMode := ArgString(args, "changed_attrs_display", "")
	if err := validChangedAttrsMode("modules_table", changedMode); err != nil {
		return "", err
	}
	changedMode = resolveChangedAttrsMode(ctx, changedMode)

	for _, c := range cols {
		if _, ok := moduleColumns[c]; !ok {
			return "", fmt.Errorf("modules_table: unknown column %q (valid: %s)",
				c, strings.Join(sortedColumnIDs(), ", "))
		}
	}

	var b strings.Builder

	// Header row
	b.WriteString("|")
	for _, c := range cols {
		fmt.Fprintf(&b, " %s |", moduleColumns[c].heading)
	}
	b.WriteString("\n|")
	for range cols {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	groups := r.ModuleGroups
	truncated := 0
	if max > 0 && len(groups) > max {
		truncated = len(groups) - max
		groups = groups[:max]
	}

	for _, mg := range groups {
		b.WriteString("|")
		for _, c := range cols {
			cell := moduleColumns[c].render(mg, r, changedMode)
			if cell == "" {
				cell = empty
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		b.WriteString("\n")
	}

	if truncated > 0 {
		b.WriteString("|")
		for i := range cols {
			if i == 0 {
				fmt.Fprintf(&b, " … %d more module(s) not shown |", truncated)
			} else {
				b.WriteString(" |")
			}
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

type moduleColumn struct {
	heading string
	// render takes the group, its enclosing report, and the current
	// changed_attrs display mode (resolved from arg + ctx + default).
	// Most columns ignore the mode argument.
	render func(mg core.ModuleGroup, r *core.Report, changedMode string) string
}

var moduleColumns = map[string]moduleColumn{
	"module_type": {
		heading: "Module type",
		render: func(mg core.ModuleGroup, r *core.Report, _ string) string {
			mt := core.ModuleTypeForGroup(mg, r)
			if mt == "" {
				return ""
			}
			return "`" + mt + "`"
		},
	},
	"module": {
		heading: "Module",
		render: func(mg core.ModuleGroup, _ *core.Report, _ string) string {
			if mg.Name == "" {
				return ""
			}
			return "`" + mg.Name + "`"
		},
	},
	"module_path": {
		heading: "Module path",
		render: func(mg core.ModuleGroup, _ *core.Report, _ string) string {
			if mg.Path == "" {
				return ""
			}
			return "`" + mg.Path + "`"
		},
	},
	"description": {
		heading: "Description",
		render: func(mg core.ModuleGroup, _ *core.Report, _ string) string {
			return mg.Description
		},
	},
	"resources": {
		heading: "Resources",
		render: func(mg core.ModuleGroup, _ *core.Report, _ string) string {
			return strconv.Itoa(len(mg.Changes))
		},
	},
	"actions": {
		heading: "Actions",
		render: func(mg core.ModuleGroup, _ *core.Report, _ string) string {
			return actionSummaryLine(mg.ActionCounts)
		},
	},
	"impact": {
		heading: "Impact",
		render: func(mg core.ModuleGroup, _ *core.Report, _ string) string {
			imp := core.MaxImpactForGroup(mg)
			if imp == "" {
				return ""
			}
			return core.ImpactEmoji(imp) + " " + string(imp)
		},
	},
	"changed_attrs": {
		heading: "Changed attributes",
		render:  renderModulesTableChangedAttrs,
	},
}

// renderModulesTableChangedAttrs builds the per-group cell for the
// changed_attrs column. Behavior depends on changedMode:
//
//   - mode="list" (legacy) → union of every ChangedAttribute key across every
//     resource in the group, regardless of action.
//   - mode in {dash, wordy, count} → attrs from create/delete resources are
//     excluded from the union. If the group has any update/replace resources,
//     render the filtered union. If the whole group is create/delete/read/no-op,
//     render the mode-appropriate placeholder (dash / new|removed|new+removed /
//     count-of-all-attrs).
//
// Empty output is returned as "" (the Render loop replaces it with the
// block's `empty` arg, default "—").
func renderModulesTableChangedAttrs(mg core.ModuleGroup, _ *core.Report, changedMode string) string {
	if changedMode == "" {
		changedMode = ChangedAttrsDash
	}

	if changedMode == ChangedAttrsList {
		return unionAttrKeys(mg.Changes)
	}

	// Partition resources into meaningful (update/replace) vs compact
	// (create/delete/read/no-op). If any meaningful ones exist, render
	// their union only.
	var meaningful []core.ResourceChange
	var create, del int
	for _, rc := range mg.Changes {
		switch rc.Action {
		case core.ActionUpdate, core.ActionReplace:
			meaningful = append(meaningful, rc)
		case core.ActionCreate:
			create++
		case core.ActionDelete:
			del++
		}
	}
	if len(meaningful) > 0 {
		return unionAttrKeys(meaningful)
	}

	// Whole group is create/delete/read/no-op. Mode decides the placeholder.
	switch changedMode {
	case ChangedAttrsWordy:
		switch {
		case create > 0 && del > 0:
			return "new+removed"
		case create > 0:
			return "new"
		case del > 0:
			return "removed"
		default:
			return "—"
		}
	case ChangedAttrsCount:
		total := 0
		for _, rc := range mg.Changes {
			total += len(rc.ChangedAttributes)
		}
		return fmt.Sprintf("%d attrs", total)
	default: // ChangedAttrsDash
		return "—"
	}
}

// unionAttrKeys returns a sorted, backtick-wrapped, comma-joined union of
// every ChangedAttribute key across the supplied resources. Returns ""
// when the union is empty (caller decides the placeholder).
func unionAttrKeys(changes []core.ResourceChange) string {
	seen := map[string]struct{}{}
	for _, rc := range changes {
		for _, ca := range rc.ChangedAttributes {
			seen[ca.Key] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return ""
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = "`" + k + "`"
	}
	return strings.Join(parts, ", ")
}

func sortedColumnIDs() []string {
	ids := make([]string, 0, len(moduleColumns))
	for id := range moduleColumns {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Doc describes modules_table for cmd/docgen.
func (ModulesTable) Doc() BlockDoc {
	cols := make([]ColumnDoc, 0, len(moduleColumns))
	for id, col := range moduleColumns {
		cols = append(cols, ColumnDoc{
			ID:          id,
			Heading:     col.heading,
			Description: moduleColumnDescriptions[id],
		})
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].ID < cols[j].ID })

	return BlockDoc{
		Name:    "modules_table",
		Summary: "Flat one-row-per-module-group markdown table with pluggable columns. Pick columns, optionally cap rows.",
		Args: []ArgDoc{
			{Name: "report", Type: "*core.Report", Default: "(current report)", Description: "Explicit report to render. Required when looping range .Reports; pass $r."},
			{Name: "columns", Type: "csv", Default: "module,changed_attrs", Description: "Comma-separated column IDs to include. See Columns below."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap the table at this many rows. Extra rows collapse into a single '…' row."},
			{Name: "empty", Type: "string", Default: "—", Description: "Cell value used for empty/missing data."},
			{Name: "changed_attrs_display", Type: "string", Default: "(cfg.Output.ChangedAttrsDisplay or `dash`)", Description: "How the `changed_attrs` union column treats create/delete resources. Non-list modes exclude their attrs from the union when update/replace resources are present; when the whole group is create/delete, render a mode-appropriate placeholder (dash / wordy new|removed|new+removed / count of total attrs). `list` preserves the legacy full union."},
		},
		Columns: cols,
		Examples: []ExampleDoc{
			{
				Template: `{{ modules_table "report" $r "columns" "module_type,module,changed_attrs" }}`,
				Rendered: "| Module type | Module | Changed attributes |\n|---|---|---|\n| `virtual_network` | `vnet` | `address_space`, `tags` |",
			},
		},
	}
}

// moduleColumnDescriptions carries one-line descriptions for each column ID
// — separate from the renderer map so godoc stays close to presentation.
var moduleColumnDescriptions = map[string]string{
	"module_type":   "Module type derived from the source URL (e.g. `virtual_network`).",
	"module":        "Module call name (ModuleGroup.Name).",
	"module_path":   "Full dotted module path (e.g. `module.vnet.module.subnet`).",
	"description":   "Team-supplied module description from config or presets.",
	"resources":     "Count of resource changes in the group.",
	"actions":       "Action summary line (e.g. `2 update, 1 create`).",
	"impact":        "Worst impact across the group, with emoji.",
	"changed_attrs": "Union of all changed attribute keys in the group.",
	"subscription":  "", // reserved for future use
}

func init() { defaultRegistry.Register(ModulesTable{}) }
