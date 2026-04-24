package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// RiskHistogram renders an impact-level distribution across all resources
// in scope. Three visual styles:
//
//	style="bar"    (default) \u2014 full markdown table with unicode bars
//	style="table"             \u2014 same as bar minus the Bar column
//	style="inline"            \u2014 single-line form: "\ud83d\udd34 0 \u00b7 \ud83d\udd34 1 \u00b7 \ud83d\udfe1 2 \u00b7 \ud83d\udfe2 1"
//
// Args:
//
//	style         string (default "bar")
//	include_none  bool   (default false) \u2014 include impact=none (no-op) in output
//	max_bar       int    (default 40)     \u2014 cap bar length to avoid overflow
//	where         string (default "")     \u2014 HCL predicate evaluated per resource
//	    with `self` bound to the Resource tree node. Only resources where
//	    the predicate returns true contribute to the tally. Useful for
//	    distributions over a subset:
//
//	        where: self.is_import
//	        where: self.module_path == "module.platform"
//	        where: contains(["azurerm_subnet", "azurerm_nsg"], self.resource_type)
type RiskHistogram struct{}

func (RiskHistogram) Name() string { return "risk_histogram" }

var riskHistogramColumns = []string{"impact", "count", "bar"}
var riskHistogramHeadings = map[string]string{
	"impact": "Impact",
	"count":  "Count",
	"bar":    "Bar",
}

type riskRow struct {
	label  string
	value  int
	impact core.Impact
}

func (RiskHistogram) Render(ctx *BlockContext, args map[string]any) (string, error) {
	style := ArgString(args, "style", "bar")
	includeNone := ArgBool(args, "include_none", false)
	maxBar := ArgInt(args, "max_bar", 40)

	whereExpr, err := parseWhereArg(args, "risk_histogram")
	if err != nil {
		return "", err
	}

	counts, err := tallyImpacts(ctx, whereExpr)
	if err != nil {
		return "", err
	}

	order := []riskRow{
		{"🔴 critical", counts[core.ImpactCritical], core.ImpactCritical},
		{"🔴 high", counts[core.ImpactHigh], core.ImpactHigh},
		{"🟡 medium", counts[core.ImpactMedium], core.ImpactMedium},
		{"🟢 low", counts[core.ImpactLow], core.ImpactLow},
	}
	if includeNone {
		order = append(order, riskRow{"⚪ none", counts[core.ImpactNone], core.ImpactNone})
	}

	switch style {
	case "inline":
		var parts []string
		for _, b := range order {
			parts = append(parts, fmt.Sprintf("%s %d", firstWord(b.label), b.value))
		}
		return strings.Join(parts, " · "), nil

	case "table":
		// Default columns for table style: impact, count (Bar is bar-only).
		cols := defaultCols(ArgCSV(args, "columns"), []string{"impact", "count"})
		if err := validateColumns("risk_histogram", cols, toSet(riskHistogramColumns)); err != nil {
			return "", err
		}
		return renderRiskRows(order, cols, maxBar), nil

	case "bar":
		cols := defaultCols(ArgCSV(args, "columns"), riskHistogramColumns)
		if err := validateColumns("risk_histogram", cols, toSet(riskHistogramColumns)); err != nil {
			return "", err
		}
		return renderRiskRows(order, cols, maxBar), nil

	default:
		return "", fmt.Errorf("risk_histogram: unknown style %q (valid: bar, table, inline)", style)
	}
}

// renderRiskRows produces the `| Impact | Count | Bar |`-style markdown
// table using just the selected columns.
func renderRiskRows(rows []riskRow, cols []string, maxBar int) string {
	var b strings.Builder
	headings := mapSlice(cols, func(id string) string { return riskHistogramHeadings[id] })
	writeColumnHeader(&b, headings)
	for _, row := range rows {
		b.WriteString("|")
		for _, col := range cols {
			fmt.Fprintf(&b, " %s |", renderRiskCell(row, col, maxBar))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderRiskCell(row riskRow, col string, maxBar int) string {
	switch col {
	case "impact":
		return row.label
	case "count":
		return fmt.Sprintf("%d", row.value)
	case "bar":
		return renderBar(row.value, maxBar)
	}
	return ""
}

// tallyImpacts counts resources by impact across all reports in ctx.
// When whereExpr is set, each resource is evaluated against the
// predicate with `self` bound to the Resource tree node; only true
// evaluations contribute to the tally. whereExpr=nil keeps the
// existing full-tally fast path.
func tallyImpacts(ctx *BlockContext, whereExpr *core.Expr) (map[core.Impact]int, error) {
	counts := map[core.Impact]int{}
	reports := allReports(ctx)
	for _, r := range reports {
		var idx map[string]*core.Node
		if whereExpr != nil {
			// Per-report index so each Resource resolves to the node
			// under its own Report subtree — matters in multi-report
			// where two reports can share a resource address.
			idx = perReportResourceIndex(ctx, r)
		}
		for _, mg := range r.ModuleGroups {
			for _, rc := range mg.Changes {
				if whereExpr != nil {
					keep, err := evalResourceWhere(whereExpr, idx, rc, "risk_histogram")
					if err != nil {
						return nil, err
					}
					if !keep {
						continue
					}
				}
				counts[rc.Impact]++
			}
		}
	}
	return counts, nil
}

// perReportResourceIndex returns a map of address → Resource Node for
// the specific report r. In single-report mode it reuses the ctx's
// subtree; in multi-report mode it walks the Reports root to find r's
// subtree. Falls back to building a standalone tree for r if ctx has
// none bound.
func perReportResourceIndex(ctx *BlockContext, r *core.Report) map[string]*core.Node {
	var root *core.Node
	if ctx.Tree != nil && ctx.Tree.Root != nil {
		switch ctx.Tree.Root.Kind {
		case core.KindReport:
			root = ctx.Tree.Root
		case core.KindReports:
			for _, c := range ctx.Tree.Root.Children {
				if c.Kind != core.KindReport {
					continue
				}
				if payload, ok := c.Payload.(*core.Report); ok && payload == r {
					root = c
					break
				}
			}
		}
	}
	if root == nil {
		root = core.BuildTree(r).Root
	}
	if root == nil {
		return nil
	}
	idx := make(map[string]*core.Node)
	for _, n := range core.Query(root, core.Path{core.KindResource}) {
		idx[n.Name] = n
	}
	return idx
}

// renderBar returns a bar of solid blocks, capped at cap. Uses count directly
// (no normalization) so bars reflect raw magnitude when differences are
// small; large counts are truncated with a "+" marker.
func renderBar(count, cap int) string {
	if count <= 0 {
		return ""
	}
	if count > cap {
		return strings.Repeat("█", cap) + "+"
	}
	return strings.Repeat("█", count)
}

// firstWord returns the first whitespace-separated token of s. Used to
// extract just the emoji from "🔴 critical" for inline-style rendering.
func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

// Doc describes risk_histogram for cmd/docgen.
func (RiskHistogram) Doc() BlockDoc {
	return BlockDoc{
		Name:    "risk_histogram",
		Summary: "Impact-level distribution across all resources in scope. Three visual styles (bar/table/inline one-liner).",
		Args: []ArgDoc{
			{Name: "style", Type: "string", Default: "bar", Description: "One of `bar` (table with unicode bars), `table` (no bar column by default), `inline` (single line)."},
			{Name: "columns", Type: "csv", Default: "(bar: impact,count,bar; table: impact,count)", Description: "Subset of columns for table/bar styles. Ignored for inline."},
			{Name: "include_none", Type: "bool", Default: "false", Description: "Include `impact=none` (no-op) in output."},
			{Name: "max_bar", Type: "int", Default: "40", Description: "Cap bar length; counts above this truncate with a `+` marker."},
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate evaluated per resource (`self` bound to the Resource tree node). Only matching resources contribute to the tally. E.g. `self.is_import`, `contains([\"azurerm_subnet\"], self.resource_type)`."},
		},
		Columns: []ColumnDoc{
			{ID: "impact", Heading: "Impact", Description: "Impact label (emoji + name)."},
			{ID: "count", Heading: "Count", Description: "Resource count at this impact level."},
			{ID: "bar", Heading: "Bar", Description: "Unicode block bar; width == count (capped at max_bar)."},
		},
	}
}

func init() { defaultRegistry.Register(RiskHistogram{}) }
