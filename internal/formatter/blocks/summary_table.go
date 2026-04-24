package blocks

import (
	"fmt"
	"sort"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// SummaryTable renders the top-level resource count table. Five groupings,
// each with its own column registry; the `columns` csv arg picks a subset.
//
//	group="module_type"   — two-level (module source type → instances). Used by
//	                        step-summary. Requires report.ModuleSources.
//	group="module"        — flat per-module rows. Used by markdown / pr-body.
//	group="subscription"  — per-report rows. Used by pr-body / pr-comment when
//	                        multi-report; produces the cross-sub summary.
//	group="action"        — one row per action type.
//	group="resource_type" — one row per resource type.
//
// Args:
//
//	group       string — picks one of the five groupings (default target-dependent).
//	columns     csv    — column subset for the chosen grouping. Default is the
//	                     full column set for that grouping (preserves existing
//	                     output). Unknown IDs return a typed error.
//	hide_empty  bool (default false)  — drop rows with zero non-read resources.
//	max         int  (default 0)      — cap rows. The `action` grouping always
//	                                    shows all five actions (max has no effect).
//	where       string (default "")   — HCL predicate evaluated per resource
//	    with `self` bound to the Resource tree node. Resources that fail the
//	    predicate are excluded from every grouping's aggregation — row counts,
//	    ActionCounts, MaxImpact, and the `subscription` per-report totals all
//	    reflect the filtered set. Empty module groups (whose every resource
//	    was filtered out) are dropped. In multi-report mode the filter applies
//	    independently to each report. Idioms:
//
//	        where: self.is_import
//	        where: contains(["critical", "high"], self.impact)
//	        where: self.resource_type == "azurerm_subnet"
type SummaryTable struct{}

func (SummaryTable) Name() string { return "summary_table" }

func (SummaryTable) Render(ctx *BlockContext, args map[string]any) (string, error) {
	group := ArgString(args, "group", defaultSummaryGroup(ctx))
	hideEmpty := ArgBool(args, "hide_empty", false)
	max := ArgInt(args, "max", 0)
	columns := ArgCSV(args, "columns")

	whereExpr, err := parseWhereArg(args, "summary_table")
	if err != nil {
		return "", err
	}
	if whereExpr != nil {
		// Fold the predicate into a derived report (or reports, in multi
		// mode) so the downstream grouping renderers don't need to know
		// about where= at all. Preserves byte-exact output for the
		// where-absent path; the derived report has recomputed
		// ActionCounts/TotalResources/MaxImpact reflecting only the
		// surviving resources.
		derived, err := deriveFilteredCtx(ctx, whereExpr)
		if err != nil {
			return "", err
		}
		ctx = derived
	}

	switch group {
	case "module_type":
		return renderModuleTypeTable(ctx, columns, hideEmpty, max)
	case "module":
		return renderModuleTable(ctx, columns, hideEmpty, max)
	case "subscription":
		return renderSubscriptionTable(ctx, columns, hideEmpty, max)
	case "action":
		return renderActionTable(ctx, columns, hideEmpty)
	case "resource_type":
		return renderResourceTypeTable(ctx, columns, hideEmpty, max)
	default:
		return "", fmt.Errorf("summary_table: unknown group %q (valid: module, module_type, subscription, action, resource_type)", group)
	}
}

// --- action grouping ---

type actionRow struct {
	action core.Action
	count  int
}

var summaryActionColumns = []string{"action", "count", "impact"}
var summaryActionHeadings = map[string]string{
	"action": "Action",
	"count":  "Count",
	"impact": "Impact",
}

func renderActionRow(row actionRow, col string) string {
	switch col {
	case "action":
		return fmt.Sprintf("%s %s", core.ActionEmoji(row.action), row.action)
	case "count":
		return fmt.Sprintf("%d", row.count)
	case "impact":
		if row.count == 0 {
			return "—"
		}
		return defaultActionImpact(row.action)
	}
	return ""
}

// renderActionTable produces a table with one row per action type.
// Action grouping aggregates tree resources by their Action enum; the
// resulting rows aren't tree nodes, so we build them ourselves and
// delegate byte assembly to renderMarkdownTable for uniformity.
func renderActionTable(ctx *BlockContext, columns []string, hideEmpty bool) (string, error) {
	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	cols := defaultCols(columns, summaryActionColumns)
	if err := validateColumns("summary_table", cols, toSet(summaryActionColumns)); err != nil {
		return "", err
	}

	order := []core.Action{core.ActionCreate, core.ActionUpdate, core.ActionDelete, core.ActionReplace, core.ActionRead}
	rows := make([]actionRow, 0, len(order))
	for _, a := range order {
		c := r.ActionCounts[a]
		if hideEmpty && c == 0 {
			continue
		}
		rows = append(rows, actionRow{action: a, count: c})
	}

	headings := mapSlice(cols, func(id string) string { return summaryActionHeadings[id] })
	return renderMarkdownTable(len(rows), headings, cols, func(i int, col string) string {
		return renderActionRow(rows[i], col)
	}, tableRenderOpts{}), nil
}

// --- resource_type grouping ---

type resourceTypeRow struct {
	typeName string
	count    int
	actions  map[core.Action]int
	imports  int
}

var summaryResourceTypeColumns = []string{"resource_type", "count", "actions"}
var summaryResourceTypeHeadings = map[string]string{
	"resource_type": "Resource Type",
	"count":         "Count",
	"actions":       "Actions",
}

func renderResourceTypeRow(ctx *BlockContext, row *resourceTypeRow, col string) string {
	switch col {
	case "resource_type":
		name := displayName(ctx, row.typeName)
		return fmt.Sprintf("%s (`%s`)", name, row.typeName)
	case "count":
		return fmt.Sprintf("%d", row.count)
	case "actions":
		return describeActions(row.actions, row.imports)
	}
	return ""
}

// renderResourceTypeTable produces a table with one row per resource type,
// aggregated across all module groups. Uses display names when available.
func renderResourceTypeTable(ctx *BlockContext, columns []string, hideEmpty bool, max int) (string, error) {
	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	cols := defaultCols(columns, summaryResourceTypeColumns)
	if err := validateColumns("summary_table", cols, toSet(summaryResourceTypeColumns)); err != nil {
		return "", err
	}

	rows := map[string]*resourceTypeRow{}
	var order []string
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			rr, ok := rows[rc.ResourceType]
			if !ok {
				rr = &resourceTypeRow{typeName: rc.ResourceType, actions: map[core.Action]int{}}
				rows[rc.ResourceType] = rr
				order = append(order, rc.ResourceType)
			}
			rr.count++
			rr.actions[rc.Action]++
			if rc.IsImport {
				rr.imports++
			}
		}
	}
	sort.Strings(order)

	kept := make([]string, 0, len(order))
	for _, t := range order {
		rr := rows[t]
		if hideEmpty && rr.count == 0 {
			continue
		}
		kept = append(kept, t)
	}
	total := len(kept)
	truncated := false
	if max > 0 && total > max {
		kept = kept[:max]
		truncated = true
	}

	headings := mapSlice(cols, func(id string) string { return summaryResourceTypeHeadings[id] })
	opts := tableRenderOpts{}
	if truncated {
		opts.TruncatedCount = total - max
		opts.TruncationLine = fmt.Sprintf("_... %d more resource types_", opts.TruncatedCount)
	}
	return renderMarkdownTable(len(kept), headings, cols, func(i int, col string) string {
		return renderResourceTypeRow(ctx, rows[kept[i]], col)
	}, opts), nil
}

// describeActions renders the Actions cell for a resource-type row. When all
// tracked changes are imports/no-ops, substitutes a descriptive label
// ("♻️ N import-only") instead of leaving the cell blank.
func describeActions(actions map[core.Action]int, imports int) string {
	if breakdown := actionBreakdownEmoji(actions); breakdown != "" {
		if imports > 0 {
			return fmt.Sprintf("%s · ♻️ %d imported", breakdown, imports)
		}
		return breakdown
	}
	if imports > 0 {
		return fmt.Sprintf("♻️ %d import-only", imports)
	}
	if n := actions[core.ActionNoOp]; n > 0 {
		return fmt.Sprintf("— %d no-op", n)
	}
	return "—"
}

// defaultActionImpact returns the natural impact for an action (used in the
// action summary table). This is informational, not a substitute for per-
// resource impact resolution.
func defaultActionImpact(a core.Action) string {
	switch a {
	case core.ActionReplace:
		return "🔴 critical"
	case core.ActionDelete:
		return "🔴 high"
	case core.ActionUpdate:
		return "🟡 medium"
	case core.ActionCreate, core.ActionRead:
		return "🟢 low"
	default:
		return "—"
	}
}

func defaultSummaryGroup(ctx *BlockContext) string {
	if len(ctx.Reports) > 1 {
		return "subscription"
	}
	if ctx.Target == "github-step-summary" {
		r := currentReport(ctx)
		if r != nil && len(r.ModuleSources) > 0 {
			return "module_type"
		}
	}
	return "module"
}

// --- module_type grouping ---

type moduleTypeRow struct {
	typeName     string
	description  string
	instances    map[string]struct{}
	total        int
	read         int
	actionCounts map[core.Action]int
	maxImpact    core.Impact
}

var summaryModuleTypeColumns = []string{"module_type", "description", "instances", "resources", "actions"}
var summaryModuleTypeHeadings = map[string]string{
	"module_type": "Module Type",
	"description": "Description",
	"instances":   "Instances",
	"resources":   "Resources",
	"actions":     "Actions",
}

func renderModuleTypeRow(row *moduleTypeRow, col string) string {
	switch col {
	case "module_type":
		return row.typeName
	case "description":
		if row.description == "" {
			return "—"
		}
		return row.description
	case "instances":
		return fmt.Sprintf("%d", len(row.instances))
	case "resources":
		return fmt.Sprintf("%d", row.total-row.read)
	case "actions":
		return actionBreakdownEmoji(row.actionCounts)
	}
	return ""
}

// renderModuleTypeTable produces the two-level module-type summary
// (step-summary format).
func renderModuleTypeTable(ctx *BlockContext, columns []string, hideEmpty bool, max int) (string, error) {
	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	rowsByType := make(map[string]*moduleTypeRow)
	var order []string

	for _, mg := range r.ModuleGroups {
		topLevel := core.TopLevelModuleName(mg.Path)
		tname := core.ResolveModuleType(topLevel, r.ModuleSources, mg.Name)

		rr, ok := rowsByType[tname]
		if !ok {
			rr = &moduleTypeRow{
				typeName:     tname,
				instances:    map[string]struct{}{},
				actionCounts: map[core.Action]int{},
			}
			rowsByType[tname] = rr
			order = append(order, tname)
		}

		if rr.description == "" {
			if desc := ctx.ModuleTypeDescriptions[tname]; desc != "" {
				rr.description = desc
			} else if mg.Description != "" {
				rr.description = mg.Description
			}
		}

		inst := topLevel
		if inst == "" {
			inst = mg.Name
		}
		rr.instances[inst] = struct{}{}

		for a, c := range mg.ActionCounts {
			if a == core.ActionRead {
				rr.read += c
			}
			rr.actionCounts[a] += c
			rr.total += c
		}

		if imp := core.MaxImpactForGroup(mg); core.ImpactSeverity(imp) > core.ImpactSeverity(rr.maxImpact) {
			rr.maxImpact = imp
		}
	}

	var rows []*moduleTypeRow
	for _, t := range order {
		rr := rowsByType[t]
		if hideEmpty && rr.total-rr.read == 0 {
			continue
		}
		rows = append(rows, rr)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		si := core.ImpactSeverity(rows[i].maxImpact)
		sj := core.ImpactSeverity(rows[j].maxImpact)
		if si != sj {
			return si > sj
		}
		return rows[i].typeName < rows[j].typeName
	})

	total := len(rows)
	truncated := false
	if max > 0 && total > max {
		rows = rows[:max]
		truncated = true
	}

	// Preserve existing behavior: when no row has a description, auto-hide
	// the description column unless the caller explicitly asked for it.
	explicit := len(columns) > 0
	hasDesc := false
	for _, rr := range rows {
		if rr.description != "" {
			hasDesc = true
			break
		}
	}

	cols := defaultCols(columns, summaryModuleTypeColumns)
	if err := validateColumns("summary_table", cols, toSet(summaryModuleTypeColumns)); err != nil {
		return "", err
	}
	if !explicit && !hasDesc {
		cols = removeColumn(cols, "description")
	}

	headings := mapSlice(cols, func(id string) string { return summaryModuleTypeHeadings[id] })
	opts := tableRenderOpts{}
	if truncated {
		opts.TruncatedCount = total - max
		opts.TruncationLine = fmt.Sprintf("_... %d more module types_", opts.TruncatedCount)
	}
	return renderMarkdownTable(len(rows), headings, cols, func(i int, col string) string {
		return renderModuleTypeRow(rows[i], col)
	}, opts), nil
}

// --- module grouping ---

var summaryModuleColumns = []string{"module", "resources", "actions"}
var summaryModuleHeadings = map[string]string{
	"module":    "Module",
	"resources": "Resources",
	"actions":   "Actions",
}

func renderModuleRow(mg core.ModuleGroup, col string) string {
	switch col {
	case "module":
		return mg.Name
	case "resources":
		return fmt.Sprintf("%d", len(mg.Changes))
	case "actions":
		return actionSummaryLine(mg.ActionCounts)
	}
	return ""
}

// renderModuleTable produces a flat per-module table (pr-body / markdown).
func renderModuleTable(ctx *BlockContext, columns []string, hideEmpty bool, max int) (string, error) {
	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	cols := defaultCols(columns, summaryModuleColumns)
	if err := validateColumns("summary_table", cols, toSet(summaryModuleColumns)); err != nil {
		return "", err
	}

	kept := make([]core.ModuleGroup, 0, len(r.ModuleGroups))
	for _, mg := range r.ModuleGroups {
		if hideEmpty && len(mg.Changes) == 0 {
			continue
		}
		kept = append(kept, mg)
	}
	total := len(kept)
	truncated := false
	if max > 0 && total > max {
		kept = kept[:max]
		truncated = true
	}

	headings := mapSlice(cols, func(id string) string { return summaryModuleHeadings[id] })
	opts := tableRenderOpts{}
	if truncated {
		opts.TruncatedCount = total - max
		opts.TruncationLine = fmt.Sprintf("_... %d more modules_", opts.TruncatedCount)
	}
	return renderMarkdownTable(len(kept), headings, cols, func(i int, col string) string {
		return renderModuleRow(kept[i], col)
	}, opts), nil
}

// --- subscription grouping ---

// Target-dependent column sets: pr-comment uses compact
// Add|Update|Delete|Replace columns; others use Resources + Impact + Actions.

var summarySubscriptionDefaultColumns = []string{"subscription", "resources", "impact", "actions"}
var summarySubscriptionPRCommentColumns = []string{"subscription", "impact", "add", "update", "delete", "replace"}
var summarySubscriptionHeadings = map[string]string{
	"subscription": "Subscription",
	"resources":    "Resources",
	"impact":       "Impact",
	"actions":      "Actions",
	"add":          "Add",
	"update":       "Update",
	"delete":       "Delete",
	"replace":      "Replace",
}

func renderSubscriptionRow(r *core.Report, col string) string {
	switch col {
	case "subscription":
		return reportLabel(r)
	case "resources":
		return fmt.Sprintf("%d", r.TotalResources)
	case "impact":
		// pr-comment shows plain "high"; other targets add emoji.
		// We don't have ctx here; use the presence/absence decided by the
		// subscription renderer via column choice (caller picks the column
		// set appropriate to target).
		return fmt.Sprintf("%s %s", core.ImpactEmoji(r.MaxImpact), r.MaxImpact)
	case "impact_plain":
		return string(r.MaxImpact)
	case "actions":
		return actionSummaryLine(r.ActionCounts)
	case "add":
		return fmt.Sprintf("%d", r.ActionCounts[core.ActionCreate])
	case "update":
		return fmt.Sprintf("%d", r.ActionCounts[core.ActionUpdate])
	case "delete":
		return fmt.Sprintf("%d", r.ActionCounts[core.ActionDelete])
	case "replace":
		return fmt.Sprintf("%d", r.ActionCounts[core.ActionReplace])
	}
	return ""
}

// renderSubscriptionTable produces a per-subscription cross-report table
// (pr-body / pr-comment in multi mode).
func renderSubscriptionTable(ctx *BlockContext, columns []string, hideEmpty bool, max int) (string, error) {
	reports := allReports(ctx)
	if len(reports) == 0 {
		return "", nil
	}

	// Default column set depends on target: pr-comment uses compact matrix
	// (add|update|delete|replace), others use Resources|Impact|Actions.
	def := summarySubscriptionDefaultColumns
	prComment := ctx.Target == "github-pr-comment"
	if prComment {
		def = summarySubscriptionPRCommentColumns
	}
	cols := defaultCols(columns, def)

	validSet := toSet(append([]string{},
		"subscription", "resources", "impact", "actions",
		"add", "update", "delete", "replace",
	))
	if err := validateColumns("summary_table", cols, validSet); err != nil {
		return "", err
	}

	kept := make([]*core.Report, 0, len(reports))
	for _, r := range reports {
		if hideEmpty && r.TotalResources == 0 {
			continue
		}
		kept = append(kept, r)
	}
	total := len(kept)
	truncated := false
	if max > 0 && total > max {
		kept = kept[:max]
		truncated = true
	}

	headings := mapSlice(cols, func(id string) string { return summarySubscriptionHeadings[id] })
	opts := tableRenderOpts{}
	if truncated {
		opts.TruncatedCount = total - max
		opts.TruncationLine = fmt.Sprintf("_... %d more subscriptions_", opts.TruncatedCount)
	}
	return renderMarkdownTable(len(kept), headings, cols, func(i int, col string) string {
		// pr-comment's 'impact' column historically dropped the emoji.
		if prComment && col == "impact" {
			return renderSubscriptionRow(kept[i], "impact_plain")
		}
		return renderSubscriptionRow(kept[i], col)
	}, opts), nil
}

// deriveFilteredCtx produces a BlockContext whose Report (single mode)
// or Reports (multi) have been rebuilt with only the resources that
// pass the where predicate. ModuleGroups are pruned (empty groups
// dropped), per-group and per-report ActionCounts are recomputed, and
// TotalResources/MaxImpact reflect the filtered set. The caller's
// context is returned with swapped Report/Reports fields; the rest of
// the context (Target, Tree, DisplayNames, etc.) is preserved so
// downstream renderers behave identically.
//
// Returning a filtered ctx lets every grouping renderer stay unchanged
// — the where= surface is strictly additive at the top of Render.
func deriveFilteredCtx(ctx *BlockContext, whereExpr *core.Expr) (*BlockContext, error) {
	out := *ctx
	if ctx.Report != nil {
		fr, err := filteredReport(ctx, ctx.Report, whereExpr)
		if err != nil {
			return nil, err
		}
		out.Report = fr
	}
	if len(ctx.Reports) > 0 {
		filteredReports := make([]*core.Report, 0, len(ctx.Reports))
		for _, r := range ctx.Reports {
			fr, err := filteredReport(ctx, r, whereExpr)
			if err != nil {
				return nil, err
			}
			filteredReports = append(filteredReports, fr)
		}
		out.Reports = filteredReports
	}
	return &out, nil
}

// filteredReport returns a shallow-copied report with Changes filtered
// by whereExpr and derived aggregates (ActionCounts, TotalResources,
// MaxImpact) recomputed. ModuleGroups retain their Name/Path/
// Description/Module — only Changes and ActionCounts are rebuilt.
// Empty module groups (zero surviving resources) are dropped.
func filteredReport(ctx *BlockContext, r *core.Report, whereExpr *core.Expr) (*core.Report, error) {
	idx := perReportResourceIndex(ctx, r)

	newGroups := make([]core.ModuleGroup, 0, len(r.ModuleGroups))
	overallActionCounts := map[core.Action]int{}
	var overallImpact core.Impact
	totalNonRead := 0

	for _, mg := range r.ModuleGroups {
		keptChanges := make([]core.ResourceChange, 0, len(mg.Changes))
		groupActionCounts := map[core.Action]int{}
		for _, rc := range mg.Changes {
			keep, err := evalResourceWhere(whereExpr, idx, rc, "summary_table")
			if err != nil {
				return nil, err
			}
			if !keep {
				continue
			}
			keptChanges = append(keptChanges, rc)
			groupActionCounts[rc.Action]++
			overallActionCounts[rc.Action]++
			if rc.Action != core.ActionRead {
				totalNonRead++
			}
			if core.ImpactSeverity(rc.Impact) > core.ImpactSeverity(overallImpact) {
				overallImpact = rc.Impact
			}
		}
		if len(keptChanges) == 0 {
			continue
		}
		newMg := mg
		newMg.Changes = keptChanges
		newMg.ActionCounts = groupActionCounts
		newGroups = append(newGroups, newMg)
	}

	filtered := *r
	filtered.ModuleGroups = newGroups
	filtered.ActionCounts = overallActionCounts
	filtered.TotalResources = totalNonRead
	filtered.MaxImpact = overallImpact
	return &filtered, nil
}

// --- small local helpers ---

func toSet(keys []string) map[string]struct{} {
	s := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		s[k] = struct{}{}
	}
	return s
}

func mapSlice[T any, U any](xs []T, fn func(T) U) []U {
	out := make([]U, len(xs))
	for i, x := range xs {
		out[i] = fn(x)
	}
	return out
}

func removeColumn(cols []string, name string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c != name {
			out = append(out, c)
		}
	}
	return out
}

// Doc describes summary_table for cmd/docgen. Columns are listed flat with
// group annotations in Description.
func (SummaryTable) Doc() BlockDoc {
	cols := []ColumnDoc{
		{ID: "action", Heading: "Action", Description: "group=action: the terraform action with emoji."},
		{ID: "actions", Heading: "Actions", Description: "group=module,module_type,resource_type,subscription: action-breakdown summary string."},
		{ID: "add", Heading: "Add", Description: "group=subscription (pr-comment default): create count."},
		{ID: "count", Heading: "Count", Description: "group=action,resource_type: resource count for the row."},
		{ID: "delete", Heading: "Delete", Description: "group=subscription (pr-comment default): delete count."},
		{ID: "description", Heading: "Description", Description: "group=module_type: team-supplied module description (auto-hidden when empty across all rows)."},
		{ID: "impact", Heading: "Impact", Description: "group=action,subscription: natural/worst impact for the row."},
		{ID: "instances", Heading: "Instances", Description: "group=module_type: count of distinct top-level module instances."},
		{ID: "module", Heading: "Module", Description: "group=module: the module call name."},
		{ID: "module_type", Heading: "Module Type", Description: "group=module_type: resolved module type from source URL."},
		{ID: "replace", Heading: "Replace", Description: "group=subscription (pr-comment default): replace count."},
		{ID: "resource_type", Heading: "Resource Type", Description: "group=resource_type: display name + raw type in backticks."},
		{ID: "resources", Heading: "Resources", Description: "group=module,module_type,subscription: non-read resource count."},
		{ID: "subscription", Heading: "Subscription", Description: "group=subscription: the report label."},
		{ID: "update", Heading: "Update", Description: "group=subscription (pr-comment default): update count."},
	}
	return BlockDoc{
		Name:    "summary_table",
		Summary: "Top-level resource-count table with five grouping modes. Each grouping accepts its own subset of columns via the `columns` csv arg (defaults to the full column set for the grouping).",
		Args: []ArgDoc{
			{Name: "group", Type: "string", Default: "(target-dependent)", Description: "One of `module`, `module_type`, `subscription`, `action`, `resource_type`."},
			{Name: "columns", Type: "csv", Default: "(full set for the grouping)", Description: "Column subset to render. Valid IDs depend on `group` — see Columns below."},
			{Name: "hide_empty", Type: "bool", Default: "false", Description: "Drop rows with zero non-read resources."},
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap number of rows. No effect for `group=action`."},
			{Name: "where", Type: "string", Default: "", Description: "HCL predicate evaluated per resource (`self` bound to the Resource tree node). Resources failing the predicate are excluded from every grouping's aggregation — counts, ActionCounts, MaxImpact and per-report totals reflect the filtered set. Modules whose every resource was filtered out disappear. In multi-report mode the filter applies independently to each report. E.g. `self.is_import`, `contains([\"critical\",\"high\"], self.impact)`."},
		},
		Columns: cols,
		Examples: []ExampleDoc{
			{
				Template: `{{ summary_table "group" "module" "columns" "module,resources" }}`,
				Rendered: "| Module | Resources |\n|---|---|\n| vnet | 4 |\n| nsg | 3 |",
			},
		},
	}
}

func init() { defaultRegistry.Register(SummaryTable{}) }
