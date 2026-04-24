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
//	empty   (string, optional)  — emitted when zero rows match AND used
//	    as the fallback for any column cell that renders to "". Default
//	    empty string — caller's template composes around absence.
//	truncated_noun (string, optional) — noun used in the "… N more
//	    {noun} not shown" row appended when `limit` truncates output.
//	    Default: derived from the row kind (e.g. "modules" for
//	    module_instance, "resources" for resource).
//	changed_attrs_display (string, optional) — mode override for any
//	    cell that renders a union of changed-attribute keys. Valid
//	    values: dash, wordy, count, list. Propagates through the cell
//	    renderer via a shallow-copied ctx so column code stays stateless.
//
//	report  (*core.Report, optional) — scope the query to one specific
//	    report's subtree. Accepts `$r` inside a `{{ range .Reports }}`
//	    loop, matching the legacy modules_table convention. When unset
//	    the query runs from ctx.Tree.Root (all reports in multi mode).
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

	// Pick the query scope: the full tree by default, OR one specific
	// report's subtree when `report=$r` is passed (migration aid for
	// callers moving off modules_table's `{{ table "report" $r ... }}`
	// idiom).
	scope := ctx.Tree.Root
	if v, ok := args["report"]; ok && v != nil {
		r, ok := v.(*core.Report)
		if !ok {
			return "", fmt.Errorf("table: 'report' arg must be a *core.Report, got %T", v)
		}
		sub := findReportSubtree(ctx.Tree.Root, r)
		if sub == nil {
			return "", fmt.Errorf("table: 'report' arg refers to a report not present in ctx.Tree")
		}
		scope = sub
	}

	// Run the query pipeline.
	nodes := core.Query(scope, path)

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

	// Propagate `changed_attrs_display` override via a shallow-copied
	// ctx so column render functions read ctx.Output.ChangedAttrsDisplay
	// without needing args threaded through every signature.
	if modeArg := ArgString(args, "changed_attrs_display", ""); modeArg != "" {
		if err := validChangedAttrsMode("table", modeArg); err != nil {
			return "", err
		}
		cp := *ctx
		cp.Output.ChangedAttrsDisplay = modeArg
		ctx = &cp
	}

	totalRows := len(nodes)
	truncated := 0
	if n := ArgInt(args, "limit", 0); n > 0 && totalRows > n {
		truncated = totalRows - n
		nodes = core.Limit(nodes, n)
	}

	if len(nodes) == 0 {
		return ArgString(args, "empty", ""), nil
	}

	empty := ArgString(args, "empty", "")

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
			cell := tableColumns[rowKind][id].Render(ctx, n)
			if cell == "" && empty != "" {
				cell = empty
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		b.WriteString("\n")
	}

	// Truncation marker when limit clipped rows — mirrors the legacy
	// modules_table / changed_resources_table grammar so thin-wrapper
	// migrations stay byte-exact.
	if truncated > 0 {
		noun := ArgString(args, "truncated_noun", defaultTruncatedNoun(rowKind))
		b.WriteString("|")
		for i := range colIDs {
			if i == 0 {
				fmt.Fprintf(&b, " … %d more %s not shown |", truncated, noun)
			} else {
				b.WriteString(" |")
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// defaultTruncatedNoun picks a plural noun for the truncation row
// based on the row kind. Callers can override via the `truncated_noun`
// arg.
func defaultTruncatedNoun(kind core.NodeKind) string {
	switch kind {
	case core.KindResource:
		return "resources"
	case core.KindAttribute:
		return "attributes"
	case core.KindKeyChange:
		return "changes"
	case core.KindModuleInstance:
		return "module(s)"
	case core.KindReport:
		return "reports"
	default:
		return "rows"
	}
}

func (Table) Doc() BlockDoc {
	return BlockDoc{
		Name:    "table",
		Summary: "Generic tree-query-backed markdown table. Select nodes via a path, optionally filter / sort / limit, render columns.",
		Args: []ArgDoc{
			{Name: "source", Type: "string", Default: "", Description: "Path selector — e.g. `\"resource\"`, `\"module_instance\"`, `\"report\"`, or chained like `\"module_instance > resource\"`. Required. See `core.ParsePath` for grammar."},
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate. Drops nodes where it evaluates false. `self` binds to each candidate."},
			{Name: "sort", Type: "string", Default: "", Description: "HCL expression yielding a string or number per node. Stable sort."},
			{Name: "desc", Type: "bool", Default: "false", Description: "Reverse sort direction."},
			{Name: "limit", Type: "int", Default: "0", Description: "Cap the row count. `<= 0` means no cap."},
			{Name: "columns", Type: "csv", Default: "(kind-dependent)", Description: "Ordered column ids. Valid ids depend on the row kind (the final Path step)."},
			{Name: "heading", Type: "string", Default: "", Description: "Inserts `### heading` above the table when non-empty."},
			{Name: "empty", Type: "string", Default: "", Description: "Rendered when zero rows match AND used as the cell fallback for any column renderer that returns an empty string. Blank by default."},
			{Name: "truncated_noun", Type: "string", Default: "(kind-derived)", Description: "Noun used in the `… N more {noun} not shown` row appended when `limit` truncates output. Default derives from the row kind: resources / attributes / changes / module(s) / reports."},
			{Name: "changed_attrs_display", Type: "string", Default: "(from ctx.Output)", Description: "Mode override for columns that render changed-attribute unions: `dash`, `wordy`, `count`, `list`. Propagates to cell renderers via shallow-copied ctx."},
			{Name: "report", Type: "*core.Report", Default: "(all reports)", Description: "Scope the query to one report's subtree. Accepts `$r` inside a `{{ range .Reports }}` loop — matches the legacy `modules_table \"report\" $r` convention so existing templates migrate with minimal rewiring."},
		},
		Columns: tableColumnDocs(),
	}
}

func tableColumnDocs() []ColumnDoc {
	var out []ColumnDoc
	for _, kind := range []core.NodeKind{
		core.KindResource,
		core.KindAttribute,
		core.KindKeyChange,
		core.KindModuleInstance,
		core.KindReport,
	} {
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

// findReportSubtree locates the Report node in root whose Payload is
// the exact *core.Report pointer target. For a single-report tree
// (root.Kind == KindReport) it checks identity and returns root on
// match; for a multi-report tree it walks root's children. Pointer
// identity is required because two reports can share a label yet be
// logically distinct (different subscriptions, different runs).
func findReportSubtree(root *core.Node, target *core.Report) *core.Node {
	if root == nil || target == nil {
		return nil
	}
	switch root.Kind {
	case core.KindReport:
		if r, ok := root.Payload.(*core.Report); ok && r == target {
			return root
		}
		return nil
	case core.KindReports:
		for _, c := range root.Children {
			if c.Kind != core.KindReport {
				continue
			}
			if r, ok := c.Payload.(*core.Report); ok && r == target {
				return c
			}
		}
	}
	return nil
}

func init() { defaultRegistry.Register(Table{}) }
