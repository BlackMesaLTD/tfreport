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
// Internally this block collects rows by walking the PlanTree when one is
// bound to ctx — Query("resource") is the enumeration primitive, and the
// filter axes compose against the resulting node slice. When no tree is
// available (unit-test contexts, legacy callers) it falls back to the
// classic ModuleGroups loop so output is byte-exact identical.
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
//	where string (default "")
//	    HCL predicate evaluated per resource with `self` bound to the
//	    current tree node. Composes AND with every other filter — a row
//	    must satisfy both the CSV filters and the predicate. Gives
//	    terraform users their native idiom for complex cases:
//
//	        where = contains(["critical", "high"], self.impact) && !self.is_import
//	        where = self.action == "replace" && count(self.changed_attrs) > 3
//
//	    `self` exposes: kind, name, depth, resource_count, import_count,
//	    max_impact, action_counts, changed_attrs, is_leaf, child_count,
//	    address, module_path, resource_type, resource_name, action,
//	    impact, is_import, display_label.
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

// changedResourcesFilters bundles every filter axis the block supports so
// the tree and legacy collectors can share a single predicate function.
type changedResourcesFilters struct {
	actions       map[core.Action]struct{}
	impact        map[core.Impact]struct{}
	modules       map[string]struct{}
	moduleTypes   map[string]struct{}
	resourceTypes map[string]struct{}
	isImport      string // "true", "false", or ""
	// where is an optional HCL predicate. Applied per resource with
	// `self` bound to the tree node. Composes AND with every CSV filter.
	// nil when the arg was absent.
	where *core.Expr
}

// changedResourcesColumnRename maps the historic changed_resources_table
// column IDs onto the Resource-kind registry's canonical IDs. Most ids
// match 1:1; `impact` maps to `impact_with_note` because the legacy
// block's impact column renders notes inline.
var changedResourcesColumnRename = map[string]string{
	"impact": "impact_with_note",
}

func (ChangedResourcesTable) Render(ctx *BlockContext, args map[string]any) (string, error) {
	cols := defaultCols(ArgCSV(args, "columns"),
		[]string{"resource_type", "name", "changed", "impact"})
	if err := validateColumns("changed_resources_table", cols, toSet(changedResourcesColumns)); err != nil {
		return "", err
	}

	filters := changedResourcesFilters{
		actions:       parseActionFilter(ArgString(args, "actions", "update,delete,replace")),
		impact:        parseImpactFilterSet(ArgCSV(args, "impact")),
		modules:       toCaseInsensitiveSet(ArgCSV(args, "modules")),
		moduleTypes:   toCaseInsensitiveSet(ArgCSV(args, "module_types")),
		resourceTypes: toSet(ArgCSV(args, "resource_types")),
		isImport:      ArgString(args, "is_import", ""),
	}
	if w := ArgString(args, "where", ""); w != "" {
		expr, err := core.ParseExpr(w, "changed_resources_table.where")
		if err != nil {
			return "", fmt.Errorf("changed_resources_table: where: %w", err)
		}
		filters.where = expr
	}
	max := ArgInt(args, "max", 0)

	changedMode := ArgString(args, "changed_attrs_display", "")
	if err := validChangedAttrsMode("changed_resources_table", changedMode); err != nil {
		return "", err
	}

	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	// Build ctx.Tree on-demand so wrapper works in test contexts that
	// don't pre-populate one, matching the modules_table wrapper pattern.
	inner := ctx
	if inner.Tree == nil || inner.Tree.Root == nil {
		cp := *ctx
		cp.Tree = core.BuildTree(r)
		inner = &cp
	}

	// Propagate changed_attrs_display into ctx so the shared `changed`
	// column renderer picks up the mode without extra plumbing.
	if changedMode != "" {
		cp := *inner
		cp.Output.ChangedAttrsDisplay = changedMode
		inner = &cp
	}

	nodes, err := collectChangedResourcesNodes(inner, r, filters)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", nil
	}

	total := len(nodes)
	truncated := 0
	if max > 0 && total > max {
		truncated = total - max
		nodes = nodes[:max]
	}

	// Translate legacy column ids → table canonical ids; carry the
	// legacy heading grammar so callers see identical bytes.
	canonical := make([]string, len(cols))
	headings := map[string]string{}
	for i, id := range cols {
		dst := id
		if rename, ok := changedResourcesColumnRename[id]; ok {
			dst = rename
		}
		canonical[i] = dst
		headings[dst] = changedResourcesHeadings[id]
	}

	tableOut, err := renderTable(inner, nodes, core.KindResource, canonical, tableRenderOpts{
		HeadingOverrides: headings,
		TruncatedCount:   truncated,
		TruncationLine:   fmt.Sprintf("_... %d more resources_", truncated),
	})
	if err != nil {
		return "", err
	}
	if tableOut == "" {
		return "", nil
	}
	return "**Changed resources:**\n\n" + tableOut, nil
}

// collectChangedResourcesNodes walks the tree at ctx.Tree, filters by
// every axis in f, and returns the surviving Resource *core.Nodes in
// tree-walk order. Caller passes them to renderTable directly.
//
// Row ordering matches legacy byte-for-byte: tree walk visits module
// groups in path-sorted order (grouper guarantees this).
func collectChangedResourcesNodes(ctx *BlockContext, r *core.Report, f changedResourcesFilters) ([]*core.Node, error) {
	sub := reportSubtree(ctx)
	if sub == nil {
		return nil, nil
	}
	mgByPath := make(map[string]core.ModuleGroup, len(r.ModuleGroups))
	for _, mg := range r.ModuleGroups {
		mgByPath[mg.Path] = mg
	}

	var out []*core.Node
	for _, n := range core.Query(sub, core.Path{core.KindResource}) {
		rc, ok := n.Payload.(*core.ResourceChange)
		if !ok || rc == nil {
			continue
		}
		mg, ok := mgByPath[mgLookupKey(rc.ModulePath)]
		if !ok {
			mg = core.ModuleGroup{Name: moduleNameFromPath(rc.ModulePath), Path: mgLookupKey(rc.ModulePath)}
		}
		if !changedResourcesRowMatches(*rc, mg, f, r) {
			continue
		}
		if f.where != nil {
			keep, err := core.EvalBool(f.where, n, nil)
			if err != nil {
				return nil, fmt.Errorf("changed_resources_table: where: %w", err)
			}
			if !keep {
				continue
			}
		}
		out = append(out, n)
	}
	return out, nil
}

// changedResourcesRowMatches is the composite predicate used by the tree
// collector — the legacy collector splits this into per-group and
// per-resource halves for early-exit on whole-group filters. Both paths
// apply the same set of checks in the same order; the split exists only
// to match the legacy loop's short-circuit behaviour.
func changedResourcesRowMatches(rc core.ResourceChange, mg core.ModuleGroup, f changedResourcesFilters, r *core.Report) bool {
	if !changedResourcesModuleMatches(mg, f, r) {
		return false
	}
	return changedResourcesResourceMatches(rc, f)
}

func changedResourcesModuleMatches(mg core.ModuleGroup, f changedResourcesFilters, r *core.Report) bool {
	if len(f.modules) > 0 {
		topLevel := core.TopLevelModuleName(mg.Path)
		if !matchesFilter(f.modules, topLevel, mg.Name) {
			return false
		}
	}
	if len(f.moduleTypes) > 0 {
		topLevel := core.TopLevelModuleName(mg.Path)
		modType := core.ResolveModuleType(topLevel, r.ModuleSources, mg.Name)
		if _, ok := f.moduleTypes[strings.ToLower(modType)]; !ok {
			return false
		}
	}
	return true
}

func changedResourcesResourceMatches(rc core.ResourceChange, f changedResourcesFilters) bool {
	if _, ok := f.actions[rc.Action]; !ok {
		return false
	}
	if f.impact != nil {
		if _, ok := f.impact[rc.Impact]; !ok {
			return false
		}
	}
	if len(f.resourceTypes) > 0 {
		if _, ok := f.resourceTypes[rc.ResourceType]; !ok {
			return false
		}
	}
	switch f.isImport {
	case "true":
		if !rc.IsImport {
			return false
		}
	case "false":
		if rc.IsImport {
			return false
		}
	}
	return true
}

// mgLookupKey translates an empty module path to the "(root)" sentinel
// the grouper uses as the ModuleGroup.Path for root-module resources.
func mgLookupKey(modulePath string) string {
	if modulePath == "" {
		return "(root)"
	}
	return modulePath
}

// moduleNameFromPath mirrors the grouper's leaf-segment extraction for
// the fallback case when a resource's ModulePath doesn't resolve to a
// known ModuleGroup. Uses the structured Module value so for_each
// brackets render the same way grouper.moduleName would.
func moduleNameFromPath(path string) string {
	if path == "" {
		return "(root)"
	}
	m := core.ParseModuleAddress(path)
	if m.IsRoot() {
		return "(root)"
	}
	last := m.Last()
	if last.Instance != "" {
		return last.Name + "[" + last.Instance + "]"
	}
	return last.Name
}

// reportSubtree returns the Report node that currentReport(ctx) addresses,
// picked out of the PlanTree. Single-report trees have a KindReport root;
// multi-report trees have a KindReports root whose first child is the
// target. Returns nil when no such node can be found (empty tree or
// non-canonical shape).
func reportSubtree(ctx *BlockContext) *core.Node {
	if ctx.Tree == nil || ctx.Tree.Root == nil {
		return nil
	}
	root := ctx.Tree.Root
	if root.Kind == core.KindReport {
		return root
	}
	for _, c := range root.Children {
		if c.Kind == core.KindReport {
			return c
		}
	}
	return nil
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
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate evaluated per resource (`self` bound to the tree node). Composes AND with the CSV filters. Idiomatic for terraform users — e.g. `contains([\"critical\", \"high\"], self.impact) && !self.is_import`. See `core.NodeValue` for the `self` field set and `core.DefaultFunctions` for registered functions."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap number of rows; truncated rows collapse into `… N more resources`."},
			{Name: "changed_attrs_display", Type: "string", Default: "(cfg.Output.ChangedAttrsDisplay or `dash`)", Description: "Render mode for the `changed` column on create/delete rows: `dash` (—), `wordy` (new/removed), `count` (N attrs), `list` (legacy full keys-list). Update/replace always show backticked keys."},
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
