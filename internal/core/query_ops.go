package core

import (
	"fmt"
	"slices"
	"sort"

	"github.com/zclconf/go-cty/cty"
)

// Group is one bucket produced by GroupBy. Key is the stringified value
// of the grouping expression; Nodes are the members in original order.
type Group struct {
	Key   string
	Nodes []*Node
}

// Filter returns the subset of nodes for which expr evaluates true.
// expr is evaluated once per node with `self` bound to the node; extras
// merge in. A nil expr returns the input unchanged.
//
// A non-bool result errors for that node — callers should gate `where`
// expressions through EvalBool's same rules.
func Filter(nodes []*Node, expr *Expr, extras map[string]cty.Value) ([]*Node, error) {
	if expr == nil {
		return nodes, nil
	}
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		ok, err := EvalBool(expr, n, extras)
		if err != nil {
			return nil, fmt.Errorf("filter: node %q: %w", n.Name, err)
		}
		if ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// GroupBy partitions nodes into Groups keyed on the stringified result of
// expr. Insertion order of groups is preserved (first seen, first
// returned). A nil expr returns one Group with key "" containing every
// node.
//
// Keys derive from cty.Value.GoString so type and shape survive —
// cty.StringVal("x") and cty.NumberIntVal(0) produce distinct keys even
// if both display as "x" and "0" in prose.
func GroupBy(nodes []*Node, expr *Expr, extras map[string]cty.Value) ([]Group, error) {
	if expr == nil {
		if len(nodes) == 0 {
			return nil, nil
		}
		return []Group{{Key: "", Nodes: slices.Clone(nodes)}}, nil
	}

	byKey := make(map[string]int) // key -> group index
	var groups []Group

	for _, n := range nodes {
		v, err := Eval(expr, n, extras)
		if err != nil {
			return nil, fmt.Errorf("group: node %q: %w", n.Name, err)
		}
		key := groupKey(v)
		idx, seen := byKey[key]
		if !seen {
			idx = len(groups)
			byKey[key] = idx
			groups = append(groups, Group{Key: key})
		}
		groups[idx].Nodes = append(groups[idx].Nodes, n)
	}
	return groups, nil
}

// groupKey produces a deterministic string key from a cty.Value. Strings
// use their raw bytes; numbers use their canonical decimal form; other
// types fall back to GoString so structurally-different values don't
// collide.
func groupKey(v cty.Value) string {
	if v.IsNull() {
		return "(null)"
	}
	switch v.Type() {
	case cty.String:
		return v.AsString()
	case cty.Number:
		return v.AsBigFloat().Text('f', -1)
	case cty.Bool:
		if v.True() {
			return "true"
		}
		return "false"
	default:
		return v.GoString()
	}
}

// SortBy stably sorts nodes by the value of expr. desc flips the
// direction. A nil expr returns the input unchanged.
//
// Supported sort types: string and number. Booleans, lists, maps, and
// objects error — callers should project to a comparable value in expr
// (e.g. `length(self.children)` rather than `self.children`).
func SortBy(nodes []*Node, expr *Expr, desc bool) ([]*Node, error) {
	if expr == nil {
		return nodes, nil
	}
	// Pre-evaluate so we sort without repeat calls.
	type kv struct {
		node *Node
		val  cty.Value
	}
	pairs := make([]kv, len(nodes))
	for i, n := range nodes {
		v, err := Eval(expr, n, nil)
		if err != nil {
			return nil, fmt.Errorf("sort: node %q: %w", n.Name, err)
		}
		pairs[i] = kv{node: n, val: v}
	}

	// Type-check: every value must be the same type (string or number).
	// We don't auto-coerce — that hides bugs.
	if len(pairs) > 0 {
		t := pairs[0].val.Type()
		if t != cty.String && t != cty.Number {
			return nil, fmt.Errorf("sort: expression must yield string or number, got %s", t.FriendlyName())
		}
		for _, p := range pairs[1:] {
			if p.val.Type() != t {
				return nil, fmt.Errorf("sort: mixed types %s and %s — expression must be consistent per node", t.FriendlyName(), p.val.Type().FriendlyName())
			}
		}
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		less := ctyLess(pairs[i].val, pairs[j].val)
		if desc {
			return !less
		}
		return less
	})

	out := make([]*Node, len(pairs))
	for i, p := range pairs {
		out[i] = p.node
	}
	return out, nil
}

// ctyLess compares two values of the same scalar type. Strings compare
// lexicographically; numbers compare as arbitrary-precision floats.
// Assumes caller has already type-checked both values are the same
// supported scalar type.
func ctyLess(a, b cty.Value) bool {
	switch a.Type() {
	case cty.String:
		return a.AsString() < b.AsString()
	case cty.Number:
		af := a.AsBigFloat()
		bf := b.AsBigFloat()
		return af.Cmp(bf) < 0
	default:
		return false
	}
}

// Limit returns at most n nodes from the head of the slice. n <= 0 or
// n >= len(nodes) returns the input unchanged. Never allocates when
// no truncation is needed.
func Limit(nodes []*Node, n int) []*Node {
	if n <= 0 || n >= len(nodes) {
		return nodes
	}
	return nodes[:n]
}
