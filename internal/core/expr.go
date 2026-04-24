package core

import (
	"fmt"
	"maps"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Expr is a parsed HCL expression ready for repeated evaluation. A single
// Expr is immutable once parsed; concurrent Eval calls are safe.
//
// Expressions are the runtime surface for block arguments like
// `where="count(self.children) > 5"` or `group="fingerprint(self)"`.
// They are authored in YAML strings but evaluated as HCL — the same
// dialect terraform users already know.
type Expr struct {
	expr     hcl.Expression
	src      string
	filename string
}

// Source returns the original expression text for error messages and
// debugging. Does not reflect any runtime substitutions.
func (e *Expr) Source() string { return e.src }

// ParseExpr compiles an HCL expression from a string. filename is attached
// to any diagnostic so that errors anchor to the user's YAML source
// (e.g. `.tfreport.yml#blocks[2].where`). An empty filename is acceptable
// but produces less helpful errors.
//
// Returns a non-nil *Expr on success. On parse failure the returned error
// is a hcl.Diagnostics with position information — callers can type-assert
// if they want structured diagnostics.
func ParseExpr(text, filename string) (*Expr, error) {
	expr, diags := hclsyntax.ParseExpression([]byte(text), filename, hcl.Pos{Line: 1, Column: 1, Byte: 0})
	if diags.HasErrors() {
		return nil, diags
	}
	return &Expr{expr: expr, src: text, filename: filename}, nil
}

// MustParseExpr is ParseExpr that panics on failure. Tests only.
func MustParseExpr(text string) *Expr {
	e, err := ParseExpr(text, "")
	if err != nil {
		panic(err)
	}
	return e
}

// Eval evaluates the expression with `self` bound to the supplied tree
// node and any additional variables merged in. Custom functions are
// the set returned by DefaultFunctions().
//
// Panics in user-registered functions or inside cty are recovered and
// surfaced as errors — a bad expression never aborts the host process.
func Eval(expr *Expr, self *Node, extras map[string]cty.Value) (cty.Value, error) {
	if expr == nil {
		return cty.NilVal, fmt.Errorf("nil expression")
	}

	vars := map[string]cty.Value{}
	if self != nil {
		vars["self"] = NodeValue(self)
	}
	maps.Copy(vars, extras)

	evalCtx := &hcl.EvalContext{
		Variables: vars,
		Functions: DefaultFunctions(),
	}

	return safeEval(expr, evalCtx)
}

// EvalBool is a convenience for predicate expressions (e.g. `where=...`).
// Returns an error if the expression does not evaluate to a bool value.
func EvalBool(expr *Expr, self *Node, extras map[string]cty.Value) (bool, error) {
	val, err := Eval(expr, self, extras)
	if err != nil {
		return false, err
	}
	if val.Type() != cty.Bool {
		return false, fmt.Errorf("expression %q: expected bool, got %s", expr.src, val.Type().FriendlyName())
	}
	if val.IsNull() {
		return false, nil
	}
	return val.True(), nil
}

// safeEval wraps expr.Value with panic recovery. cty and user functions
// can panic on type mismatches or nil dereferences inside Impl; we never
// want that to take down the host.
func safeEval(expr *Expr, ctx *hcl.EvalContext) (val cty.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			val = cty.NilVal
			err = fmt.Errorf("expression %q: evaluation panicked: %v", expr.src, r)
		}
	}()

	v, diags := expr.expr.Value(ctx)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	return v, nil
}

// NodeValue builds a cty.Value view of a tree node. The value exposes the
// common fields every kind shares (kind, name, depth, resource_count,
// import_count, max_impact, action_counts, changed_attrs, is_leaf) plus
// kind-specific fields for resource and attribute nodes.
//
// Depth is measured from the PlanTree root: the root node has depth 0,
// its direct children have depth 1, and so on. Consumers don't need to
// pre-compute it — this function walks Parent pointers.
//
// A nil Node returns cty.NilVal so HCL expressions evaluated against a
// missing context fail cleanly rather than panicking.
func NodeValue(n *Node) cty.Value {
	if n == nil {
		return cty.NilVal
	}
	depth := nodeDepth(n)
	attrs := map[string]cty.Value{
		"kind":           cty.StringVal(string(n.Kind)),
		"name":           cty.StringVal(n.Name),
		"depth":          cty.NumberIntVal(int64(depth)),
		"resource_count": cty.NumberIntVal(int64(n.Agg.ResourceCount)),
		"import_count":   cty.NumberIntVal(int64(n.Agg.ImportCount)),
		"max_impact":     cty.StringVal(string(n.Agg.MaxImpact)),
		"action_counts":  actionCountsValue(n.Agg.ActionCounts),
		"changed_attrs":  stringListValue(n.Agg.ChangedAttrs),
		"is_leaf":        cty.BoolVal(len(n.Children) == 0),
		"child_count":    cty.NumberIntVal(int64(len(n.Children))),
	}
	// Kind-specific payload fields.
	switch n.Kind {
	case KindResource:
		if rc, ok := n.Payload.(*ResourceChange); ok && rc != nil {
			attrs["address"] = cty.StringVal(rc.Address)
			attrs["module_path"] = cty.StringVal(rc.ModulePath)
			attrs["resource_type"] = cty.StringVal(rc.ResourceType)
			attrs["resource_name"] = cty.StringVal(rc.ResourceName)
			attrs["action"] = cty.StringVal(string(rc.Action))
			attrs["impact"] = cty.StringVal(string(rc.Impact))
			attrs["is_import"] = cty.BoolVal(rc.IsImport)
			attrs["display_label"] = cty.StringVal(rc.DisplayLabel)
		}
	case KindAttribute:
		if a, ok := n.Payload.(*ChangedAttribute); ok && a != nil {
			attrs["key"] = cty.StringVal(a.Key)
			attrs["sensitive"] = cty.BoolVal(a.Sensitive)
			attrs["computed"] = cty.BoolVal(a.Computed)
			attrs["description"] = cty.StringVal(a.Description)
		}
	case KindKeyChange:
		if kc, ok := n.Payload.(*KeyChange); ok && kc != nil {
			attrs["text"] = cty.StringVal(kc.Text)
			attrs["impact"] = cty.StringVal(string(kc.Impact))
		}
	case KindTextPlan:
		if tp, ok := n.Payload.(TextPlanData); ok {
			attrs["address"] = cty.StringVal(tp.Address)
			attrs["body"] = cty.StringVal(tp.Body)
		}
	}
	return cty.ObjectVal(attrs)
}

// nodeDepth counts Parent hops from n back to the tree root.
func nodeDepth(n *Node) int {
	depth := 0
	for p := n.Parent; p != nil; p = p.Parent {
		depth++
	}
	return depth
}

// actionCountsValue converts map[Action]int into a cty map of
// string -> number so HCL users can read `self.action_counts.create`.
// nil input produces an empty map (never cty.NilVal) so expressions
// accessing a missing action return 0 instead of erroring.
func actionCountsValue(counts map[Action]int) cty.Value {
	if len(counts) == 0 {
		return cty.MapValEmpty(cty.Number)
	}
	m := make(map[string]cty.Value, len(counts))
	for k, v := range counts {
		m[string(k)] = cty.NumberIntVal(int64(v))
	}
	return cty.MapVal(m)
}

// stringListValue converts []string into a cty list of strings. Empty
// input returns an empty list (never cty.NilVal).
func stringListValue(items []string) cty.Value {
	if len(items) == 0 {
		return cty.ListValEmpty(cty.String)
	}
	vals := make([]cty.Value, len(items))
	for i, s := range items {
		vals[i] = cty.StringVal(s)
	}
	return cty.ListVal(vals)
}

