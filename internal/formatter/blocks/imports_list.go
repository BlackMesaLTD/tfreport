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

	type importedRow struct {
		rc core.ResourceChange
		mg core.ModuleGroup
	}
	var rows []importedRow
	for _, r := range allReports(ctx) {
		for _, mg := range r.ModuleGroups {
			for _, rc := range mg.Changes {
				if rc.IsImport {
					rows = append(rows, importedRow{rc: rc, mg: mg})
				}
			}
		}
	}
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
				fmt.Fprintf(&b, " %s |", renderImportsCell(ctx, row.rc, row.mg, col))
			}
			b.WriteString("\n")
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "\n_... %d more imports_\n", truncated)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func renderImportsCell(ctx *BlockContext, rc core.ResourceChange, mg core.ModuleGroup, col string) string {
	switch col {
	case "address":
		return "`" + rc.Address + "`"
	case "resource_type":
		return displayName(ctx, rc.ResourceType)
	case "resource_name":
		return core.ResourceDisplayLabel(rc)
	case "module":
		if mg.Name == "" {
			return "(root)"
		}
		return "`" + mg.Name + "`"
	case "module_path":
		if mg.Path == "" {
			return "(root)"
		}
		return "`" + mg.Path + "`"
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
