package core

import "sort"

// PlanTree is the structured view of one or more reports as a single
// typed tree. It exists alongside the legacy *Report data model, not as a
// replacement — downstream code migrates incrementally.
//
// Every meaningful entity — report, module call, module instance, resource,
// changed attribute, key change, text-plan block — is a first-class Node
// with the same shape. Aggregates (resource counts, action breakdowns, max
// impact, changed-attribute union, import count) are pre-rolled at every
// intermediate node during construction so reads are O(1).
type PlanTree struct {
	Root *Node
}

// NodeKind identifies the role a Node plays in the tree. The set is
// closed: callers switch over these kinds rather than introducing new
// ones. New semantic entities belong in Node.Meta.
type NodeKind string

const (
	KindReports        NodeKind = "reports"         // multi-report wrapper
	KindReport         NodeKind = "report"          // one terraform plan
	KindModuleCall     NodeKind = "module_call"     // a `module "x" {}` declaration
	KindModuleInstance NodeKind = "module_instance" // one for_each/count instance of a call
	KindResource       NodeKind = "resource"        // one terraform resource
	KindAttribute      NodeKind = "attribute"       // one changed attribute on a resource
	KindKeyChange      NodeKind = "key_change"      // one summarizer sentence
	KindTextPlan       NodeKind = "text_plan"       // raw terraform text for one resource
)

// Node is a single element in the PlanTree. Parent is nil only for the
// PlanTree.Root node. Children are ordered — traversal order is stable
// across rebuilds for the same input.
type Node struct {
	Kind     NodeKind
	Name     string // identity within parent scope (module call name, resource address, attr key, …)
	Parent   *Node
	Children []*Node
	// Meta is a free-form bag for external data (cost attributions, policy
	// violations, team tags, raw terraform-json bytes). Lazily allocated —
	// nil until a caller touches it via EnsureMeta.
	Meta map[string]any
	// Agg carries pre-rolled aggregates. Present on every node except
	// pure leaves (Attribute, TextPlan) where all fields remain zero.
	Agg Aggregates
	// Payload holds the kind-specific domain value. Readers type-assert
	// based on Kind:
	//   KindReports        -> []*Report
	//   KindReport         -> *Report
	//   KindModuleCall     -> nil (identity via Name)
	//   KindModuleInstance -> nil (identity via Name)
	//   KindResource       -> *ResourceChange
	//   KindAttribute      -> *ChangedAttribute
	//   KindKeyChange      -> *KeyChange
	//   KindTextPlan       -> TextPlanData
	Payload any
}

// Aggregates summarises a sub-tree. Pre-rolled at build time so downstream
// queries don't have to walk the tree for common counts.
type Aggregates struct {
	ResourceCount int
	ImportCount   int
	ActionCounts  map[Action]int
	MaxImpact     Impact
	// ChangedAttrs is the sorted union of attribute keys changed by any
	// resource in this sub-tree. Deduplicated.
	ChangedAttrs []string
}

// TextPlanData is the Payload for KindTextPlan nodes.
type TextPlanData struct {
	Address string
	Body    string
}

// EnsureMeta initialises Node.Meta on first touch and returns it. Callers
// can treat the return value as the live map; appending to it mutates
// the node.
func (n *Node) EnsureMeta() map[string]any {
	if n.Meta == nil {
		n.Meta = make(map[string]any)
	}
	return n.Meta
}

// BuildTree assembles a PlanTree from one or more reports. Zero reports
// returns an empty tree (Root == nil). One report produces a Report-rooted
// tree; multiple reports produce a Reports-rooted tree whose children are
// the individual Report sub-trees in the order supplied.
func BuildTree(reports ...*Report) *PlanTree {
	switch len(reports) {
	case 0:
		return &PlanTree{}
	case 1:
		root := buildReportNode(reports[0])
		rollUp(root)
		return &PlanTree{Root: root}
	default:
		root := &Node{Kind: KindReports, Name: "reports", Payload: reports}
		for _, r := range reports {
			child := buildReportNode(r)
			child.Parent = root
			root.Children = append(root.Children, child)
		}
		rollUp(root)
		return &PlanTree{Root: root}
	}
}

// buildReportNode constructs the sub-tree for one Report without rolling
// up aggregates (rollUp runs once at the outermost layer).
func buildReportNode(r *Report) *Node {
	node := &Node{
		Kind:    KindReport,
		Name:    r.Label,
		Payload: r,
	}

	// Key changes attach directly to the report.
	for i := range r.KeyChanges {
		kc := &r.KeyChanges[i]
		node.Children = append(node.Children, &Node{
			Kind:    KindKeyChange,
			Name:    kc.Text,
			Parent:  node,
			Payload: kc,
			Agg:     Aggregates{MaxImpact: kc.Impact},
		})
	}

	// Walk every module group and insert its resources at the right
	// depth. ModuleCall / ModuleInstance nodes are created on demand and
	// shared across groups that live under the same call.
	for i := range r.ModuleGroups {
		mg := &r.ModuleGroups[i]
		parent := insertModulePath(node, mg.Module)
		for j := range mg.Changes {
			parent.Children = append(parent.Children, buildResourceNode(r, &mg.Changes[j], parent))
		}
	}

	return node
}

// buildResourceNode builds a Resource node with its Attribute children
// and an optional TextPlan child when the report carries text for that
// resource's address.
func buildResourceNode(r *Report, rc *ResourceChange, parent *Node) *Node {
	node := &Node{
		Kind:    KindResource,
		Name:    rc.Address,
		Parent:  parent,
		Payload: rc,
	}
	for i := range rc.ChangedAttributes {
		attr := &rc.ChangedAttributes[i]
		node.Children = append(node.Children, &Node{
			Kind:    KindAttribute,
			Name:    attr.Key,
			Parent:  node,
			Payload: attr,
		})
	}
	if text, ok := r.TextPlanBlocks[rc.Address]; ok && text != "" {
		node.Children = append(node.Children, &Node{
			Kind:    KindTextPlan,
			Name:    rc.Address,
			Parent:  node,
			Payload: TextPlanData{Address: rc.Address, Body: text},
		})
	}
	return node
}

// insertModulePath walks the segments of a module address, finding or
// creating ModuleCall + ModuleInstance nodes as needed, and returns the
// deepest ModuleInstance. For root-module resources, returns the supplied
// root unchanged.
//
// Nodes with the same (Kind, Name) inside the same parent are reused, so
// many resources sharing a prefix collapse onto one sub-tree. ModuleCall
// keys on segment.Name; ModuleInstance keys on segment.Instance (the raw
// bracket contents, empty for single-instance modules).
func insertModulePath(root *Node, m Module) *Node {
	if m.IsRoot() {
		return root
	}
	current := root
	for _, seg := range m.Segments {
		call := findChild(current, KindModuleCall, seg.Name)
		if call == nil {
			call = &Node{Kind: KindModuleCall, Name: seg.Name, Parent: current}
			current.Children = append(current.Children, call)
		}
		inst := findChild(call, KindModuleInstance, seg.Instance)
		if inst == nil {
			inst = &Node{Kind: KindModuleInstance, Name: seg.Instance, Parent: call}
			call.Children = append(call.Children, inst)
		}
		current = inst
	}
	return current
}

func findChild(parent *Node, kind NodeKind, name string) *Node {
	for _, c := range parent.Children {
		if c.Kind == kind && c.Name == name {
			return c
		}
	}
	return nil
}

// rollUp fills Aggregates on every intermediate node by walking the tree
// depth-first post-order. Leaves set their self-aggregates; intermediate
// nodes sum their children's.
func rollUp(node *Node) {
	if node == nil {
		return
	}
	for _, c := range node.Children {
		rollUp(c)
	}

	switch node.Kind {
	case KindAttribute, KindTextPlan:
		return // pure leaves — all aggregate fields stay zero
	case KindKeyChange:
		return // MaxImpact set at construction; counts irrelevant
	case KindResource:
		rc, _ := node.Payload.(*ResourceChange)
		if rc == nil {
			return
		}
		node.Agg.ResourceCount = 1
		node.Agg.ActionCounts = map[Action]int{rc.Action: 1}
		if rc.IsImport {
			node.Agg.ImportCount = 1
		}
		node.Agg.MaxImpact = rc.Impact
		for _, c := range node.Children {
			if c.Kind == KindAttribute {
				node.Agg.ChangedAttrs = append(node.Agg.ChangedAttrs, c.Name)
			}
		}
		sort.Strings(node.Agg.ChangedAttrs)
	default:
		// ModuleCall, ModuleInstance, Report, Reports
		sum := Aggregates{ActionCounts: map[Action]int{}}
		attrSet := map[string]struct{}{}
		for _, c := range node.Children {
			sum.ResourceCount += c.Agg.ResourceCount
			sum.ImportCount += c.Agg.ImportCount
			for a, n := range c.Agg.ActionCounts {
				sum.ActionCounts[a] += n
			}
			if impactRank(c.Agg.MaxImpact) > impactRank(sum.MaxImpact) {
				sum.MaxImpact = c.Agg.MaxImpact
			}
			for _, k := range c.Agg.ChangedAttrs {
				attrSet[k] = struct{}{}
			}
		}
		keys := make([]string, 0, len(attrSet))
		for k := range attrSet {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sum.ChangedAttrs = keys
		node.Agg = sum
	}
}

// impactRank orders Impact values so maxImpact via numeric comparison is
// correct: critical > high > medium > low > none.
func impactRank(i Impact) int {
	switch i {
	case ImpactCritical:
		return 4
	case ImpactHigh:
		return 3
	case ImpactMedium:
		return 2
	case ImpactLow:
		return 1
	default:
		return 0
	}
}

// Walk invokes fn for every node in the tree in pre-order. If fn returns
// false the traversal stops. Safe to call on a tree with a nil Root (no-op).
func (t *PlanTree) Walk(fn func(*Node) bool) {
	if t == nil || t.Root == nil || fn == nil {
		return
	}
	walk(t.Root, fn)
}

func walk(n *Node, fn func(*Node) bool) bool {
	if !fn(n) {
		return false
	}
	for _, c := range n.Children {
		if !walk(c, fn) {
			return false
		}
	}
	return true
}

// Find returns the first node of the given kind encountered in a
// pre-order walk, or nil if none exists. Provided for tests and one-off
// lookups; query-engine workloads should use the path language.
func (t *PlanTree) Find(kind NodeKind) *Node {
	var found *Node
	t.Walk(func(n *Node) bool {
		if n.Kind == kind {
			found = n
			return false
		}
		return true
	})
	return found
}
