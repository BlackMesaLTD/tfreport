package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// tableColumn is a render contract: given the BlockContext and a tree
// node of the registered kind, return one markdown-table cell's worth
// of text. Renderers must not emit pipes — callers trust them to be
// cell-safe.
type tableColumn struct {
	Heading     string
	Description string
	Render      func(ctx *BlockContext, n *core.Node) string
}

// tableColumns is the per-kind column registry. Blocks that pluck
// columns by id for a given row kind consult this map. Column ids are
// case-sensitive — they line up 1:1 with the `columns="a,b,c"` arg.
var tableColumns = map[core.NodeKind]map[string]tableColumn{
	core.KindResource:       resourceColumns(),
	core.KindAttribute:      attributeColumns(),
	core.KindKeyChange:      keyChangeColumns(),
	core.KindModuleInstance: moduleInstanceColumns(),
	core.KindReport:         reportColumns(),
}

// tableDefaultColumns is the fallback column order when the caller
// omits `columns`. Defined per kind; new kinds must register an entry
// here or the table block will error.
var tableDefaultColumns = map[core.NodeKind][]string{
	core.KindResource:       {"address", "action", "impact"},
	core.KindAttribute:      {"key", "description"},
	core.KindKeyChange:      {"text", "impact"},
	core.KindModuleInstance: {"module", "resources", "actions"},
	core.KindReport:         {"subscription", "resources", "impact", "actions"},
}

// toColumnSet returns the valid-id set for validateColumns.
func toColumnSet(kind core.NodeKind) map[string]struct{} {
	src := tableColumns[kind]
	out := make(map[string]struct{}, len(src))
	for id := range src {
		out[id] = struct{}{}
	}
	return out
}

// --- Resource columns ---

func resourceColumns() map[string]tableColumn {
	return map[string]tableColumn{
		"address": {
			Heading:     "Resource",
			Description: "Full terraform address, rendered as `inline code`.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return fmt.Sprintf("`%s`", n.Name)
				}
				return fmt.Sprintf("`%s`", rc.Address)
			},
		},
		"resource_type": {
			Heading:     "Type",
			Description: "Terraform resource type (e.g. `azurerm_subnet`).",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				return fmt.Sprintf("`%s`", rc.ResourceType)
			},
		},
		"resource_name": {
			Heading:     "Name",
			Description: "Terraform resource local name.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				return rc.ResourceName
			},
		},
		"module_path": {
			Heading:     "Module",
			Description: "Module address as terraform prints it (empty for root).",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil || rc.ModulePath == "" {
					return "(root)"
				}
				return rc.ModulePath
			},
		},
		"action": {
			Heading:     "Action",
			Description: "Emoji + lowercase action name (create, update, delete, replace, read).",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				emoji := core.ActionEmoji(rc.Action)
				if emoji == "" {
					return string(rc.Action)
				}
				return fmt.Sprintf("%s %s", emoji, rc.Action)
			},
		},
		"impact": {
			Heading:     "Impact",
			Description: "Emoji + lowercase impact level (critical, high, medium, low, none).",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				emoji := core.ImpactEmoji(rc.Impact)
				if emoji == "" {
					return string(rc.Impact)
				}
				return fmt.Sprintf("%s %s", emoji, rc.Impact)
			},
		},
		"is_import": {
			Heading:     "Import",
			Description: "`yes` when the resource is being imported, blank otherwise.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil || !rc.IsImport {
					return ""
				}
				return "yes"
			},
		},
		"changed_attrs": {
			Heading:     "Changed",
			Description: "Comma-joined list of changed attribute keys. Empty dash when the resource has none.",
			Render: func(_ *BlockContext, n *core.Node) string {
				if len(n.Agg.ChangedAttrs) == 0 {
					return "—"
				}
				return strings.Join(n.Agg.ChangedAttrs, ", ")
			},
		},
		"display_label": {
			Heading:     "Label",
			Description: "Pre-computed display label (resource name or `name` attr) — empty for resources without one.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				return rc.DisplayLabel
			},
		},
	}
}

// --- Attribute columns ---

func attributeColumns() map[string]tableColumn {
	return map[string]tableColumn{
		"key": {
			Heading:     "Attribute",
			Description: "The attribute key, rendered inline-code.",
			Render: func(_ *BlockContext, n *core.Node) string {
				a, _ := n.Payload.(*core.ChangedAttribute)
				if a == nil {
					return fmt.Sprintf("`%s`", n.Name)
				}
				return fmt.Sprintf("`%s`", a.Key)
			},
		},
		"sensitive": {
			Heading:     "Sensitive",
			Description: "`yes` when terraform flagged the attribute sensitive.",
			Render: func(_ *BlockContext, n *core.Node) string {
				a, _ := n.Payload.(*core.ChangedAttribute)
				if a == nil || !a.Sensitive {
					return ""
				}
				return "yes"
			},
		},
		"computed": {
			Heading:     "Computed",
			Description: "`yes` when the new value is known-after-apply.",
			Render: func(_ *BlockContext, n *core.Node) string {
				a, _ := n.Payload.(*core.ChangedAttribute)
				if a == nil || !a.Computed {
					return ""
				}
				return "yes"
			},
		},
		"description": {
			Heading:     "Description",
			Description: "Human-readable attribute description (preset-sourced; blank when none).",
			Render: func(_ *BlockContext, n *core.Node) string {
				a, _ := n.Payload.(*core.ChangedAttribute)
				if a == nil {
					return ""
				}
				return a.Description
			},
		},
	}
}

// --- ModuleInstance columns (legacy modules_table equivalent) ---

func moduleInstanceColumns() map[string]tableColumn {
	return map[string]tableColumn{
		"module": {
			Heading:     "Module",
			Description: "Leaf module-call name with instance bracket when present, backticked. Equivalent to legacy `modules_table` `module` column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				leaf := moduleInstanceLeafName(n)
				if leaf == "" {
					return "(root)"
				}
				return "`" + leaf + "`"
			},
		},
		"module_path": {
			Heading:     "Module path",
			Description: "Full terraform module address (e.g. `module.platform.module.vnet`), backticked.",
			Render: func(_ *BlockContext, n *core.Node) string {
				path := moduleInstancePath(n)
				if path == "" {
					return "(root)"
				}
				return "`" + path + "`"
			},
		},
		"module_type": {
			Heading:     "Module type",
			Description: "Resolved module type from the outermost call's source URL; preset-aware, backticked.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				r := enclosingReport(n)
				top := moduleInstanceTopLevel(n)
				var sources map[string]string
				var fallback string
				if r != nil {
					sources = r.ModuleSources
				}
				fallback = moduleInstanceLeafName(n)
				mt := core.ResolveModuleType(top, sources, fallback)
				if mt == "" {
					return ""
				}
				return "`" + mt + "`"
			},
		},
		"description": {
			Heading:     "Description",
			Description: "Module type description from `module_descriptions_file` or preset; blank when unset.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				r := enclosingReport(n)
				top := moduleInstanceTopLevel(n)
				var sources map[string]string
				if r != nil {
					sources = r.ModuleSources
				}
				mt := core.ResolveModuleType(top, sources, moduleInstanceLeafName(n))
				if ctx != nil && ctx.ModuleTypeDescriptions != nil {
					if d := ctx.ModuleTypeDescriptions[mt]; d != "" {
						return d
					}
				}
				return ""
			},
		},
		"resources": {
			Heading:     "Resources",
			Description: "Count of resource descendants under this instance (pre-rolled from the tree).",
			Render: func(_ *BlockContext, n *core.Node) string {
				return fmt.Sprintf("%d", n.Agg.ResourceCount)
			},
		},
		"actions": {
			Heading:     "Actions",
			Description: "Canonical action summary line (e.g. `2 update, 1 create`).",
			Render: func(_ *BlockContext, n *core.Node) string {
				return moduleInstanceActionSummary(n)
			},
		},
		"impact": {
			Heading:     "Impact",
			Description: "Worst impact across the instance, with emoji.",
			Render: func(_ *BlockContext, n *core.Node) string {
				imp := n.Agg.MaxImpact
				if imp == "" {
					return ""
				}
				return core.ImpactEmoji(imp) + " " + string(imp)
			},
		},
		"changed_attrs": {
			Heading:     "Changed attributes",
			Description: "Union of changed attribute keys across the instance's resources, backticked, comma-joined.",
			Render: func(_ *BlockContext, n *core.Node) string {
				if len(n.Agg.ChangedAttrs) == 0 {
					return "—"
				}
				parts := make([]string, len(n.Agg.ChangedAttrs))
				for i, k := range n.Agg.ChangedAttrs {
					parts[i] = "`" + k + "`"
				}
				return strings.Join(parts, ", ")
			},
		},
	}
}

// --- Report columns (legacy summary_table subscription grouping equivalent) ---

func reportColumns() map[string]tableColumn {
	return map[string]tableColumn{
		"subscription": {
			Heading:     "Subscription",
			Description: "Report label or `default` when unlabelled.",
			Render: func(_ *BlockContext, n *core.Node) string {
				r, _ := n.Payload.(*core.Report)
				return reportLabel(r)
			},
		},
		"resources": {
			Heading:     "Resources",
			Description: "`*core.Report.TotalResources` (non-read resource count).",
			Render: func(_ *BlockContext, n *core.Node) string {
				if r, ok := n.Payload.(*core.Report); ok && r != nil {
					return fmt.Sprintf("%d", r.TotalResources)
				}
				return ""
			},
		},
		"impact": {
			Heading:     "Impact",
			Description: "Worst impact for the report with emoji prefix.",
			Render: func(_ *BlockContext, n *core.Node) string {
				r, _ := n.Payload.(*core.Report)
				if r == nil || r.MaxImpact == "" {
					return ""
				}
				return core.ImpactEmoji(r.MaxImpact) + " " + string(r.MaxImpact)
			},
		},
		"impact_plain": {
			Heading:     "Impact",
			Description: "Same as `impact` but without emoji — matches pr-comment's compact matrix grammar.",
			Render: func(_ *BlockContext, n *core.Node) string {
				r, _ := n.Payload.(*core.Report)
				if r == nil {
					return ""
				}
				return string(r.MaxImpact)
			},
		},
		"actions": {
			Heading:     "Actions",
			Description: "Canonical action-summary line (e.g. `2 create, 1 update, 1 delete`).",
			Render: func(_ *BlockContext, n *core.Node) string {
				r, _ := n.Payload.(*core.Report)
				if r == nil {
					return ""
				}
				return actionSummaryLine(r.ActionCounts)
			},
		},
		"add": {
			Heading:     "Add",
			Description: "create count — mirrors summary_table's pr-comment compact column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				return reportActionCount(n, core.ActionCreate)
			},
		},
		"update": {
			Heading:     "Update",
			Description: "update count — mirrors summary_table's pr-comment compact column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				return reportActionCount(n, core.ActionUpdate)
			},
		},
		"delete": {
			Heading:     "Delete",
			Description: "delete count — mirrors summary_table's pr-comment compact column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				return reportActionCount(n, core.ActionDelete)
			},
		},
		"replace": {
			Heading:     "Replace",
			Description: "replace count — mirrors summary_table's pr-comment compact column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				return reportActionCount(n, core.ActionReplace)
			},
		},
		"changed_attrs": {
			Heading:     "Changed attributes",
			Description: "Union of changed attribute keys across every resource in the report, backticked.",
			Render: func(_ *BlockContext, n *core.Node) string {
				if len(n.Agg.ChangedAttrs) == 0 {
					return "—"
				}
				parts := make([]string, len(n.Agg.ChangedAttrs))
				for i, k := range n.Agg.ChangedAttrs {
					parts[i] = "`" + k + "`"
				}
				return strings.Join(parts, ", ")
			},
		},
	}
}

func reportActionCount(n *core.Node, action core.Action) string {
	r, _ := n.Payload.(*core.Report)
	if r == nil {
		return "0"
	}
	return fmt.Sprintf("%d", r.ActionCounts[action])
}

// --- KeyChange columns ---

func keyChangeColumns() map[string]tableColumn {
	return map[string]tableColumn{
		"text": {
			Heading:     "Change",
			Description: "Plain-English summary sentence from the summarizer.",
			Render: func(_ *BlockContext, n *core.Node) string {
				kc, _ := n.Payload.(*core.KeyChange)
				if kc == nil {
					return n.Name
				}
				return kc.Text
			},
		},
		"impact": {
			Heading:     "Impact",
			Description: "Emoji + lowercase impact level for the worst resource covered by the sentence.",
			Render: func(_ *BlockContext, n *core.Node) string {
				kc, _ := n.Payload.(*core.KeyChange)
				if kc == nil {
					return ""
				}
				emoji := core.ImpactEmoji(kc.Impact)
				if emoji == "" {
					return string(kc.Impact)
				}
				return fmt.Sprintf("%s %s", emoji, kc.Impact)
			},
		},
	}
}
