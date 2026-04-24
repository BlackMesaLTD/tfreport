package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// Table is the generic tree-query-backed table renderer. It selects
// nodes via a path expression, optionally filters / sorts / truncates
// them, and emits a markdown table with user-chosen columns.
//
// Args:
//
//	source  (string, required)  — Path selector (e.g. "resource",
//	    "module_instance > resource"). See core.ParsePath.
//	where   (string, optional)  — HCL predicate. Nodes where it returns
//	    false are dropped.
//	sort    (string, optional)  — HCL expression. String or number result.
//	desc    (bool,   optional)  — reverse sort direction.
//	limit   (int,    optional)  — cap rows.
//	columns (csv,    optional)  — column ids in render order. Defaults
//	    depend on the last Path step's kind.
//	heading (string, optional)  — inserts `### heading\n\n` above the
//	    table. Empty (default) emits no heading.
//	empty   (string, optional)  — emitted when zero rows match. Default
//	    empty string — caller's template composes around absence.
//
// Not yet supported (rejected with an actionable error):
//
//	group — will split the output into one labelled table per group in a
//	    later PR. For now, do the grouping in the template via multiple
//	    `table` calls with differing `where` args.
type Table struct{}

func (Table) Name() string { return "table" }

func (Table) Render(ctx *BlockContext, args map[string]any) (string, error) {
	if ctx.Tree == nil || ctx.Tree.Root == nil {
		// No tree built (multi-report pipeline path that hasn't been
		// migrated yet, or unit test context). Render nothing.
		return ArgString(args, "empty", ""), nil
	}

	// Reject args we don't implement yet, so users don't silently get
	// ignored behaviour.
	if v := ArgString(args, "group", ""); v != "" {
		return "", fmt.Errorf("table: group arg is not yet supported; split into multiple table calls with where= for now")
	}

	source := ArgString(args, "source", "")
	if source == "" {
		return "", fmt.Errorf("table: source is required (e.g. \"resource\" or \"module_instance > resource\")")
	}

	path, err := core.ParsePath(source)
	if err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	if len(path) == 0 {
		return "", fmt.Errorf("table: source %q yielded an empty path", source)
	}
	rowKind := path[len(path)-1]

	// Resolve columns for the row kind. Empty request → kind defaults.
	colIDs := ArgCSV(args, "columns")
	defaults, ok := tableDefaultColumns[rowKind]
	if !ok {
		return "", fmt.Errorf("table: no column schema registered for kind %q", rowKind)
	}
	colIDs = defaultCols(colIDs, defaults)
	validSet := toColumnSet(rowKind)
	if err := validateColumns("table", colIDs, validSet); err != nil {
		return "", err
	}

	// Run the query pipeline.
	nodes := core.Query(ctx.Tree.Root, path)

	if w := ArgString(args, "where", ""); w != "" {
		expr, err := core.ParseExpr(w, "table.where")
		if err != nil {
			return "", fmt.Errorf("table: where: %w", err)
		}
		nodes, err = core.Filter(nodes, expr, nil)
		if err != nil {
			return "", fmt.Errorf("table: %w", err)
		}
	}

	if s := ArgString(args, "sort", ""); s != "" {
		expr, err := core.ParseExpr(s, "table.sort")
		if err != nil {
			return "", fmt.Errorf("table: sort: %w", err)
		}
		nodes, err = core.SortBy(nodes, expr, ArgBool(args, "desc", false))
		if err != nil {
			return "", fmt.Errorf("table: %w", err)
		}
	}

	if n := ArgInt(args, "limit", 0); n > 0 {
		nodes = core.Limit(nodes, n)
	}

	if len(nodes) == 0 {
		return ArgString(args, "empty", ""), nil
	}

	// Build header + rows.
	var b strings.Builder
	if h := ArgString(args, "heading", ""); h != "" {
		fmt.Fprintf(&b, "### %s\n\n", h)
	}
	headings := make([]string, len(colIDs))
	for i, id := range colIDs {
		headings[i] = tableColumns[rowKind][id].Heading
	}
	writeColumnHeader(&b, headings)
	for _, n := range nodes {
		b.WriteString("|")
		for _, id := range colIDs {
			fmt.Fprintf(&b, " %s |", tableColumns[rowKind][id].Render(ctx, n))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (Table) Doc() BlockDoc {
	return BlockDoc{
		Name:    "table",
		Summary: "Generic tree-query-backed markdown table. Select nodes via a path, optionally filter / sort / limit, render columns.",
		Args: []ArgDoc{
			{Name: "source", Type: "string", Default: "", Description: "Path selector — e.g. `\"resource\"` or `\"module_instance > resource\"`. Required. See `core.ParsePath` for grammar."},
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate. Drops nodes where it evaluates false. `self` binds to each candidate."},
			{Name: "sort", Type: "string", Default: "", Description: "HCL expression yielding a string or number per node. Stable sort."},
			{Name: "desc", Type: "bool", Default: "false", Description: "Reverse sort direction."},
			{Name: "limit", Type: "int", Default: "0", Description: "Cap the row count. `<= 0` means no cap."},
			{Name: "columns", Type: "csv", Default: "(kind-dependent)", Description: "Ordered column ids. Valid ids depend on the row kind (the final Path step)."},
			{Name: "heading", Type: "string", Default: "", Description: "Inserts `### heading` above the table when non-empty."},
			{Name: "empty", Type: "string", Default: "", Description: "Rendered when zero rows match. Blank by default — caller's template handles absence."},
		},
		Columns: tableColumnDocs(),
	}
}

func tableColumnDocs() []ColumnDoc {
	var out []ColumnDoc
	for _, kind := range []core.NodeKind{core.KindResource, core.KindAttribute, core.KindKeyChange} {
		for _, id := range tableDefaultColumns[kind] {
			if col, ok := tableColumns[kind][id]; ok {
				out = append(out, ColumnDoc{
					ID:          fmt.Sprintf("%s:%s", kind, id),
					Heading:     col.Heading,
					Description: col.Description,
				})
			}
		}
	}
	return out
}

func init() { defaultRegistry.Register(Table{}) }
