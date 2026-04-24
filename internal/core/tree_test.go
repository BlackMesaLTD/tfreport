package core

import (
	"os"
	"reflect"
	"sort"
	"testing"
)

func TestBuildTree_Empty(t *testing.T) {
	tree := BuildTree()
	if tree.Root != nil {
		t.Errorf("Root = %#v, want nil for empty input", tree.Root)
	}
}

// synthReport fabricates a Report with the shape a real pipeline would
// produce — grouped by module path, Module populated, attributes
// attached. Used across tree tests to avoid depending on a fixture file
// for pure-unit assertions.
func synthReport(label string) *Report {
	rootRG := ResourceChange{
		Address: "azurerm_resource_group.rg", ModulePath: "",
		ResourceType: "azurerm_resource_group", ResourceName: "rg",
		Action: ActionCreate, Impact: ImpactLow,
		ChangedAttributes: []ChangedAttribute{{Key: "location"}, {Key: "tags"}},
	}
	nested := ResourceChange{
		Address: "module.platform.module.vnet.azurerm_virtual_network.hub",
		ModulePath: "module.platform.module.vnet",
		ResourceType: "azurerm_virtual_network", ResourceName: "hub",
		Action: ActionUpdate, Impact: ImpactMedium,
		ChangedAttributes: []ChangedAttribute{{Key: "address_space"}, {Key: "tags"}},
	}
	imp := ResourceChange{
		Address: "module.dns.azurerm_private_dns_zone.internal",
		ModulePath: "module.dns",
		ResourceType: "azurerm_private_dns_zone", ResourceName: "internal",
		Action: ActionCreate, Impact: ImpactLow, IsImport: true,
		ChangedAttributes: []ChangedAttribute{{Key: "name"}},
	}
	replaced := ResourceChange{
		Address: "module.compute.azurerm_network_interface.vm",
		ModulePath: "module.compute",
		ResourceType: "azurerm_network_interface", ResourceName: "vm",
		Action: ActionReplace, Impact: ImpactCritical,
		ChangedAttributes: []ChangedAttribute{{Key: "ip_configuration"}},
	}

	r := &Report{
		Label:          label,
		ModuleGroups:   GroupByModule([]ResourceChange{rootRG, nested, imp, replaced}),
		KeyChanges:     []KeyChange{{Text: "Replacing network interface", Impact: ImpactCritical}},
		TotalResources: 4,
		ActionCounts:   map[Action]int{ActionCreate: 2, ActionUpdate: 1, ActionReplace: 1},
		MaxImpact:      ImpactCritical,
		TextPlanBlocks: map[string]string{
			"module.compute.azurerm_network_interface.vm": "# nic replace text plan ...",
		},
	}
	return r
}

func TestBuildTree_SingleReport_Shape(t *testing.T) {
	tree := BuildTree(synthReport("sub-a"))

	if tree.Root == nil || tree.Root.Kind != KindReport {
		t.Fatalf("Root kind = %v, want %v", kindOf(tree.Root), KindReport)
	}

	// 1 KeyChange child + ModuleCall children (platform, dns, compute) +
	// one direct Resource for the root-module resource_group.
	wantKinds := []NodeKind{KindKeyChange, KindResource, KindModuleCall, KindModuleCall, KindModuleCall}
	gotKinds := childKinds(tree.Root)
	sort.Slice(gotKinds, func(i, j int) bool { return string(gotKinds[i]) < string(gotKinds[j]) })
	sort.Slice(wantKinds, func(i, j int) bool { return string(wantKinds[i]) < string(wantKinds[j]) })
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Errorf("root child kinds = %v, want %v", gotKinds, wantKinds)
	}

	// ModuleCall("platform") should have one ModuleInstance child, which
	// in turn should have one ModuleCall("vnet") child — the nested
	// submodule collapsed correctly.
	platform := findByName(tree.Root, KindModuleCall, "platform")
	if platform == nil {
		t.Fatal("platform ModuleCall not found")
	}
	if len(platform.Children) != 1 || platform.Children[0].Kind != KindModuleInstance {
		t.Fatalf("platform children = %v, want 1 ModuleInstance", platform.Children)
	}
	inst := platform.Children[0]
	if len(inst.Children) != 1 || inst.Children[0].Kind != KindModuleCall || inst.Children[0].Name != "vnet" {
		t.Errorf("platform instance children = %v, want 1 ModuleCall(vnet)", inst.Children)
	}
}

func TestBuildTree_Aggregates_RollUp(t *testing.T) {
	tree := BuildTree(synthReport("x"))

	// Root: 4 resources, 1 import, MaxImpact critical
	agg := tree.Root.Agg
	if agg.ResourceCount != 4 {
		t.Errorf("ResourceCount = %d, want 4", agg.ResourceCount)
	}
	if agg.ImportCount != 1 {
		t.Errorf("ImportCount = %d, want 1", agg.ImportCount)
	}
	if agg.MaxImpact != ImpactCritical {
		t.Errorf("MaxImpact = %q, want %q", agg.MaxImpact, ImpactCritical)
	}
	if agg.ActionCounts[ActionCreate] != 2 || agg.ActionCounts[ActionUpdate] != 1 || agg.ActionCounts[ActionReplace] != 1 {
		t.Errorf("ActionCounts = %v", agg.ActionCounts)
	}

	// Changed-attrs union: location, tags, address_space, name, ip_configuration (5 unique)
	wantAttrs := []string{"address_space", "ip_configuration", "location", "name", "tags"}
	if !reflect.DeepEqual(agg.ChangedAttrs, wantAttrs) {
		t.Errorf("ChangedAttrs = %v, want %v", agg.ChangedAttrs, wantAttrs)
	}

	// compute ModuleCall rolls up the replace
	compute := findByName(tree.Root, KindModuleCall, "compute")
	if compute == nil {
		t.Fatal("compute ModuleCall not found")
	}
	if compute.Agg.MaxImpact != ImpactCritical {
		t.Errorf("compute MaxImpact = %q, want %q", compute.Agg.MaxImpact, ImpactCritical)
	}
	if compute.Agg.ResourceCount != 1 {
		t.Errorf("compute ResourceCount = %d, want 1", compute.Agg.ResourceCount)
	}
}

func TestBuildTree_Resource_AttributeChildren(t *testing.T) {
	tree := BuildTree(synthReport("x"))

	// Find the root resource group Resource node
	var rg *Node
	tree.Walk(func(n *Node) bool {
		if n.Kind == KindResource && n.Name == "azurerm_resource_group.rg" {
			rg = n
			return false
		}
		return true
	})
	if rg == nil {
		t.Fatal("resource group Resource node not found")
	}

	// Should have 2 Attribute children (location, tags) and no TextPlan
	// (we put a text plan on the nic, not the rg).
	attrs := childKinds(rg)
	want := []NodeKind{KindAttribute, KindAttribute}
	if !reflect.DeepEqual(attrs, want) {
		t.Errorf("rg child kinds = %v, want %v", attrs, want)
	}
	if rg.Agg.ResourceCount != 1 {
		t.Errorf("rg ResourceCount = %d, want 1", rg.Agg.ResourceCount)
	}
}

func TestBuildTree_Resource_TextPlanChild(t *testing.T) {
	tree := BuildTree(synthReport("x"))

	var nic *Node
	tree.Walk(func(n *Node) bool {
		if n.Kind == KindResource && n.Name == "module.compute.azurerm_network_interface.vm" {
			nic = n
			return false
		}
		return true
	})
	if nic == nil {
		t.Fatal("nic Resource node not found")
	}

	var tp *Node
	for _, c := range nic.Children {
		if c.Kind == KindTextPlan {
			tp = c
		}
	}
	if tp == nil {
		t.Fatal("expected TextPlan child on nic")
	}
	data, ok := tp.Payload.(TextPlanData)
	if !ok || data.Address != nic.Name || data.Body == "" {
		t.Errorf("TextPlan payload = %#v, want non-empty body for address %q", tp.Payload, nic.Name)
	}
}

func TestBuildTree_ForEach_MultipleInstances(t *testing.T) {
	// Two resources in two instances of the same ModuleCall.
	changes := []ResourceChange{
		{
			Address:      `module.zone["internal"].azurerm_record.a`,
			ModulePath:   `module.zone["internal"]`,
			ResourceType: "azurerm_dns_record", ResourceName: "a",
			Action: ActionCreate, Impact: ImpactLow,
		},
		{
			Address:      `module.zone["external"].azurerm_record.a`,
			ModulePath:   `module.zone["external"]`,
			ResourceType: "azurerm_dns_record", ResourceName: "a",
			Action: ActionCreate, Impact: ImpactLow,
		},
	}
	r := &Report{ModuleGroups: GroupByModule(changes)}
	tree := BuildTree(r)

	zone := findByName(tree.Root, KindModuleCall, "zone")
	if zone == nil {
		t.Fatal("zone ModuleCall not found")
	}
	if len(zone.Children) != 2 {
		t.Fatalf("zone has %d instance children, want 2", len(zone.Children))
	}
	gotKeys := []string{zone.Children[0].Name, zone.Children[1].Name}
	sort.Strings(gotKeys)
	wantKeys := []string{`"external"`, `"internal"`}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("instance keys = %v, want %v", gotKeys, wantKeys)
	}
}

func TestBuildTree_MultiReport(t *testing.T) {
	tree := BuildTree(synthReport("sub-a"), synthReport("sub-b"))

	if tree.Root == nil || tree.Root.Kind != KindReports {
		t.Fatalf("Root kind = %v, want %v", kindOf(tree.Root), KindReports)
	}
	if len(tree.Root.Children) != 2 {
		t.Fatalf("Reports has %d children, want 2", len(tree.Root.Children))
	}
	for i, c := range tree.Root.Children {
		if c.Kind != KindReport {
			t.Errorf("child[%d] kind = %q, want report", i, c.Kind)
		}
	}
	// Aggregates at the Reports level: 2x per-report counts.
	if tree.Root.Agg.ResourceCount != 8 {
		t.Errorf("Reports ResourceCount = %d, want 8", tree.Root.Agg.ResourceCount)
	}
	if tree.Root.Agg.MaxImpact != ImpactCritical {
		t.Errorf("Reports MaxImpact = %q, want critical", tree.Root.Agg.MaxImpact)
	}
}

func TestBuildTree_AgainstKitchenSinkFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/kitchen_sink_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := GenerateReport(data, ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	tree := BuildTree(r)

	if tree.Root == nil {
		t.Fatal("nil root")
	}

	// The fixture has 7 resource_changes — but ChangedOnly does NOT drop
	// the read action (data.azurerm_client_config); it drops no-ops only.
	// Expect 7 Resource nodes reachable from root.
	resCount := 0
	tree.Walk(func(n *Node) bool {
		if n.Kind == KindResource {
			resCount++
		}
		return true
	})
	if resCount != 7 {
		t.Errorf("resource nodes = %d, want 7", resCount)
	}

	if tree.Root.Agg.MaxImpact != ImpactCritical {
		t.Errorf("max impact = %q, want critical (replace on nic)", tree.Root.Agg.MaxImpact)
	}
	if tree.Root.Agg.ImportCount != 1 {
		t.Errorf("import count = %d, want 1 (private dns zone import)", tree.Root.Agg.ImportCount)
	}
}

func TestNode_EnsureMeta(t *testing.T) {
	n := &Node{Kind: KindReport}
	if n.Meta != nil {
		t.Fatal("Meta should be nil on fresh node")
	}
	m := n.EnsureMeta()
	m["cost"] = 42
	if n.Meta["cost"] != 42 {
		t.Errorf("Meta[cost] = %v, want 42", n.Meta["cost"])
	}
	// second call returns the same map
	if got := n.EnsureMeta(); !reflect.DeepEqual(got, n.Meta) {
		t.Error("EnsureMeta returned a different map on second call")
	}
}

func TestPlanTree_Walk_Stops(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	visited := 0
	tree.Walk(func(n *Node) bool {
		visited++
		return n.Kind != KindModuleCall // stop at first ModuleCall
	})
	if visited == 0 {
		t.Error("Walk visited 0 nodes")
	}
}

func TestPlanTree_Find(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	if n := tree.Find(KindKeyChange); n == nil || n.Kind != KindKeyChange {
		t.Errorf("Find(KeyChange) = %v, want KeyChange node", n)
	}
	if n := tree.Find(NodeKind("bogus")); n != nil {
		t.Errorf("Find(bogus) = %v, want nil", n)
	}
}

// --- helpers ---

func childKinds(n *Node) []NodeKind {
	if n == nil {
		return nil
	}
	out := make([]NodeKind, len(n.Children))
	for i, c := range n.Children {
		out[i] = c.Kind
	}
	return out
}

func findByName(root *Node, kind NodeKind, name string) *Node {
	var found *Node
	var walkFn func(*Node)
	walkFn = func(n *Node) {
		if n.Kind == kind && n.Name == name {
			found = n
			return
		}
		for _, c := range n.Children {
			if found != nil {
				return
			}
			walkFn(c)
		}
	}
	walkFn(root)
	return found
}

func kindOf(n *Node) NodeKind {
	if n == nil {
		return ""
	}
	return n.Kind
}
