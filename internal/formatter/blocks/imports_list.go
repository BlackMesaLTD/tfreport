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
// For format=table the block is a thin wrapper over `table` — filters
// down to imports, then delegates markdown assembly to renderTable.
// For format=list it keeps its own bullet rendering (not a table shape).
//
// Args:
//
//	format (list|table; default "list")
//	    "list" emits `- ` backticked-address bullets.
//	    "table" emits a multi-column markdown table (via the shared
//	    renderTable helper — same code path as the `table` block).
//
//	columns csv (default "address,resource_type,module")
//	    Table-mode only. Valid IDs: address, resource_type, resource_name,
//	    module, module_path.
//
//	max int (default 0 = unlimited)
//	    Cap rows; extras collapse into `… N more imports`.
type ImportsList struct{}

func (ImportsList) Name() string { return "imports_list" }

// importsListColumns and importsListHeadings carry the legacy column-id
// + heading grammar users know. Internally we translate each id to its
// Resource-kind equivalent before delegating to renderTable.
var importsListColumns = []string{"address", "resource_type", "resource_name", "module", "module_path"}
var importsListHeadings = map[string]string{
	"address":       "Address",
	"resource_type": "Resource Type",
	"resource_name": "Name",
	"module":        "Module",
	"module_path":   "Module Path",
}

// importsListColumnRename maps the historic column IDs onto the
// Resource-kind registry's canonical IDs. Empty values keep the ID as
// supplied (when the table registry accepts it directly).
var importsListColumnRename = map[string]string{
	"resource_name": "name",
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

	// Filter resource tree nodes down to imports. Same for both formats —
	// list prints the addresses, table delegates to renderTable.
	nodes := collectImportNodes(ctx)
	if len(nodes) == 0 {
		return "", nil
	}

	total := len(nodes)
	truncated := 0
	if max > 0 && total > max {
		truncated = total - max
		nodes = nodes[:max]
	}

	switch format {
	case "list":
		var b strings.Builder
		for _, n := range nodes {
			rc, _ := n.Payload.(*core.ResourceChange)
			if rc == nil {
				continue
			}
			fmt.Fprintf(&b, "- `%s`\n", rc.Address)
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "- _... %d more imports_\n", truncated)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "table":
		// Translate legacy column ids → table registry canonical ids.
		// Carry heading overrides so imports_list-style headings ("Resource
		// Type", "Module Path") survive the switch to the shared renderer.
		canonical := make([]string, len(cols))
		headings := map[string]string{}
		for i, id := range cols {
			dst := id
			if rename, ok := importsListColumnRename[id]; ok {
				dst = rename
			}
			canonical[i] = dst
			headings[dst] = importsListHeadings[id]
		}
		return renderTable(ctx, nodes, core.KindResource, canonical, tableRenderOpts{
			HeadingOverrides: headings,
			TruncatedCount:   truncated,
			TruncationLine:   fmt.Sprintf("_... %d more imports_", truncated),
		})
	}
	return "", nil
}

// collectImportNodes returns the Resource tree nodes whose Payload has
// IsImport=true. Tree-first: uses ctx.Tree when bound, otherwise
// falls back to a per-report tree build so unit-test contexts still
// work without callers pre-populating ctx.Tree.
func collectImportNodes(ctx *BlockContext) []*core.Node {
	var roots []*core.Node
	if ctx.Tree != nil && ctx.Tree.Root != nil {
		roots = []*core.Node{ctx.Tree.Root}
	} else {
		for _, r := range allReports(ctx) {
			if r == nil {
				continue
			}
			if tree := core.BuildTree(r); tree != nil && tree.Root != nil {
				roots = append(roots, tree.Root)
			}
		}
	}

	var out []*core.Node
	for _, root := range roots {
		for _, n := range core.Query(root, core.Path{core.KindResource}) {
			rc, ok := n.Payload.(*core.ResourceChange)
			if !ok || rc == nil || !rc.IsImport {
				continue
			}
			out = append(out, n)
		}
	}
	return out
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
		Summary: "Enumerates resources with `IsImport=true` across all module groups. Bulleted list by default; table with pluggable columns when `format=table` (delegates to the shared `table` renderer).",
		Args: []ArgDoc{
			{Name: "format", Type: "string", Default: "list", Description: "One of `list` (bullet points) or `table` (markdown table via the shared `table` renderer)."},
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
