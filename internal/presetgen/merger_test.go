package presetgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tfreport/tfreport/internal/presets"
)

func TestMerge_DocsOnly(t *testing.T) {
	parsed, err := ParseDocsDir(testdataDir(), "azurerm", nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Merge(MergeOptions{
		Provider:   "azurerm",
		Version:    "4.46.0",
		DocsParsed: parsed,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if result.Provider != "azurerm" {
		t.Errorf("provider = %q", result.Provider)
	}

	subnet, ok := result.Resources["azurerm_subnet"]
	if !ok {
		t.Fatal("azurerm_subnet not in result")
	}

	nameAttr, ok := subnet.Attributes["name"]
	if !ok {
		t.Fatal("name not in subnet attributes")
	}
	if !nameAttr.ForceNew {
		t.Error("name should be force_new")
	}
}

func TestMerge_WithExistingPreset(t *testing.T) {
	parsed, err := ParseDocsDir(testdataDir(), "azurerm", nil)
	if err != nil {
		t.Fatal(err)
	}

	existing := &presets.Preset{
		Provider: "azurerm",
		Version:  "4.x",
		Resources: map[string]presets.ResourcePreset{
			"azurerm_subnet": {
				DisplayName: "subnet",
			},
			"azurerm_lb": {
				DisplayName: "load balancer",
			},
		},
	}

	result, err := Merge(MergeOptions{
		Provider:       "azurerm",
		Version:        "4.46.0",
		ExistingPreset: existing,
		DocsParsed:     parsed,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Display name preserved from existing
	subnet := result.Resources["azurerm_subnet"]
	if subnet.DisplayName != "subnet" {
		t.Errorf("display name = %q, want subnet", subnet.DisplayName)
	}

	// Attributes enriched from docs
	if _, ok := subnet.Attributes["name"]; !ok {
		t.Error("name attribute should be added from docs")
	}

	// Resource not in docs is preserved from existing
	lb, ok := result.Resources["azurerm_lb"]
	if !ok {
		t.Fatal("azurerm_lb should be preserved from existing")
	}
	if lb.DisplayName != "load balancer" {
		t.Errorf("lb display name = %q", lb.DisplayName)
	}
}

func TestMarshalPreset(t *testing.T) {
	p := &presets.Preset{
		Provider: "test",
		Version:  "1.0",
		Resources: map[string]presets.ResourcePreset{
			"test_resource": {
				DisplayName: "test resource",
				Attributes: map[string]presets.AttributePreset{
					"name": {Description: "The name.", ForceNew: true},
				},
			},
		},
	}

	data, err := MarshalPreset(p)
	if err != nil {
		t.Fatalf("MarshalPreset: %v", err)
	}

	// Verify it's valid JSON
	var parsed presets.Preset
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed.Provider != "test" {
		t.Errorf("provider = %q", parsed.Provider)
	}
}

func TestMerge_WithSchemaFile(t *testing.T) {
	// Create a minimal schema file
	schema := providerSchema{
		ProviderSchemas: map[string]providerSchemaEntry{
			"registry.terraform.io/hashicorp/azurerm": {
				ResourceSchemas: map[string]resourceSchema{
					"azurerm_subnet": {
						Block: blockSchema{
							Attributes: map[string]schemaAttribute{
								"id": {
									Description: "The ID of the subnet.",
									Computed:    true,
								},
								"name": {
									Description: "Schema description for name.",
									Required:    true,
								},
							},
						},
					},
				},
			},
		},
	}

	schemaData, _ := json.Marshal(schema)
	tmpFile := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(tmpFile, schemaData, 0644); err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseDocsDir(testdataDir(), "azurerm", nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Merge(MergeOptions{
		Provider:   "azurerm",
		Version:    "4.46.0",
		DocsParsed: parsed,
		SchemaFile: tmpFile,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	subnet := result.Resources["azurerm_subnet"]

	// Doc description should win over schema description
	nameAttr := subnet.Attributes["name"]
	if nameAttr.Description == "Schema description for name." {
		t.Error("doc description should take precedence over schema description")
	}
}

func TestMatchesProvider(t *testing.T) {
	tests := []struct {
		key      string
		provider string
		want     bool
	}{
		{"azurerm", "azurerm", true},
		{"registry.terraform.io/hashicorp/azurerm", "azurerm", true},
		{"registry.terraform.io/hashicorp/aws", "azurerm", false},
		{"aws", "azurerm", false},
	}

	for _, tt := range tests {
		got := matchesProvider(tt.key, tt.provider)
		if got != tt.want {
			t.Errorf("matchesProvider(%q, %q) = %v, want %v", tt.key, tt.provider, got, tt.want)
		}
	}
}
