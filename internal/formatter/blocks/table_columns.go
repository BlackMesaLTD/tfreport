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
			Heading:     "Address",
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
			Description: "Full module address, backticked; `(root)` for root-module resources. Matches legacy imports_list / changed_resources_table output.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil || rc.ModulePath == "" {
					return "`(root)`"
				}
				return "`" + rc.ModulePath + "`"
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
			Description: "`♻️ yes` when the resource is being imported, `—` otherwise. Matches legacy changed_resources_table output.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil || !rc.IsImport {
					return "—"
				}
				return "♻️ yes"
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
		"module": {
			Heading:     "Module",
			Description: "Enclosing ModuleCall's name (backticked); backticked `(root)` for root-module resources. Matches legacy imports_list / changed_resources_table `module` column.",
			Render: func(_ *BlockContext, n *core.Node) string {
				for p := n.Parent; p != nil; p = p.Parent {
					if p.Kind == core.KindModuleCall {
						return "`" + p.Name + "`"
					}
				}
				return "`(root)`"
			},
		},
		"module_type": {
			Heading:     "Module Type",
			Description: "Resolved module type via the outermost ModuleCall's source URL. Matches legacy changed_resources_table `module_type` column.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				r := enclosingReport(n)
				// Walk up to find the outermost ModuleCall for type resolution.
				var topCall, leafCall string
				for p := n.Parent; p != nil && p.Kind != core.KindReport; p = p.Parent {
					if p.Kind == core.KindModuleCall {
						topCall = p.Name // overwritten; final value is outermost
						if leafCall == "" {
							leafCall = p.Name
						}
					}
				}
				if topCall == "" {
					return ""
				}
				var sources map[string]string
				if r != nil {
					sources = r.ModuleSources
				}
				return core.ResolveModuleType(topCall, sources, leafCall)
			},
		},
		"notes": {
			Heading:     "Notes",
			Description: "Config-provided attribute notes joined with `; `; `—` when no note matches. Uses ctx.NoteResolver.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc, _ := n.Payload.(*core.ResourceChange)
				if rc == nil || ctx.NoteResolver == nil {
					return "—"
				}
				var notes []string
				for _, a := range rc.ChangedAttributes {
					if note := ctx.NoteResolver(rc.ResourceType, a.Key); note != "" {
						notes = append(notes, note)
					}
				}
				if len(notes) == 0 {
					return "—"
				}
				return strings.Join(notes, "; ")
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
		"old": {
			Heading:     "Before",
			Description: "Before value, JSON-encoded and truncated to ctx.Output.AttributeValueTruncate chars. `—` for nil. Matches legacy attribute_diff `old` column.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				a, _ := n.Payload.(*core.ChangedAttribute)
				if a == nil {
					return ""
				}
				return renderAttrValue(a.OldValue, ctx.Output.AttributeValueTruncate, false)
			},
		},
		"new": {
			Heading:     "After",
			Description: "After value, JSON-encoded and truncated. `(known after apply)` for computed; `—` for nil. Matches legacy attribute_diff `new` column.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				a, _ := n.Payload.(*core.ChangedAttribute)
				if a == nil {
					return ""
				}
				return renderAttrValue(a.NewValue, ctx.Output.AttributeValueTruncate, a.Computed)
			},
		},
		"impact": {
			Heading:     "Impact",
			Description: "Impact of the enclosing resource, with emoji + optional note (walks up to the Resource parent).",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc := enclosingResource(n)
				if rc == nil {
					return ""
				}
				return formatImpactWithNote(ctx, *rc)
			},
		},
		"address": {
			Heading:     "Address",
			Description: "Full terraform address of the enclosing resource, backticked.",
			Render: func(_ *BlockContext, n *core.Node) string {
				rc := enclosingResource(n)
				if rc == nil {
					return ""
				}
				return "`" + rc.Address + "`"
			},
		},
		"resource_type": {
			Heading:     "Resource Type",
			Description: "Display name for the enclosing resource's type.",
			Render: func(ctx *BlockContext, n *core.Node) string {
				rc := enclosingResource(n)
				if rc == nil {
					return ""
				}
				return displayName(ctx, rc.ResourceType)
			},
		},
	}
}

// enclosingResource walks up from an Attribute node to find its parent
// Resource and returns that Resource's ChangedAttribute payload's
// enclosing ResourceChange. Returns nil when no Resource ancestor or
// when the node isn't attached to one.
func enclosingResource(n *core.Node) *core.ResourceChange {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Kind == core.KindResource {
			rc, _ := p.Payload.(*core.ResourceChange)
			return rc
		}
	}
	return nil
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
	for _, rc := range resourcesUnder(n) {
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
		return groupedAttrKeys(n, core.ActionUpdate, core.ActionReplace)
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
		for _, rc := range resourcesUnder(n) {
			total += len(rc.ChangedAttributes)
		}
		return fmt.Sprintf("%d attrs", total)
	default:
		return "—"
	}
}

// unionAttrKeysFromNode collects + sorts the backticked union of
// attribute keys across every Resource under n (direct children and
// nested sub-module descendants). Empty result returns "—" as a
// cell-safe placeholder.
func unionAttrKeysFromNode(n *core.Node, _ bool) string {
	return groupedAttrKeys(n)
}

// groupedAttrKeys renders the changed-attribute cell for a module
// instance without losing which resource an expanded key belongs to.
// Plain keys (tags, location, …) are unioned across every resource as
// before. Expanded block-set keys (security_rule[<name>].…) are grouped
// under the sub-module (or resource) that owns them, and cosmetic
// verdicts are counted rather than listed, so 160 rules rewritten as lists
// read as one entry:
//
//	`tags`; `nsg["evwprod-mgmt"]`: `security_rule[coreplf-mailbox-out].removed`, `security_rule[…×160].string_to_list`
//
// actions, when given, restrict which resources contribute (the dash /
// wordy modes pass update+replace; list mode passes nothing = all).
func groupedAttrKeys(n *core.Node, actions ...core.Action) string {
	want := map[core.Action]bool{}
	for _, a := range actions {
		want[a] = true
	}
	plain := map[string]struct{}{}
	type ownerEntry struct {
		real   map[string]struct{}
		counts map[string]int    // verdict → count
		single map[string]string // verdict → the one key, when count == 1
		base   string
	}
	owners := map[string]*ownerEntry{}
	var ownerOrder []string
	for _, rc := range resourcesUnder(n) {
		if len(want) > 0 && !want[rc.Action] {
			continue
		}
		for _, a := range rc.ChangedAttributes {
			if !strings.Contains(a.Key, "[") && core.CosmeticVerdict(a.Key) == "" {
				plain[a.Key] = struct{}{}
				continue
			}
			label := ownerLabel(rc)
			o, ok := owners[label]
			if !ok {
				o = &ownerEntry{real: map[string]struct{}{}, counts: map[string]int{}, single: map[string]string{}}
				owners[label] = o
				ownerOrder = append(ownerOrder, label)
			}
			if v := core.CosmeticVerdict(a.Key); v != "" {
				o.counts[v]++
				o.single[v] = a.Key
				o.base = core.BaseAttributeKey(a.Key)
				continue
			}
			o.real[a.Key] = struct{}{}
		}
	}
	if len(plain) == 0 && len(owners) == 0 {
		return "—"
	}
	var segments []string
	if len(plain) > 0 {
		segments = append(segments, backtickedSorted(plain))
	}
	sortStrings(ownerOrder)
	for _, label := range ownerOrder {
		o := owners[label]
		var parts []string
		if len(o.real) > 0 {
			parts = append(parts, backtickedSorted(o.real))
		}
		verdicts := make([]string, 0, len(o.counts))
		for v := range o.counts {
			verdicts = append(verdicts, v)
		}
		sortStrings(verdicts)
		for _, v := range verdicts {
			if o.counts[v] == 1 {
				parts = append(parts, "`"+o.single[v]+"`")
			} else {
				parts = append(parts, fmt.Sprintf("`%s[…×%d].%s`", o.base, o.counts[v], v))
			}
		}
		seg := strings.Join(parts, ", ")
		if label != "" {
			seg = "`" + label + "`: " + seg
		}
		segments = append(segments, seg)
	}
	return strings.Join(segments, "; ")
}

// ownerLabel names the sub-module (relative to the top-level instance) or,
// for a resource sitting directly in the instance, the resource itself.
// module.vnet.module.nsg["app"].azurerm_network_security_group.main → nsg["app"]
// module.vnet.azurerm_network_security_group.main                   → azurerm_network_security_group.main
func ownerLabel(rc *core.ResourceChange) string {
	m := core.ParseModuleAddress(rc.ModulePath)
	if len(m.Segments) <= 1 {
		return rc.ResourceType + "." + rc.ResourceName
	}
	parts := make([]string, 0, len(m.Segments)-1)
	for _, seg := range m.Segments[1:] {
		if seg.Instance != "" {
			parts = append(parts, seg.Name+"["+seg.Instance+"]")
		} else {
			parts = append(parts, seg.Name)
		}
	}
	return strings.Join(parts, ".")
}

func backtickedSorted(set map[string]struct{}) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = "`" + k + "`"
	}
	return strings.Join(parts, ", ")
}

// resourcesUnder returns every ResourceChange in n's subtree in tree
// order. A module instance's resources routinely live one or more
// sub-module calls down (module.vnet.module.nsg["app"].azurerm_…), and
// a column that only looked at direct children rendered "—" for exactly
// those instances.
func resourcesUnder(n *core.Node) []*core.ResourceChange {
	var out []*core.ResourceChange
	var walk func(*core.Node)
	walk = func(x *core.Node) {
		for _, c := range x.Children {
			if c.Kind == core.KindResource {
				if rc, ok := c.Payload.(*core.ResourceChange); ok && rc != nil {
					out = append(out, rc)
				}
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return out
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
