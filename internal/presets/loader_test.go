package presets

import "testing"

func TestLoadBuiltinAzurerm(t *testing.T) {
	p, err := Load("azurerm")
	if err != nil {
		t.Fatalf("Load(azurerm): %v", err)
	}

	if p.Provider != "azurerm" {
		t.Errorf("provider = %q, want azurerm", p.Provider)
	}

	// Check a known resource
	subnet, ok := p.Resources["azurerm_subnet"]
	if !ok {
		t.Fatal("azurerm_subnet not found in preset")
	}
	if subnet.DisplayName != "subnet" {
		t.Errorf("subnet display name = %q, want %q", subnet.DisplayName, "subnet")
	}

	// Check count is reasonable
	if len(p.Resources) < 50 {
		t.Errorf("expected 50+ resources, got %d", len(p.Resources))
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("nonexistent")
	if err == nil {
		t.Error("expected error for missing preset")
	}
}

func TestDisplayNames(t *testing.T) {
	p, err := Load("azurerm")
	if err != nil {
		t.Fatal(err)
	}

	names := DisplayNames(p)

	if names["azurerm_subnet"] != "subnet" {
		t.Errorf("subnet = %q", names["azurerm_subnet"])
	}
	if names["azurerm_lb"] != "load balancer" {
		t.Errorf("lb = %q", names["azurerm_lb"])
	}
}

func TestLoadBuiltinAzurermEnriched(t *testing.T) {
	p, err := Load("azurerm")
	if err != nil {
		t.Fatal(err)
	}

	subnet, ok := p.Resources["azurerm_subnet"]
	if !ok {
		t.Fatal("azurerm_subnet not found")
	}

	// Verify enriched attribute data is present
	nameAttr, ok := subnet.Attributes["name"]
	if !ok {
		t.Fatal("name attribute not found on subnet")
	}
	if !nameAttr.ForceNew {
		t.Error("subnet.name should have force_new=true")
	}
	if nameAttr.Description == "" {
		t.Error("subnet.name should have a description")
	}

	// tags should NOT be force_new
	vnet := p.Resources["azurerm_virtual_network"]
	tagsAttr, ok := vnet.Attributes["tags"]
	if !ok {
		t.Fatal("tags attribute not found on virtual_network")
	}
	if tagsAttr.ForceNew {
		t.Error("virtual_network.tags should not be force_new")
	}
}

func TestForceNewResolver(t *testing.T) {
	p := &Preset{
		Resources: map[string]ResourcePreset{
			"azurerm_subnet": {
				DisplayName: "subnet",
				Attributes: map[string]AttributePreset{
					"name":             {ForceNew: true, Description: "The name of the subnet."},
					"address_prefixes": {ForceNew: false, Description: "The address prefixes to use."},
				},
			},
		},
	}

	resolver := ForceNewResolver(p)

	// Known force_new attribute
	forceNew, ok := resolver("azurerm_subnet", "name")
	if !ok || !forceNew {
		t.Errorf("subnet.name = forceNew:%v found:%v, want true/true", forceNew, ok)
	}

	// Known non-force_new attribute
	forceNew, ok = resolver("azurerm_subnet", "address_prefixes")
	if !ok || forceNew {
		t.Errorf("subnet.address_prefixes = forceNew:%v found:%v, want false/true", forceNew, ok)
	}

	// Unknown attribute
	_, ok = resolver("azurerm_subnet", "unknown_attr")
	if ok {
		t.Error("expected not found for unknown attribute")
	}

	// Unknown resource type
	_, ok = resolver("azurerm_unknown", "name")
	if ok {
		t.Error("expected not found for unknown resource type")
	}
}

func TestDescriptionResolver(t *testing.T) {
	p := &Preset{
		Resources: map[string]ResourcePreset{
			"azurerm_subnet": {
				Attributes: map[string]AttributePreset{
					"name":             {Description: "The name of the subnet."},
					"address_prefixes": {Description: "The address prefixes to use."},
					"tags":             {},
				},
			},
		},
	}

	resolver := DescriptionResolver(p)

	// Known attribute with description
	if desc := resolver("azurerm_subnet", "name"); desc != "The name of the subnet." {
		t.Errorf("subnet.name desc = %q, want %q", desc, "The name of the subnet.")
	}

	// Known attribute with description
	if desc := resolver("azurerm_subnet", "address_prefixes"); desc != "The address prefixes to use." {
		t.Errorf("subnet.address_prefixes desc = %q, want %q", desc, "The address prefixes to use.")
	}

	// Known attribute without description
	if desc := resolver("azurerm_subnet", "tags"); desc != "" {
		t.Errorf("subnet.tags desc = %q, want empty", desc)
	}

	// Unknown attribute
	if desc := resolver("azurerm_subnet", "unknown"); desc != "" {
		t.Errorf("unknown attr desc = %q, want empty", desc)
	}

	// Unknown resource type
	if desc := resolver("azurerm_unknown", "name"); desc != "" {
		t.Errorf("unknown resource desc = %q, want empty", desc)
	}
}

func TestDescriptionResolverMultiplePresets(t *testing.T) {
	p1 := &Preset{
		Resources: map[string]ResourcePreset{
			"azurerm_subnet": {
				Attributes: map[string]AttributePreset{
					"name": {Description: "From first preset"},
				},
			},
		},
	}
	p2 := &Preset{
		Resources: map[string]ResourcePreset{
			"azurerm_subnet": {
				Attributes: map[string]AttributePreset{
					"name": {Description: "From second preset"},
					"tags": {Description: "A mapping of tags."},
				},
			},
		},
	}

	resolver := DescriptionResolver(p1, p2)

	// First preset wins
	if desc := resolver("azurerm_subnet", "name"); desc != "From first preset" {
		t.Errorf("name desc = %q, want %q", desc, "From first preset")
	}

	// Falls through to second preset
	if desc := resolver("azurerm_subnet", "tags"); desc != "A mapping of tags." {
		t.Errorf("tags desc = %q, want %q", desc, "A mapping of tags.")
	}
}
