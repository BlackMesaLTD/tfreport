package blocks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// ImportsList enumerates resources with IsImport=true across all module
// groups. Two formats (bulleted list or markdown table); list is the
// default because most recipes want a collapsible dropdown of addresses.
//
// Internally this block now runs over the PlanTree — it queries for every
// Resource node and filters on IsImport — but its output is byte-exact
// identical to the prior ModuleGroups-iterating implementation. The
// refactor unlocks future query-engine features (user-authored
// where/group/sort) without forcing every caller to learn a new block
// name.
//
// Args:
//
//	format (list|table; default "list")
//	    "list" emits `- ` backticked-address bullets.
//	    "table" emits a multi-column markdown table.
//
//	columns csv (default "address,resource_type,module")
//	    Table-mode only. Valid IDs: address, resource_type, resource_name,
//	    module, module_path.
//
//	max int (default 0 = unlimited)
//	    Cap rows; extras collapse into `… N more imports`.
type ImportsList struct{}

func (ImportsList) Name() string { return "imports_list" }

var importsListColumns = []string{"address", "resource_type", "resource_name", "module", "module_path"}
var importsListHeadings = map[string]string{
	"address":       "Address",
	"resource_type": "Resource Type",
	"resource_name": "Name",
	"module":        "Module",
	"module_path":   "Module Path",
}

func (ImportsList) Render(ctx *BlockContext, args map[string]any) (string, error) {
	format := ArgString(args, "format", "list")
	switch format {
	case "list", "table":
	default:
		return "", fmt.Errorf("imports_list: unknown format %q (valid: list, table)", format)
	}

	cols := defaultCols(ArgCSV(args, "columns"), []string{"address", "resource_type", "module"})
	if format == "table" {
		if err := validateColumns("imports_list", cols, toSet(importsListColumns)); err != nil {
			return "", err
		}
	}
	max := ArgInt(args, "max", 0)

	rows := collectImportRows(ctx)
	if len(rows) == 0 {
		return "", nil
	}

	total := len(rows)
	truncated := 0
	if max > 0 && total > max {
		truncated = total - max
		rows = rows[:max]
	}

	var b strings.Builder
	switch format {
	case "list":
		for _, row := range rows {
			fmt.Fprintf(&b, "- `%s`\n", row.rc.Address)
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "- _... %d more imports_\n", truncated)
		}
	case "table":
		headings := mapSlice(cols, func(id string) string { return importsListHeadings[id] })
		writeColumnHeader(&b, headings)
		for _, row := range rows {
			b.WriteString("|")
			for _, col := range cols {
				fmt.Fprintf(&b, " %s |", renderImportsCell(ctx, row, col))
			}
			b.WriteString("\n")
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "\n_... %d more imports_\n", truncated)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// importRow is the scratch shape the renderer iterates over. Derived from
// either the PlanTree (preferred) or the legacy ModuleGroups fallback.
// ModuleName and ModulePath mirror the old ModuleGroup.Name / .Path fields
// the previous implementation rendered against.
type importRow struct {
	rc         core.ResourceChange
	moduleName string
	modulePath string
}

// collectImportRows is the tree-first collector with a legacy fallback.
// When ctx.Tree is populated it runs Query("resource") and filters by
// IsImport via a single walk — one pass, no per-block-duplicated loop
// nesting. When the tree is absent (older multi-report callers that
// haven't been migrated, or unit tests) it falls back to the original
// ModuleGroups iteration so callers see no behaviour change.
//
// Row ordering matches the tree walk, which is pre-order over the
// grouper's path-sorted ModuleGroups — preserving the existing golden
// output byte-for-byte.
func collectImportRows(ctx *BlockContext) []importRow {
	if ctx.Tree != nil && ctx.Tree.Root != nil {
		return collectImportRowsFromTree(ctx.Tree)
	}
	return collectImportRowsFromReports(ctx)
}

func collectImportRowsFromTree(tree *core.PlanTree) []importRow {
	nodes := core.Query(tree.Root, core.Path{core.KindResource})
	var rows []importRow
	for _, n := range nodes {
		rc, ok := n.Payload.(*core.ResourceChange)
		if !ok || rc == nil || !rc.IsImport {
			continue
		}
		name, path := enclosingModuleDisplay(n, rc)
		rows = append(rows, importRow{
			rc:         *rc,
			moduleName: name,
			modulePath: path,
		})
	}
	return rows
}

// enclosingModuleDisplay returns the (Name, Path) pair the legacy
// imports_list rendered from the ResourceChange's enclosing ModuleGroup.
// For root-module resources both values are "(root)" (matching
// grouper.moduleName's sentinel). For nested resources Name is the
// innermost ModuleCall's name and Path is rc.ModulePath verbatim.
func enclosingModuleDisplay(n *core.Node, rc *core.ResourceChange) (string, string) {
	if rc.ModulePath == "" {
		return "(root)", "(root)"
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Kind == core.KindModuleCall {
			return p.Name, rc.ModulePath
		}
	}
	return "", rc.ModulePath
}

// collectImportRowsFromReports is the pre-tree fallback. Kept so blocks
// still render sensibly in unit-test BlockContexts that don't build a
// tree.
func collectImportRowsFromReports(ctx *BlockContext) []importRow {
	var rows []importRow
	for _, r := range allReports(ctx) {
		for _, mg := range r.ModuleGroups {
			for _, rc := range mg.Changes {
				if rc.IsImport {
					rows = append(rows, importRow{
						rc:         rc,
						moduleName: mg.Name,
						modulePath: mg.Path,
					})
				}
			}
		}
	}
	return rows
}

func renderImportsCell(ctx *BlockContext, row importRow, col string) string {
	switch col {
	case "address":
		return "`" + row.rc.Address + "`"
	case "resource_type":
		return displayName(ctx, row.rc.ResourceType)
	case "resource_name":
		return core.ResourceDisplayLabel(row.rc)
	case "module":
		if row.moduleName == "" {
			return "(root)"
		}
		return "`" + row.moduleName + "`"
	case "module_path":
		if row.modulePath == "" {
			return "(root)"
		}
		return "`" + row.modulePath + "`"
	}
	return ""
}

// Doc describes imports_list for cmd/docgen.
func (ImportsList) Doc() BlockDoc {
	cols := make([]ColumnDoc, 0, len(importsListColumns))
	for _, id := range importsListColumns {
		cols = append(cols, ColumnDoc{
			ID:          id,
			Heading:     importsListHeadings[id],
			Description: importsListColumnDescriptions[id],
		})
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].ID < cols[j].ID })

	return BlockDoc{
		Name:    "imports_list",
		Summary: "Enumerates resources with `IsImport=true` across all module groups. Bulleted list by default; table with pluggable columns when `format=table`.",
		Args: []ArgDoc{
			{Name: "format", Type: "string", Default: "list", Description: "One of `list` (bullet points) or `table` (markdown table)."},
			{Name: "columns", Type: "csv", Default: "address,resource_type,module", Description: "Table-mode columns."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap rows; truncated rows collapse into `… N more imports`."},
		},
		Columns: cols,
	}
}

var importsListColumnDescriptions = map[string]string{
	"address":       "Full terraform address, backticked.",
	"resource_type": "Display name for the resource type.",
	"resource_name": "Pre-computed resource display label.",
	"module":        "Enclosing module call name.",
	"module_path":   "Enclosing module's full dotted path.",
}

func init() { defaultRegistry.Register(ImportsList{}) }
