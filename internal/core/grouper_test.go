package core

import (
	"os"
	"testing"
)

func TestGroupByModule(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatal(err)
	}

	groups := GroupByModule(changes)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Groups should be sorted by path
	expectedPaths := []string{"module.privatelink", "module.routes", "module.virtual_network"}
	for i, g := range groups {
		if g.Path != expectedPaths[i] {
			t.Errorf("group[%d].Path = %q, want %q", i, g.Path, expectedPaths[i])
		}
	}

	// virtual_network should have 2 updates
	vnetGroup := groups[2]
	if vnetGroup.Name != "virtual_network" {
		t.Errorf("name = %q, want %q", vnetGroup.Name, "virtual_network")
	}
	if len(vnetGroup.Changes) != 2 {
		t.Errorf("changes = %d, want 2", len(vnetGroup.Changes))
	}
	if vnetGroup.ActionCounts[ActionUpdate] != 2 {
		t.Errorf("update count = %d, want 2", vnetGroup.ActionCounts[ActionUpdate])
	}
}

func TestGroupByModuleRootResources(t *testing.T) {
	changes := []ResourceChange{
		{Address: "azurerm_resource_group.rg", ModulePath: "", Action: ActionCreate},
	}

	groups := GroupByModule(changes)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "(root)" {
		t.Errorf("name = %q, want %q", groups[0].Name, "(root)")
	}
}

func TestModuleName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"module.virtual_network", "virtual_network"},
		{"module.a.module.b", "b"},
		{"module.a.module.b.module.c", "c"},
		{"(root)", "(root)"},
		{"", "(root)"},
	}

	for _, tt := range tests {
		got := moduleName(tt.path)
		if got != tt.want {
			t.Errorf("moduleName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestDisambiguateNames(t *testing.T) {
	groups := []ModuleGroup{
		{Name: "zscc_lb", Path: "module.zscc-azci.module.zscc_lb"},
		{Name: "zscc_lb", Path: "module.zscc-azsvcs.module.zscc_lb"},
		{Name: "vnet", Path: "module.vnet"},
	}

	DisambiguateNames(groups)

	if groups[0].Name != "zscc-azci > zscc_lb" {
		t.Errorf("groups[0].Name = %q, want %q", groups[0].Name, "zscc-azci > zscc_lb")
	}
	if groups[1].Name != "zscc-azsvcs > zscc_lb" {
		t.Errorf("groups[1].Name = %q, want %q", groups[1].Name, "zscc-azsvcs > zscc_lb")
	}
	// Non-colliding name should be unchanged
	if groups[2].Name != "vnet" {
		t.Errorf("groups[2].Name = %q, want %q", groups[2].Name, "vnet")
	}
}

func TestDisambiguateNamesThreeLevels(t *testing.T) {
	// When parent context still collides, add grandparent
	groups := []ModuleGroup{
		{Name: "pip", Path: "module.a.module.vm1.module.pip"},
		{Name: "pip", Path: "module.a.module.vm2.module.pip"},
		{Name: "pip", Path: "module.b.module.vm1.module.pip"},
	}

	DisambiguateNames(groups)

	// After first pass: "vm1 > pip" (2x collide), "vm2 > pip" (unique)
	// After second pass: "a > vm1 > pip", "b > vm1 > pip" (resolved)
	if groups[0].Name != "a > vm1 > pip" {
		t.Errorf("groups[0].Name = %q, want %q", groups[0].Name, "a > vm1 > pip")
	}
	if groups[1].Name != "vm2 > pip" {
		t.Errorf("groups[1].Name = %q, want %q", groups[1].Name, "vm2 > pip")
	}
	if groups[2].Name != "b > vm1 > pip" {
		t.Errorf("groups[2].Name = %q, want %q", groups[2].Name, "b > vm1 > pip")
	}
}

func TestTotalActionCounts(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatal(err)
	}

	groups := GroupByModule(changes)
	totals := TotalActionCounts(groups)

	if totals[ActionUpdate] != 2 {
		t.Errorf("update = %d, want 2", totals[ActionUpdate])
	}
	if totals[ActionCreate] != 1 {
		t.Errorf("create = %d, want 1", totals[ActionCreate])
	}
	if totals[ActionDelete] != 1 {
		t.Errorf("delete = %d, want 1", totals[ActionDelete])
	}
}
