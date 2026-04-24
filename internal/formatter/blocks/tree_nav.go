package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// tree_nav.go carries helpers for walking the PlanTree from a node back
// up to its enclosing module or report. Used by table columns that need
// the legacy ModuleGroup equivalents (mg.Name / mg.Path / module type
// resolution) when the row unit is a tree Node rather than a raw
// *core.ModuleGroup.

// enclosingReport returns the *core.Report payload attached to the
// nearest KindReport ancestor of n (inclusive of n itself). Returns nil
// when no Report is in the chain — happens for synthetic nodes in
// tests that aren't hung off a real tree.
func enclosingReport(n *core.Node) *core.Report {
	for p := n; p != nil; p = p.Parent {
		if p.Kind == core.KindReport {
			if r, ok := p.Payload.(*core.Report); ok {
				return r
			}
		}
	}
	return nil
}

// moduleInstancePath reconstructs the full terraform module address
// for a KindModuleInstance node by walking up the ModuleCall /
// ModuleInstance chain to the enclosing Report. Returns an empty
// string for the root module or for non-ModuleInstance inputs.
//
// Example: the `vnet` instance under `module.platform.module.vnet`
// reconstructs to "module.platform.module.vnet". A for-each child
// like `module.zone["prod"]` reconstructs verbatim with brackets.
func moduleInstancePath(n *core.Node) string {
	if n == nil || n.Kind != core.KindModuleInstance {
		return ""
	}
	// Collect (call-name, instance-key) pairs from innermost to
	// outermost by hopping Parent: ModuleInstance -> ModuleCall -> ...
	type pair struct{ call, instance string }
	var pairs []pair
	cur := n
	for cur != nil && cur.Kind != core.KindReport {
		if cur.Kind == core.KindModuleInstance && cur.Parent != nil && cur.Parent.Kind == core.KindModuleCall {
			pairs = append(pairs, pair{call: cur.Parent.Name, instance: cur.Name})
			cur = cur.Parent.Parent
			continue
		}
		cur = cur.Parent
	}
	// Render outermost-first.
	var b strings.Builder
	for i := len(pairs) - 1; i >= 0; i-- {
		if b.Len() > 0 {
			b.WriteString(".")
		}
		b.WriteString("module.")
		b.WriteString(pairs[i].call)
		if pairs[i].instance != "" {
			b.WriteString("[")
			b.WriteString(pairs[i].instance)
			b.WriteString("]")
		}
	}
	return b.String()
}

// moduleInstanceTopLevel returns the outermost ModuleCall name in the
// chain above n. For a deeply-nested ModuleInstance it yields the
// first-declared module call (the one whose source URL lives in
// Report.ModuleSources). Returns "" for the root module.
func moduleInstanceTopLevel(n *core.Node) string {
	if n == nil {
		return ""
	}
	var last string
	for p := n; p != nil && p.Kind != core.KindReport; p = p.Parent {
		if p.Kind == core.KindModuleCall {
			last = p.Name // walking up; last assignment is the outermost call
		}
	}
	return last
}

// moduleInstanceLeafName produces the short label that modules_table's
// `module` column historically rendered — the ModuleCall's Name, with
// the instance bracket appended if this is a for_each/count child.
// Matches grouper.moduleName's output shape exactly.
func moduleInstanceLeafName(n *core.Node) string {
	if n == nil || n.Kind != core.KindModuleInstance {
		return ""
	}
	if n.Parent == nil || n.Parent.Kind != core.KindModuleCall {
		return ""
	}
	call := n.Parent.Name
	if n.Name == "" {
		return call
	}
	return fmt.Sprintf("%s[%s]", call, n.Name)
}

// moduleInstanceActionSummary renders the ModuleInstance's action
// counts as "2 create, 1 update" — matches modules_table's `actions`
// column grammar so migration doesn't shift bytes.
func moduleInstanceActionSummary(n *core.Node) string {
	if n == nil {
		return ""
	}
	return actionSummaryLine(n.Agg.ActionCounts)
}
