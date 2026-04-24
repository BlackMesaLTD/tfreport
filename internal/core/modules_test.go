package core

import (
	"reflect"
	"testing"
)

func TestParseModuleAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		segs    []ModuleSegment
		wantStr string
		isRoot  bool
		depth   int
	}{
		{
			name:    "empty string is root",
			addr:    "",
			segs:    nil,
			wantStr: "(root)",
			isRoot:  true,
			depth:   0,
		},
		{
			name:    "sentinel (root) is root",
			addr:    "(root)",
			segs:    nil,
			wantStr: "(root)",
			isRoot:  true,
			depth:   0,
		},
		{
			name:    "single segment",
			addr:    "module.vnet",
			segs:    []ModuleSegment{{Name: "vnet"}},
			wantStr: "module.vnet",
			depth:   1,
		},
		{
			name:    "two nested segments",
			addr:    "module.platform.module.vnet",
			segs:    []ModuleSegment{{Name: "platform"}, {Name: "vnet"}},
			wantStr: "module.platform.module.vnet",
			depth:   2,
		},
		{
			name:    "for_each with quoted string key",
			addr:    `module.nsg["app"]`,
			segs:    []ModuleSegment{{Name: "nsg", Instance: `"app"`}},
			wantStr: `module.nsg["app"]`,
			depth:   1,
		},
		{
			name:    "for_each with dots inside string key",
			addr:    `module.dns.module.zone["privatelink.adf.azure.com"]`,
			segs:    []ModuleSegment{{Name: "dns"}, {Name: "zone", Instance: `"privatelink.adf.azure.com"`}},
			wantStr: `module.dns.module.zone["privatelink.adf.azure.com"]`,
			depth:   2,
		},
		{
			name:    "count with numeric index",
			addr:    "module.spoke[0]",
			segs:    []ModuleSegment{{Name: "spoke", Instance: "0"}},
			wantStr: "module.spoke[0]",
			depth:   1,
		},
		{
			name:    "mixed nesting — instance then plain then instance",
			addr:    `module.a["x"].module.b.module.c[3]`,
			segs:    []ModuleSegment{{Name: "a", Instance: `"x"`}, {Name: "b"}, {Name: "c", Instance: "3"}},
			wantStr: `module.a["x"].module.b.module.c[3]`,
			depth:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ParseModuleAddress(tt.addr)
			if !reflect.DeepEqual(m.Segments, tt.segs) {
				t.Errorf("Segments = %#v, want %#v", m.Segments, tt.segs)
			}
			if m.String() != tt.wantStr {
				t.Errorf("String() = %q, want %q", m.String(), tt.wantStr)
			}
			if m.IsRoot() != tt.isRoot {
				t.Errorf("IsRoot() = %v, want %v", m.IsRoot(), tt.isRoot)
			}
			if m.Depth() != tt.depth {
				t.Errorf("Depth() = %d, want %d", m.Depth(), tt.depth)
			}
			if m.Path() != m.Address {
				t.Errorf("Path() = %q, Address = %q, want equal", m.Path(), m.Address)
			}
		})
	}
}

func TestModuleAccessors_First_Last(t *testing.T) {
	m := ParseModuleAddress("module.platform.module.vnet.module.subnet")

	if got := m.First(); got.Name != "platform" {
		t.Errorf("First().Name = %q, want %q", got.Name, "platform")
	}
	if got := m.Last(); got.Name != "subnet" {
		t.Errorf("Last().Name = %q, want %q", got.Name, "subnet")
	}

	root := ParseModuleAddress("")
	if got := root.First(); got != (ModuleSegment{}) {
		t.Errorf("root First() = %#v, want zero", got)
	}
	if got := root.Last(); got != (ModuleSegment{}) {
		t.Errorf("root Last() = %#v, want zero", got)
	}
}

func TestModule_Segment_BoundsCheck(t *testing.T) {
	m := ParseModuleAddress("module.a.module.b")

	if s, ok := m.Segment(0); !ok || s.Name != "a" {
		t.Errorf("Segment(0) = %#v ok=%v, want a,true", s, ok)
	}
	if s, ok := m.Segment(1); !ok || s.Name != "b" {
		t.Errorf("Segment(1) = %#v ok=%v, want b,true", s, ok)
	}
	if _, ok := m.Segment(2); ok {
		t.Errorf("Segment(2) ok=true, want false (out of range)")
	}
	if _, ok := m.Segment(-1); ok {
		t.Errorf("Segment(-1) ok=true, want false (negative)")
	}
}

// TestModuleGroup_ModuleField_PopulatedFromPlan verifies the grouper wires
// the structured Module onto every ModuleGroup so downstream consumers can
// navigate segments without re-parsing Path.
func TestModuleGroup_ModuleField_PopulatedFromPlan(t *testing.T) {
	changes := []ResourceChange{
		{Address: "azurerm_resource_group.rg", ModulePath: "", ResourceType: "azurerm_resource_group", Action: ActionCreate},
		{Address: "module.vnet.azurerm_subnet.app", ModulePath: "module.vnet", ResourceType: "azurerm_subnet", Action: ActionUpdate},
		{Address: `module.dns.module.zone["internal"].azurerm_record.a`, ModulePath: `module.dns.module.zone["internal"]`, ResourceType: "azurerm_record", Action: ActionCreate},
	}

	groups := GroupByModule(changes)

	if len(groups) != 3 {
		t.Fatalf("groups count = %d, want 3", len(groups))
	}

	want := map[string]Module{
		"(root)":                                 {Address: "", Segments: nil},
		"module.vnet":                            ParseModuleAddress("module.vnet"),
		`module.dns.module.zone["internal"]`:     ParseModuleAddress(`module.dns.module.zone["internal"]`),
	}

	for _, g := range groups {
		expected, ok := want[g.Path]
		if !ok {
			t.Errorf("unexpected group path %q", g.Path)
			continue
		}
		if !reflect.DeepEqual(g.Module, expected) {
			t.Errorf("group %q Module = %#v, want %#v", g.Path, g.Module, expected)
		}
	}
}
