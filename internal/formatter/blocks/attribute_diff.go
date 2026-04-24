package blocks

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// AttributeDiff renders compact per-attribute diffs — one row per
// ChangedAttribute, across resources selected by the `addresses` filter.
// Fills the gap between text_plan (verbose terraform output) and
// synthetic one-line-per-resource diffs.
//
// Three formats:
//
//	format=table (default) — markdown table with pluggable columns.
//	format=list             — bulleted `- **key**: old → new`.
//	format=inline           — `key(old→new), key2(old→new)` (compact).
//
// Args:
//
//	addresses csv (default "" = all non-read resources)
//	    Restrict to these resource addresses.
//
//	actions csv (default "update,replace")
//	    Filter by action.
//
//	columns csv (default "key,old,new")
//	    Table-mode only. Valid IDs:
//	      key, old, new, description, impact, address, resource_type
//
//	max int (default 0 = unlimited)
//	    Cap total rows; truncated rows collapse into `… N more attributes`.
//
//	truncate int (default 60)
//	    Max characters per before/after cell. Longer values are ellipsized.
//	    Pass 0 to disable truncation.
//
//	where string (default "")
//	    HCL predicate evaluated per attribute with `self` bound to the
//	    Attribute tree node. `self` exposes key, sensitive, computed,
//	    description (plus the shared kind/name/depth fields). Composes
//	    AND with `addresses` and `actions`. Example idioms:
//
//	        where: self.sensitive
//	        where: !self.computed
//	        where: contains(["tags", "location"], self.key)
type AttributeDiff struct{}

func (AttributeDiff) Name() string { return "attribute_diff" }

var attributeDiffColumns = []string{"key", "old", "new", "description", "impact", "address", "resource_type"}
var attributeDiffHeadings = map[string]string{
	"key":           "Attribute",
	"old":           "Before",
	"new":           "After",
	"description":   "Description",
	"impact":        "Impact",
	"address":       "Address",
	"resource_type": "Resource Type",
}

type attrDiffRow struct {
	rc   core.ResourceChange
	attr core.ChangedAttribute
}

func (AttributeDiff) Render(ctx *BlockContext, args map[string]any) (string, error) {
	format := ArgString(args, "format", "table")
	switch format {
	case "table", "list", "inline":
	default:
		return "", fmt.Errorf("attribute_diff: unknown format %q (valid: table, list, inline)", format)
	}

	cols := defaultCols(ArgCSV(args, "columns"), []string{"key", "old", "new"})
	if format == "table" {
		if err := validateColumns("attribute_diff", cols, toSet(attributeDiffColumns)); err != nil {
			return "", err
		}
	}

	addrFilter := toSet(ArgCSV(args, "addresses"))
	actions := parseActionFilter(ArgString(args, "actions", "update,replace"))
	max := ArgInt(args, "max", 0)
	truncate := ArgInt(args, "truncate", 60)

	whereExpr, err := parseWhereArg(args, "attribute_diff")
	if err != nil {
		return "", err
	}

	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	var attrIdx map[string]*core.Node
	if whereExpr != nil {
		attrIdx = attributeNodeIndex(ctx, r)
	}

	var rows []attrDiffRow
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			if _, ok := actions[rc.Action]; !ok {
				continue
			}
			if len(addrFilter) > 0 {
				if _, ok := addrFilter[rc.Address]; !ok {
					continue
				}
			}
			for _, attr := range rc.ChangedAttributes {
				keep, err := evalAttributeWhere(whereExpr, attrIdx, rc, attr, "attribute_diff")
				if err != nil {
					return "", err
				}
				if !keep {
					continue
				}
				rows = append(rows, attrDiffRow{rc: rc, attr: attr})
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
	case "table":
		headings := mapSlice(cols, func(id string) string { return attributeDiffHeadings[id] })
		writeColumnHeader(&b, headings)
		for _, row := range rows {
			b.WriteString("|")
			for _, col := range cols {
				fmt.Fprintf(&b, " %s |", renderAttrDiffCell(ctx, row, col, truncate))
			}
			b.WriteString("\n")
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "\n_... %d more attributes_\n", truncated)
		}
	case "list":
		for _, row := range rows {
			fmt.Fprintf(&b, "- **%s**: %s → %s\n",
				row.attr.Key,
				renderAttrValue(row.attr.OldValue, truncate, false),
				renderAttrValue(row.attr.NewValue, truncate, row.attr.Computed))
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "- _... %d more attributes_\n", truncated)
		}
	case "inline":
		parts := make([]string, 0, len(rows))
		for _, row := range rows {
			parts = append(parts, fmt.Sprintf("%s(%s→%s)",
				row.attr.Key,
				renderAttrValue(row.attr.OldValue, truncate, false),
				renderAttrValue(row.attr.NewValue, truncate, row.attr.Computed)))
		}
		out := strings.Join(parts, ", ")
		if truncated > 0 {
			out += fmt.Sprintf(" ... +%d more", truncated)
		}
		return out, nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func renderAttrDiffCell(ctx *BlockContext, row attrDiffRow, col string, truncate int) string {
	switch col {
	case "key":
		return "`" + row.attr.Key + "`"
	case "old":
		return renderAttrValue(row.attr.OldValue, truncate, false)
	case "new":
		return renderAttrValue(row.attr.NewValue, truncate, row.attr.Computed)
	case "description":
		if row.attr.Description == "" {
			return "—"
		}
		return row.attr.Description
	case "impact":
		return formatImpactWithNote(ctx, row.rc)
	case "address":
		return "`" + row.rc.Address + "`"
	case "resource_type":
		return displayName(ctx, row.rc.ResourceType)
	}
	return ""
}

// renderAttrValue renders a before/after value for a markdown table cell.
// Computed values surface as `(known after apply)`; nil values as `—`;
// other values JSON-encoded (to preserve numeric/boolean/slice structure)
// then truncated.
func renderAttrValue(v any, truncate int, computed bool) string {
	if computed {
		return "(known after apply)"
	}
	if v == nil {
		return "—"
	}
	s := ""
	switch typed := v.(type) {
	case string:
		s = typed
	default:
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprintf("%v", v)
		} else {
			s = string(b)
		}
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	if truncate > 0 && len(s) > truncate {
		s = s[:truncate] + "…"
	}
	return "`" + s + "`"
}

// Doc describes attribute_diff for cmd/docgen.
func (AttributeDiff) Doc() BlockDoc {
	cols := make([]ColumnDoc, 0, len(attributeDiffColumns))
	for _, id := range attributeDiffColumns {
		cols = append(cols, ColumnDoc{
			ID:          id,
			Heading:     attributeDiffHeadings[id],
			Description: attributeDiffColumnDescriptions[id],
		})
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].ID < cols[j].ID })

	return BlockDoc{
		Name:    "attribute_diff",
		Summary: "Per-attribute diff rendering (table/list/inline). Fills the gap between verbose text_plan and one-line synthetic diffs.",
		Args: []ArgDoc{
			{Name: "format", Type: "string", Default: "table", Description: "One of `table`, `list`, `inline`."},
			{Name: "addresses", Type: "csv", Default: "(all non-read)", Description: "Restrict to these resource addresses."},
			{Name: "actions", Type: "csv", Default: "update,replace", Description: "Filter by action."},
			{Name: "columns", Type: "csv", Default: "key,old,new", Description: "Table-mode columns."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap rows; truncation marker `… N more attributes`."},
			{Name: "truncate", Type: "int", Default: "60", Description: "Max characters per before/after cell (0 disables)."},
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate evaluated per attribute with `self` bound to the Attribute tree node (`self.key`, `self.sensitive`, `self.computed`, `self.description`). Composes AND with `addresses` and `actions`. E.g. `self.sensitive`, `!self.computed`, `contains([\"tags\", \"location\"], self.key)`."},
		},
		Columns: cols,
	}
}

var attributeDiffColumnDescriptions = map[string]string{
	"key":           "Attribute name, backticked.",
	"old":           "Before value. `(known after apply)` for computed values; `—` for nil.",
	"new":           "After value, same grammar as Before.",
	"description":   "Attribute description (from preset or config); `—` when unset.",
	"impact":        "Impact of the enclosing resource, with emoji + optional note.",
	"address":       "Full terraform address of the enclosing resource.",
	"resource_type": "Display name for the enclosing resource's type.",
}

func init() { defaultRegistry.Register(AttributeDiff{}) }
