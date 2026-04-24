package core

import (
	"os"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

func TestParsePlan(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}

	if len(changes) != 4 {
		t.Fatalf("expected 4 resource changes, got %d", len(changes))
	}

	// Verify first resource (update)
	rc := changes[0]
	if rc.Address != "module.virtual_network.azurerm_subnet.app" {
		t.Errorf("address = %q, want %q", rc.Address, "module.virtual_network.azurerm_subnet.app")
	}
	if rc.ModulePath != "module.virtual_network" {
		t.Errorf("module path = %q, want %q", rc.ModulePath, "module.virtual_network")
	}
	if rc.ResourceType != "azurerm_subnet" {
		t.Errorf("type = %q, want %q", rc.ResourceType, "azurerm_subnet")
	}
	if rc.ResourceName != "app" {
		t.Errorf("name = %q, want %q", rc.ResourceName, "app")
	}
	if rc.Action != ActionUpdate {
		t.Errorf("action = %q, want %q", rc.Action, ActionUpdate)
	}
	if rc.Impact != ImpactMedium {
		t.Errorf("impact = %q, want %q", rc.Impact, ImpactMedium)
	}

	// Verify create
	rc = changes[2]
	if rc.Action != ActionCreate {
		t.Errorf("action = %q, want %q", rc.Action, ActionCreate)
	}
	if rc.Impact != ImpactLow {
		t.Errorf("impact = %q, want %q", rc.Impact, ImpactLow)
	}
	if rc.ModulePath != "module.privatelink" {
		t.Errorf("module path = %q, want %q", rc.ModulePath, "module.privatelink")
	}

	// Verify delete
	rc = changes[3]
	if rc.Action != ActionDelete {
		t.Errorf("action = %q, want %q", rc.Action, ActionDelete)
	}
	if rc.Impact != ImpactHigh {
		t.Errorf("impact = %q, want %q", rc.Impact, ImpactHigh)
	}
}

func TestParsePlanReplace(t *testing.T) {
	data := []byte(`{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "azurerm_subnet.test",
			"type": "azurerm_subnet",
			"name": "test",
			"provider_name": "registry.terraform.io/hashicorp/azurerm",
			"change": {
				"actions": ["delete", "create"],
				"before": {"name": "old"},
				"after": {"name": "new"},
				"after_unknown": {},
				"before_sensitive": {},
				"after_sensitive": {}
			}
		}],
		"configuration": {"root_module": {}}
	}`)

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Action != ActionReplace {
		t.Errorf("action = %q, want %q", changes[0].Action, ActionReplace)
	}
	if changes[0].Impact != ImpactCritical {
		t.Errorf("impact = %q, want %q", changes[0].Impact, ImpactCritical)
	}
}

func TestParsePlanRootModule(t *testing.T) {
	data := []byte(`{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "azurerm_resource_group.rg",
			"type": "azurerm_resource_group",
			"name": "rg",
			"provider_name": "registry.terraform.io/hashicorp/azurerm",
			"change": {
				"actions": ["create"],
				"before": null,
				"after": {"name": "rg-test"},
				"after_unknown": {"id": true},
				"before_sensitive": {},
				"after_sensitive": {}
			}
		}],
		"configuration": {"root_module": {}}
	}`)

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}

	if changes[0].ModulePath != "" {
		t.Errorf("module path = %q, want empty (root module)", changes[0].ModulePath)
	}
}

func TestParsePlanNestedModule(t *testing.T) {
	data := []byte(`{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "module.network.module.subnets.azurerm_subnet.this",
			"module_address": "module.network.module.subnets",
			"type": "azurerm_subnet",
			"name": "this",
			"provider_name": "registry.terraform.io/hashicorp/azurerm",
			"change": {
				"actions": ["update"],
				"before": {},
				"after": {},
				"after_unknown": {},
				"before_sensitive": {},
				"after_sensitive": {}
			}
		}],
		"configuration": {"root_module": {}}
	}`)

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}

	if changes[0].ModulePath != "module.network.module.subnets" {
		t.Errorf("module path = %q, want %q", changes[0].ModulePath, "module.network.module.subnets")
	}
}

func TestParsePlanForEachWithDots(t *testing.T) {
	data := []byte(`{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "module.dns.module.zone[\"privatelink.adf.azure.com\"].azurerm_private_dns_zone.main",
			"module_address": "module.dns.module.zone[\"privatelink.adf.azure.com\"]",
			"type": "azurerm_private_dns_zone",
			"name": "main",
			"provider_name": "registry.terraform.io/hashicorp/azurerm",
			"change": {
				"actions": ["create"],
				"before": null,
				"after": {"name": "privatelink.adf.azure.com"},
				"after_unknown": {"id": true},
				"before_sensitive": {},
				"after_sensitive": {}
			}
		}],
		"configuration": {"root_module": {}}
	}`)

	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}

	want := `module.dns.module.zone["privatelink.adf.azure.com"]`
	if changes[0].ModulePath != want {
		t.Errorf("module path = %q, want %q", changes[0].ModulePath, want)
	}
}

func TestParsePlanInvalidJSON(t *testing.T) {
	_, err := ParsePlan([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePlanMissingVersion(t *testing.T) {
	_, err := ParsePlan([]byte(`{"resource_changes":[]}`))
	if err == nil {
		t.Fatal("expected error for missing format_version")
	}
}

func TestParseModuleSources(t *testing.T) {
	// Monorepo-style mix: local paths dominate (most common in real repos),
	// plus one registry reference and one parent-dir path. Covers the source
	// string shapes ParseModuleSources must round-trip verbatim.
	data := []byte(`{
		"format_version": "1.2",
		"resource_changes": [],
		"configuration": {
			"root_module": {
				"module_calls": {
					"network":         {"source": "./modules/network"},
					"dns":             {"source": "./modules/dns"},
					"privatelink":     {"source": "./modules/privatelink"},
					"shared_security": {"source": "../shared-modules/security"},
					"registry_consul": {"source": "hashicorp/consul/aws"}
				}
			}
		}
	}`)

	sources := ParseModuleSources(data)

	checks := map[string]string{
		"network":         "./modules/network",
		"dns":             "./modules/dns",
		"privatelink":     "./modules/privatelink",
		"shared_security": "../shared-modules/security",
		"registry_consul": "hashicorp/consul/aws",
	}

	if len(sources) != len(checks) {
		t.Fatalf("sources length = %d, want %d", len(sources), len(checks))
	}

	for name, want := range checks {
		got, ok := sources[name]
		if !ok {
			t.Errorf("module %q not found in sources", name)
			continue
		}
		if got != want {
			t.Errorf("sources[%q] = %q, want %q", name, got, want)
		}
	}
}

func TestParseModuleSourcesEmpty(t *testing.T) {
	// Plan without configuration section
	data := []byte(`{"format_version": "1.2", "resource_changes": []}`)

	sources := ParseModuleSources(data)

	if len(sources) != 0 {
		t.Errorf("expected empty map, got %d entries", len(sources))
	}
}

func TestExtractModuleType(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		// Git URLs with //modules/X subpath
		{
			"git::https://github.com/example-org/terraform-azure-modules.git//modules/virtual_network?ref=virtual_network/v1.0.0",
			"virtual_network",
		},
		{
			"git::https://github.com/example-org/terraform-azure-modules.git//modules/bootstrap?ref=bootstrap/v1.0.0",
			"bootstrap",
		},
		{
			"git::https://github.com/org/repo.git//modules/zscaler_cloud_connector?ref=v1.0.0",
			"zscaler_cloud_connector",
		},
		// Local paths
		{"./modules/foo", "foo"},
		{"../modules/bar", "bar"},
		{"./modules/nested/deep", "deep"},
		// Registry format: namespace/name/provider
		{"hashicorp/consul/aws", "consul"},
		{"myorg/network/azurerm", "network"},
		// Empty string
		{"", ""},
		// Fallback: last path segment
		{"./some-module", "some-module"},
		{"https://example.com/modules/custom_mod", "custom_mod"},
	}

	for _, tt := range tests {
		got := ExtractModuleType(tt.source)
		if got != tt.want {
			t.Errorf("ExtractModuleType(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestMapTfAction(t *testing.T) {
	tests := []struct {
		actions tfjson.Actions
		want    Action
	}{
		{tfjson.Actions{tfjson.ActionCreate}, ActionCreate},
		{tfjson.Actions{tfjson.ActionUpdate}, ActionUpdate},
		{tfjson.Actions{tfjson.ActionDelete}, ActionDelete},
		{tfjson.Actions{tfjson.ActionRead}, ActionRead},
		{tfjson.Actions{tfjson.ActionNoop}, ActionNoOp},
		{tfjson.Actions{tfjson.ActionDelete, tfjson.ActionCreate}, ActionReplace},
		{tfjson.Actions{tfjson.ActionCreate, tfjson.ActionDelete}, ActionReplace},
		{tfjson.Actions{}, ActionNoOp},
		{nil, ActionNoOp},
	}

	for _, tt := range tests {
		got := mapTfAction(tt.actions)
		if got != tt.want {
			t.Errorf("mapTfAction(%v) = %q, want %q", tt.actions, got, tt.want)
		}
	}
}

func TestParsePlan_IsImport(t *testing.T) {
	data, err := os.ReadFile("../../testdata/import_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := ParsePlan(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}

	// Import-only (no-op action)
	if changes[0].Action != ActionNoOp {
		t.Errorf("changes[0] action = %q, want no-op", changes[0].Action)
	}
	if !changes[0].IsImport {
		t.Error("changes[0] IsImport = false, want true (rg import)")
	}

	// Import + update
	if changes[1].Action != ActionUpdate {
		t.Errorf("changes[1] action = %q, want update", changes[1].Action)
	}
	if !changes[1].IsImport {
		t.Error("changes[1] IsImport = false, want true (vnet import + update)")
	}

	// Plain create (not an import)
	if changes[2].Action != ActionCreate {
		t.Errorf("changes[2] action = %q, want create", changes[2].Action)
	}
	if changes[2].IsImport {
		t.Error("changes[2] IsImport = true, want false (plain create)")
	}
}
