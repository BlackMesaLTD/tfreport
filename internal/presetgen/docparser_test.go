package presetgen

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

func TestParseDocFile_Subnet(t *testing.T) {
	path := filepath.Join(testdataDir(), "subnet.html.markdown")
	parsed, err := ParseDocFile(path, "azurerm")
	if err != nil {
		t.Fatalf("ParseDocFile: %v", err)
	}

	if parsed.ResourceType != "azurerm_subnet" {
		t.Errorf("resource type = %q, want azurerm_subnet", parsed.ResourceType)
	}

	// Check we got top-level attributes
	attrMap := make(map[string]ParsedAttribute)
	for _, a := range parsed.Attributes {
		key := a.Name
		if a.Block != "" {
			key = a.Block + "." + a.Name
		}
		attrMap[key] = a
	}

	// name should be force_new
	name, ok := attrMap["name"]
	if !ok {
		t.Fatal("name attribute not found")
	}
	if !name.ForceNew {
		t.Error("name should be force_new")
	}
	if !name.Required {
		t.Error("name should be required")
	}

	// address_prefixes should NOT be force_new
	addr, ok := attrMap["address_prefixes"]
	if !ok {
		t.Fatal("address_prefixes not found")
	}
	if addr.ForceNew {
		t.Error("address_prefixes should not be force_new")
	}

	// delegation should be optional
	delegation, ok := attrMap["delegation"]
	if !ok {
		t.Fatal("delegation not found")
	}
	if delegation.Required {
		t.Error("delegation should be optional")
	}

	// Nested block: delegation.name
	delegationName, ok := attrMap["delegation.name"]
	if !ok {
		t.Fatal("delegation.name not found")
	}
	if delegationName.Block != "delegation" {
		t.Errorf("delegation.name block = %q, want delegation", delegationName.Block)
	}

	// service_delegation.name (nested under service_delegation block)
	sdName, ok := attrMap["service_delegation.name"]
	if !ok {
		t.Fatal("service_delegation.name not found")
	}
	if sdName.Block != "service_delegation" {
		t.Errorf("service_delegation.name block = %q", sdName.Block)
	}
}

func TestParseDocFile_VirtualNetwork(t *testing.T) {
	path := filepath.Join(testdataDir(), "virtual_network.html.markdown")
	parsed, err := ParseDocFile(path, "azurerm")
	if err != nil {
		t.Fatalf("ParseDocFile: %v", err)
	}

	if parsed.ResourceType != "azurerm_virtual_network" {
		t.Errorf("resource type = %q", parsed.ResourceType)
	}

	attrMap := make(map[string]ParsedAttribute)
	for _, a := range parsed.Attributes {
		attrMap[a.Name] = a
	}

	// name: force_new
	if !attrMap["name"].ForceNew {
		t.Error("name should be force_new")
	}

	// tags: not force_new
	tags, ok := attrMap["tags"]
	if !ok {
		t.Fatal("tags not found")
	}
	if tags.ForceNew {
		t.Error("tags should not be force_new")
	}
	if tags.Required {
		t.Error("tags should be optional")
	}

	// address_space: not force_new
	if attrMap["address_space"].ForceNew {
		t.Error("address_space should not be force_new")
	}
}

func TestParseDocsDir(t *testing.T) {
	results, err := ParseDocsDir(testdataDir(), "azurerm", nil)
	if err != nil {
		t.Fatalf("ParseDocsDir: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 resources, got %d", len(results))
	}

	if _, ok := results["azurerm_subnet"]; !ok {
		t.Error("azurerm_subnet not found")
	}
	if _, ok := results["azurerm_virtual_network"]; !ok {
		t.Error("azurerm_virtual_network not found")
	}
}

func TestParseDocsDirWithFilter(t *testing.T) {
	filter := map[string]bool{
		"azurerm_subnet": true,
	}

	results, err := ParseDocsDir(testdataDir(), "azurerm", filter)
	if err != nil {
		t.Fatalf("ParseDocsDir: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 resource, got %d", len(results))
	}

	if _, ok := results["azurerm_subnet"]; !ok {
		t.Error("azurerm_subnet not found")
	}
}

func TestToPresetResources(t *testing.T) {
	results, err := ParseDocsDir(testdataDir(), "azurerm", nil)
	if err != nil {
		t.Fatal(err)
	}

	presetResources := ToPresetResources(results)

	subnet, ok := presetResources["azurerm_subnet"]
	if !ok {
		t.Fatal("azurerm_subnet not in preset resources")
	}

	nameAttr, ok := subnet.Attributes["name"]
	if !ok {
		t.Fatal("name not in subnet attributes")
	}
	if !nameAttr.ForceNew {
		t.Error("name should be force_new")
	}
	if nameAttr.Description == "" {
		t.Error("name should have a description")
	}

	// Nested block attribute uses dot notation
	delegationName, ok := subnet.Attributes["delegation.name"]
	if !ok {
		t.Fatal("delegation.name not in subnet attributes")
	}
	if delegationName.Description == "" {
		t.Error("delegation.name should have a description")
	}
}

func TestCleanDescription(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"The name of the subnet. Changing this forces a new resource to be created.",
			"The name of the subnet",
		},
		{
			"The address prefixes to use for the subnet.",
			"The address prefixes to use for the subnet.",
		},
		{
			"A mapping of tags to assign to the resource.",
			"A mapping of tags to assign to the resource.",
		},
	}

	for _, tt := range tests {
		got := cleanDescription(tt.input)
		if got != tt.want {
			t.Errorf("cleanDescription(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsForceNew(t *testing.T) {
	tests := []struct {
		desc string
		want bool
	}{
		{"The name of the subnet. Changing this forces a new resource to be created.", true},
		{"Forces a new resource to be created.", true},
		{"The address prefixes to use for the subnet.", false},
		{"A mapping of tags to assign to the resource.", false},
	}

	for _, tt := range tests {
		got := isForceNew(tt.desc)
		if got != tt.want {
			t.Errorf("isForceNew(%q) = %v, want %v", tt.desc, got, tt.want)
		}
	}
}
