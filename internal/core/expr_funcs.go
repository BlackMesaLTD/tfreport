package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// DefaultFunctions returns the function set available to every tfreport
// HCL expression. Callers shouldn't mutate the returned map — it's
// constructed per call and discarded; if you need to register additional
// functions for a specific evaluation, merge into your own copy.
//
// Currently registered:
//
//	count(x)       — length of list/set/map/tuple/object; errors otherwise
//	contains(c, v) — true iff collection c contains value v. Mirrors the
//	                 terraform stdlib function so users can write
//	                 `contains(["critical","high"], self.impact)` in a
//	                 `where` predicate.
//	fingerprint(x) — sha256-hex of a canonical string form of x
//	is_root(n)     — true iff the node has no parent (tree root)
//	depth(n)       — shortcut for n.depth; accepts any object with `depth`
//
// The function surface is deliberately tiny but favours terraform
// idioms — the priority is that expressions feel native to a terraform
// user reading them in `.tfreport.yml`.
func DefaultFunctions() map[string]function.Function {
	return map[string]function.Function{
		"count":       countFunc,
		"contains":    containsFunc,
		"fingerprint": fingerprintFunc,
		"is_root":     isRootFunc,
		"depth":       depthFunc,
	}
}

var countFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name:         "collection",
			Type:         cty.DynamicPseudoType,
			AllowNull:    true,
			AllowUnknown: false,
		},
	},
	Type: function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		v := args[0]
		if v.IsNull() {
			return cty.NumberIntVal(0), nil
		}
		t := v.Type()
		switch {
		case t.IsListType(), t.IsSetType(), t.IsMapType(), t.IsTupleType():
			return cty.NumberIntVal(int64(v.LengthInt())), nil
		case t.IsObjectType():
			return cty.NumberIntVal(int64(len(t.AttributeTypes()))), nil
		default:
			return cty.NilVal, function.NewArgErrorf(0,
				"count: requires list, set, map, tuple, or object; got %s", t.FriendlyName())
		}
	},
})

// containsFunc mirrors terraform's stdlib contains(list, value). A
// terraform user's first reach for "is x one of [a, b, c]" is
// `contains([a, b, c], x)`, not `x == a || x == b || x == c`. Keeps
// tfreport expressions idiomatic in their daily editor.
//
// Accepts list, set, or tuple for the collection argument. Returns
// false on a null collection (defensive — null isn't the same as an
// empty list but the user intent is almost always the same).
var containsFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "collection", Type: cty.DynamicPseudoType, AllowNull: true},
		{Name: "value", Type: cty.DynamicPseudoType, AllowNull: true},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		collection := args[0]
		value := args[1]
		if collection.IsNull() {
			return cty.False, nil
		}
		t := collection.Type()
		if !(t.IsListType() || t.IsSetType() || t.IsTupleType()) {
			return cty.NilVal, function.NewArgErrorf(0,
				"contains: first argument must be a list, set, or tuple; got %s", t.FriendlyName())
		}
		for it := collection.ElementIterator(); it.Next(); {
			_, el := it.Element()
			if el.Type().Equals(value.Type()) && el.RawEquals(value) {
				return cty.True, nil
			}
		}
		return cty.False, nil
	},
})

var fingerprintFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name:         "value",
			Type:         cty.DynamicPseudoType,
			AllowNull:    true,
			AllowUnknown: false,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		// GoString on cty.Value produces a stable, type-qualified string
		// representation — the same value always produces the same hash.
		s := args[0].GoString()
		sum := sha256.Sum256([]byte(s))
		return cty.StringVal(hex.EncodeToString(sum[:])), nil
	},
})

// isRootFunc inspects a node-like object for parentage. The HCL user
// writes `is_root(self)` or `is_root(some_other_node)`. We can't reach
// the Go *Node from cty, so we rely on the node's `depth` attribute —
// depth 0 means no ancestors.
var isRootFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "node", Type: cty.DynamicPseudoType},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		v := args[0]
		if !v.Type().IsObjectType() {
			return cty.NilVal, function.NewArgErrorf(0, "is_root: expected node-like object, got %s", v.Type().FriendlyName())
		}
		if !v.Type().HasAttribute("depth") {
			return cty.NilVal, function.NewArgErrorf(0, "is_root: object has no `depth` attribute")
		}
		d := v.GetAttr("depth")
		bf := d.AsBigFloat()
		depth, _ := bf.Int64()
		return cty.BoolVal(depth == 0), nil
	},
})

// depthFunc is the function form of `node.depth`. Handy inside function
// pipelines where method access isn't available.
var depthFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "node", Type: cty.DynamicPseudoType},
	},
	Type: function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		v := args[0]
		if !v.Type().IsObjectType() || !v.Type().HasAttribute("depth") {
			return cty.NilVal, function.NewArgErrorf(0, "depth: expected node-like object with `depth` attribute, got %s", v.Type().FriendlyName())
		}
		return v.GetAttr("depth"), nil
	},
})

// unused helper kept for future panic-recovery tests; exported-ish so
// tests can trigger a panicking function without adding a test-only
// registration API.
func panickingFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "x", Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			panic(fmt.Errorf("deliberate panic"))
		},
	})
}
