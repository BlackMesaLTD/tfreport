package core

import (
	"fmt"
	"strings"
)

// Path is a parsed node-selector: a sequence of NodeKinds joined by the
// `>` operator. Matching semantics are descendant-based — a path step
// matches any descendant of the prior step's matched node, not strictly a
// direct child.
//
// Grammar (deliberately tiny):
//
//	path := kind (">" kind)*
//	kind := "reports" | "report" | "module_call" | "module_instance" |
//	        "resource" | "attribute" | "key_change" | "text_plan"
//
// No brackets, no wildcards, no attribute filters. Filtering is HCL's
// job; every selector string describes only tree-shape traversal.
type Path []NodeKind

// String renders the path in canonical form with single spaces around
// each `>`. Stable for tests and error messages.
func (p Path) String() string {
	parts := make([]string, len(p))
	for i, k := range p {
		parts[i] = string(k)
	}
	return strings.Join(parts, " > ")
}

// ParsePath compiles a path selector string like `module_instance > resource`.
// Whitespace around `>` is optional. An empty string returns an empty Path
// and no error; callers treat that as "no selector".
func ParsePath(s string) (Path, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	raw := strings.Split(s, ">")
	path := make(Path, 0, len(raw))
	for i, r := range raw {
		step := strings.TrimSpace(r)
		if step == "" {
			return nil, fmt.Errorf("path %q: empty step at position %d", s, i)
		}
		if !isKnownKind(step) {
			return nil, fmt.Errorf("path %q: unknown kind %q at position %d (valid: %s)",
				s, step, i, strings.Join(knownKindList(), ", "))
		}
		path = append(path, NodeKind(step))
	}
	return path, nil
}

// MustParsePath is ParsePath that panics on failure. Tests only.
func MustParsePath(s string) Path {
	p, err := ParsePath(s)
	if err != nil {
		panic(err)
	}
	return p
}

var allKinds = []NodeKind{
	KindReports, KindReport, KindModuleCall, KindModuleInstance,
	KindResource, KindAttribute, KindKeyChange, KindTextPlan,
}

func isKnownKind(s string) bool {
	for _, k := range allKinds {
		if string(k) == s {
			return true
		}
	}
	return false
}

func knownKindList() []string {
	out := make([]string, len(allKinds))
	for i, k := range allKinds {
		out[i] = string(k)
	}
	return out
}

// Query walks scope's subtree and returns every node whose ancestor
// chain, in order, contains nodes matching each step of path. The final
// step's kind is the kind of the returned nodes.
//
// Descendant semantics: `a > b` means "b is anywhere below a", not
// strictly a direct child. This mirrors the CSS-space-combinator feel
// and lets users write short paths without knowing the exact nesting
// (e.g. `report > resource` finds every resource in a report, whether
// root-module or deeply nested).
//
// Empty path returns an empty result. Nil scope returns an empty
// result. Matching is deterministic and stable — children are walked
// in slice order.
func Query(scope *Node, path Path) []*Node {
	if scope == nil || len(path) == 0 {
		return nil
	}
	var out []*Node
	queryWalk(scope, path, 0, &out)
	return out
}

// queryWalk is the state-machine recursion. stepIdx is the index of the
// next path step we're looking to satisfy. When stepIdx advances past
// the last step, the current node is emitted.
func queryWalk(n *Node, path Path, stepIdx int, out *[]*Node) {
	if n == nil {
		return
	}
	newIdx := stepIdx
	if stepIdx < len(path) && n.Kind == path[stepIdx] {
		newIdx = stepIdx + 1
	}
	if newIdx == len(path) && newIdx != stepIdx {
		// We just satisfied the final step on this node — emit.
		*out = append(*out, n)
		// Don't descend further for this match; deeper matches of the
		// same path can still fire via other branches since each branch
		// walks with its own stepIdx.
		return
	}
	for _, c := range n.Children {
		queryWalk(c, path, newIdx, out)
	}
}
