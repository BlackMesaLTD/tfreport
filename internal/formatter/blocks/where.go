package blocks

import (
	"fmt"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// where.go is the shared plumbing for the HCL `where=` predicate arg
// every filter-heavy block exposes. The predicate composes AND with
// each block's CSV filters — a row survives only when BOTH the CSV
// filters and the HCL predicate accept it.
//
// Usage inside a block's Render:
//
//	whereExpr, err := parseWhereArg(args, "module_details")
//	if err != nil { return "", err }
//	if whereExpr != nil {
//	    nodeIdx := resourceNodeIndex(ctx, r)
//	    keep, err := evalResourceWhere(whereExpr, nodeIdx, rc, "module_details")
//	    ...
//	}

// parseWhereArg pulls the `where` arg off an args map and compiles it
// into a reusable *core.Expr. Returns nil, nil when the arg is absent.
// blockName appears in the error so users see the context when their
// predicate has a syntax error.
func parseWhereArg(args map[string]any, blockName string) (*core.Expr, error) {
	s := ArgString(args, "where", "")
	if s == "" {
		return nil, nil
	}
	expr, err := core.ParseExpr(s, blockName+".where")
	if err != nil {
		return nil, fmt.Errorf("%s: where: %w", blockName, err)
	}
	return expr, nil
}

// resourceNodeIndex returns a map keyed on resource address → tree
// Node, drawing from the ctx's PlanTree subtree when one is bound, or
// building one on-demand from r otherwise. Callers in single-report
// blocks should pass currentReport(ctx) as r.
//
// When no tree can be built (nil report) the returned map is empty.
// Keys match core.ResourceChange.Address exactly, which is the .Name
// field of KindResource nodes (see core.buildResourceNode).
func resourceNodeIndex(ctx *BlockContext, r *core.Report) map[string]*core.Node {
	var root *core.Node
	if sub := reportSubtree(ctx); sub != nil {
		root = sub
	} else if r != nil {
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

// attributeNodeIndex returns a map keyed on "<resource-address>|<attr-key>"
// → Attribute Node. Used by attribute_diff so its `where=` predicate can
// bind `self` to an Attribute (self.sensitive, self.computed, self.key).
// Empty map when no tree can be built.
func attributeNodeIndex(ctx *BlockContext, r *core.Report) map[string]*core.Node {
	var root *core.Node
	if sub := reportSubtree(ctx); sub != nil {
		root = sub
	} else if r != nil {
		root = core.BuildTree(r).Root
	}
	if root == nil {
		return nil
	}
	idx := make(map[string]*core.Node)
	for _, n := range core.Query(root, core.Path{core.KindResource, core.KindAttribute}) {
		if n.Parent == nil {
			continue
		}
		idx[n.Parent.Name+"|"+n.Name] = n
	}
	return idx
}

// evalResourceWhere evaluates an HCL `where` predicate for a resource
// change. Looks up the resource's tree Node by address so `self` binds
// to a real node with aggregates and payload fields. If the resource
// is missing from the index (can happen with hand-crafted fixtures
// that bypass the grouper) the predicate defaults to keep=true — the
// row survives just as if no predicate were set. blockName prefixes
// the error for user-visible context.
func evalResourceWhere(expr *core.Expr, idx map[string]*core.Node, rc core.ResourceChange, blockName string) (bool, error) {
	if expr == nil {
		return true, nil
	}
	n := idx[rc.Address]
	if n == nil {
		return true, nil
	}
	keep, err := core.EvalBool(expr, n, nil)
	if err != nil {
		return false, fmt.Errorf("%s: where: %w", blockName, err)
	}
	return keep, nil
}

// evalAttributeWhere is the attribute-scoped twin of evalResourceWhere.
// The index key is "<resource-address>|<attr-key>" — see
// attributeNodeIndex.
func evalAttributeWhere(expr *core.Expr, idx map[string]*core.Node, rc core.ResourceChange, attr core.ChangedAttribute, blockName string) (bool, error) {
	if expr == nil {
		return true, nil
	}
	n := idx[rc.Address+"|"+attr.Key]
	if n == nil {
		return true, nil
	}
	keep, err := core.EvalBool(expr, n, nil)
	if err != nil {
		return false, fmt.Errorf("%s: where: %w", blockName, err)
	}
	return keep, nil
}
