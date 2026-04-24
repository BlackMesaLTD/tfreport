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
			Heading:     "Resource",
			Description: "Display name for the resource type (e.g. `subnet` for `azurerm_subnet`). Uses ctx.DisplayNames with a provider-stripped fallback. Matches the legacy changed_resources_table `resource_type` column.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				return displayName(ctx, rc.ResourceType)
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
		"name": {
			Heading:     "Name",
			Description: "Resource display label via core.ResourceDisplayLabel (pre-computed from Before/After `name` attr). Matches the legacy changed_resources_table `name` column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				return core.ResourceDisplayLabel(*rc)
			},
		},
		"changed": {
			Heading:     "Changed",
			Description: "Changed attribute keys for update/replace; placeholder per `changed_attrs_display` mode for create/delete. Matches the legacy changed_resources_table `changed` column.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				mode := resolveChangedAttrsMode(ctx, "")
				return renderChangedCell(rc.Action, rc.ChangedAttributes, mode, formatAttrsKeysOnly)
			},
		},
		"impact_with_note": {
			Heading:     "Impact",
			Description: "Impact emoji + level with optional ` — _note_` suffix pulled from ctx.NoteResolver. Matches the legacy changed_resources_table `impact` column grammar.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil {
					return ""
				}
				return formatImpactWithNote(ctx, *rc)
			},
		},
		"force_new": {
			Heading:     "Force-new",
			Description: "`✓` when any changed attribute is preset-marked force_new; `—` otherwise. Requires ctx.ForceNewResolver.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil || ctx.ForceNewResolver == nil {
					return "—"
				}
				for _, a := range rc.ChangedAttributes {
					if fn, ok := ctx.ForceNewResolver(rc.ResourceType, a.Key); ok && fn {
						return "✓"
					}
				}
				return "—"
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
			Description: "Union of changed attribute keys across the instance's resources — honours ctx.Output.ChangedAttrsDisplay (dash / wordy / count / list). Matches legacy modules_table grammar.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				mode := resolveChangedAttrsMode(ctx, "")
				return renderModuleNodeChangedAttrs(n, mode)
			},
		},
	}
}

// renderModuleNodeChangedAttrs implements modules_table's target-aware
// changed_attrs grammar over a ModuleInstance tree node. Ported from
// renderModulesTableChangedAttrs so modules_table can delegate to the
// `table` block without losing its empty-group placeholder semantics.
//
// Behaviour:
//   - list mode → union of ALL child resources' attribute keys
//   - other modes → union of UPDATE/REPLACE resources' keys; when the
//     instance has no update/replace resources, render a placeholder
//     per mode (wordy: new/removed/new+removed; count: N attrs; dash: —)
func renderModuleNodeChangedAttrs(n *core.Node, mode string) string {
	if mode == "" {
		mode = ChangedAttrsDash
	}

	if mode == ChangedAttrsList {
		return unionAttrKeysFromNode(n, false)
	}

	// Partition resources into meaningful (update/replace) vs compact
	// (create/delete/read/no-op). If any meaningful exist, render their
	// union only — matches modules_table's partitioning.
	var meaningfulAttrs []core.ChangedAttribute
	var creates, deletes int
	for _, c := range n.Children {
		if c.Kind != core.KindResource {
			continue
		}
		rc, ok := c.Payload.(*core.ResourceChange)
		if !ok || rc == nil {
			continue
		}
		switch rc.Action {
		case core.ActionUpdate, core.ActionReplace:
			meaningfulAttrs = append(meaningfulAttrs, rc.ChangedAttributes...)
		case core.ActionCreate:
			creates++
		case core.ActionDelete:
			deletes++
		}
	}
	if len(meaningfulAttrs) > 0 {
		return unionAttrKeysFromSlice(meaningfulAttrs)
	}

	// Whole instance is create/delete/read/no-op. Mode picks the placeholder.
	switch mode {
	case ChangedAttrsWordy:
		switch {
		case creates > 0 && deletes > 0:
			return "new+removed"
		case creates > 0:
			return "new"
		case deletes > 0:
			return "removed"
		default:
			return "—"
		}
	case ChangedAttrsCount:
		total := 0
		for _, c := range n.Children {
			if c.Kind != core.KindResource {
				continue
			}
			rc, _ := c.Payload.(*core.ResourceChange)
			if rc != nil {
				total += len(rc.ChangedAttributes)
			}
		}
		return fmt.Sprintf("%d attrs", total)
	default:
		return "—"
	}
}

// unionAttrKeysFromNode collects + sorts the backticked union of
// attribute keys across all Resource children of n. empty result
// returns "—" as a cell-safe placeholder.
func unionAttrKeysFromNode(n *core.Node, _ bool) string {
	var all []core.ChangedAttribute
	for _, c := range n.Children {
		if c.Kind != core.KindResource {
			continue
		}
		rc, _ := c.Payload.(*core.ResourceChange)
		if rc != nil {
			all = append(all, rc.ChangedAttributes...)
		}
	}
	return unionAttrKeysFromSlice(all)
}

// unionAttrKeysFromSlice dedups+sorts+backticks attribute keys.
// Empty input returns "—" (cell-safe fallback).
func unionAttrKeysFromSlice(attrs []core.ChangedAttribute) string {
	if len(attrs) == 0 {
		return "—"
	}
	seen := map[string]struct{}{}
	for _, a := range attrs {
		seen[a.Key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = "`" + k + "`"
	}
	return strings.Join(parts, ", ")
}

// sortStrings is a tiny wrapper around sort.Strings so we can keep
// the import list in this file minimal. Using sort directly would
// force an import rearrangement in every render function.
func sortStrings(s []string) {
	// insertion sort — inputs are small attribute-key lists
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
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
