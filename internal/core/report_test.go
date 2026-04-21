package core

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGenerateReport(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}

	report, err := GenerateReport(data, ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	// 3 module groups
	if len(report.ModuleGroups) != 3 {
		t.Errorf("module groups = %d, want 3", len(report.ModuleGroups))
	}

	// 4 total resources
	if report.TotalResources != 4 {
		t.Errorf("total resources = %d, want 4", report.TotalResources)
	}

	// Action counts
	if report.ActionCounts[ActionUpdate] != 2 {
		t.Errorf("update count = %d, want 2", report.ActionCounts[ActionUpdate])
	}
	if report.ActionCounts[ActionCreate] != 1 {
		t.Errorf("create count = %d, want 1", report.ActionCounts[ActionCreate])
	}
	if report.ActionCounts[ActionDelete] != 1 {
		t.Errorf("delete count = %d, want 1", report.ActionCounts[ActionDelete])
	}

	// Max impact should be high (delete)
	if report.MaxImpact != ImpactHigh {
		t.Errorf("max impact = %q, want %q", report.MaxImpact, ImpactHigh)
	}

	// Key changes should have plain-English sentences
	if len(report.KeyChanges) == 0 {
		t.Error("expected key changes, got none")
	}

	// Each module group should have changed attributes populated
	for _, mg := range report.ModuleGroups {
		for _, rc := range mg.Changes {
			if len(rc.ChangedAttributes) == 0 && rc.Action != ActionNoOp {
				t.Errorf("resource %s has no changed attributes", rc.Address)
			}
		}
	}
}

func TestGenerateReportWithModuleDescriptions(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}

	opts := ReportOptions{
		ModuleDescriptions: map[string]string{
			"virtual_network": "Managed VNet with subnets",
		},
	}

	report, err := GenerateReport(data, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Find the virtual_network group
	found := false
	for _, mg := range report.ModuleGroups {
		if mg.Name == "virtual_network" {
			found = true
			if mg.Description != "Managed VNet with subnets" {
				t.Errorf("description = %q, want %q", mg.Description, "Managed VNet with subnets")
			}
		}
	}
	if !found {
		t.Error("virtual_network group not found")
	}
}

func TestDeduplicateKeyChanges(t *testing.T) {
	input := []KeyChange{
		{Text: "✅ New NSG: main", Impact: ImpactLow},
		{Text: "✅ New public IP: main", Impact: ImpactLow},
		{Text: "✅ New NSG: main", Impact: ImpactLow},
		{Text: "✅ New NSG: main", Impact: ImpactLow},
		{Text: "✅ New subnet: app", Impact: ImpactLow},
		{Text: "✅ New public IP: main", Impact: ImpactHigh},
	}

	got := deduplicateKeyChanges(input)

	if len(got) != 3 {
		t.Fatalf("expected 3 deduplicated entries, got %d: %v", len(got), got)
	}
	if got[0].Text != "✅ New NSG: main (×3 modules)" {
		t.Errorf("got[0] = %q, want %q", got[0].Text, "✅ New NSG: main (×3 modules)")
	}
	if got[1].Text != "✅ New public IP: main (×2 modules)" {
		t.Errorf("got[1] = %q, want %q", got[1].Text, "✅ New public IP: main (×2 modules)")
	}
	// Max impact preserved across collision
	if got[1].Impact != ImpactHigh {
		t.Errorf("got[1].Impact = %q, want high (max across collision)", got[1].Impact)
	}
	if got[2].Text != "✅ New subnet: app" {
		t.Errorf("got[2] = %q, want %q", got[2].Text, "✅ New subnet: app")
	}
}

func TestDeduplicateKeyChangesEmpty(t *testing.T) {
	got := deduplicateKeyChanges(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestGenerateReportEmpty(t *testing.T) {
	data := []byte(`{"format_version":"1.2","resource_changes":[],"configuration":{"root_module":{}}}`)

	report, err := GenerateReport(data, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if report.TotalResources != 0 {
		t.Errorf("total = %d, want 0", report.TotalResources)
	}
	if len(report.ModuleGroups) != 0 {
		t.Errorf("groups = %d, want 0", len(report.ModuleGroups))
	}
}

func TestIsImportRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../testdata/import_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := GenerateReport(data, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatal(err)
	}

	// Two of the three resources are imports
	imports := 0
	for _, mg := range restored.ModuleGroups {
		for _, rc := range mg.Changes {
			if rc.IsImport {
				imports++
			}
		}
	}
	if imports != 2 {
		t.Errorf("round-tripped imports = %d, want 2", imports)
	}
}

func TestLabelRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := GenerateReport(data, ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	report.Label = "prod-eastus"

	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatalf("MarshalReport: %v", err)
	}

	// Verify label is in the JSON
	var raw map[string]any
	if err := json.Unmarshal(marshaled, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["label"] != "prod-eastus" {
		t.Errorf("JSON label = %v, want %q", raw["label"], "prod-eastus")
	}

	// Round-trip back
	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatalf("UnmarshalReport: %v", err)
	}
	if restored.Label != "prod-eastus" {
		t.Errorf("restored Label = %q, want %q", restored.Label, "prod-eastus")
	}
}

func TestCustomRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := GenerateReport(data, ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	report.Custom = map[string]string{
		"sub_id": "00000000-0000-0000-0000-000000000001",
		"owner":  "platform-team",
	}

	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatalf("MarshalReport: %v", err)
	}

	// Verify custom appears in the JSON.
	var raw map[string]any
	if err := json.Unmarshal(marshaled, &raw); err != nil {
		t.Fatal(err)
	}
	custom, ok := raw["custom"].(map[string]any)
	if !ok {
		t.Fatalf("JSON custom not a map: %v", raw["custom"])
	}
	if custom["sub_id"] != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("custom.sub_id = %v, want placeholder GUID", custom["sub_id"])
	}
	if custom["owner"] != "platform-team" {
		t.Errorf("custom.owner = %v, want platform-team", custom["owner"])
	}

	// Round-trip back.
	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatalf("UnmarshalReport: %v", err)
	}
	if len(restored.Custom) != 2 {
		t.Errorf("restored Custom len = %d, want 2", len(restored.Custom))
	}
	if restored.Custom["sub_id"] != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("restored Custom.sub_id = %q", restored.Custom["sub_id"])
	}
}

func TestCustomBackwardCompat(t *testing.T) {
	// JSON without a custom field should deserialize with nil Custom (not crash).
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := GenerateReport(data, ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	// Custom is nil at this point.
	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	// The omitempty tag should mean no "custom" key in the JSON.
	var raw map[string]any
	if err := json.Unmarshal(marshaled, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["custom"]; present {
		t.Errorf("custom key should be absent when Report.Custom is nil, got %v", raw["custom"])
	}
	// Round-trip without the field.
	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Custom != nil && len(restored.Custom) != 0 {
		t.Errorf("restored Custom should be nil/empty, got %v", restored.Custom)
	}
}

func TestLabelBackwardCompat(t *testing.T) {
	// JSON without a label field should deserialize with empty label
	data, err := os.ReadFile("../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := GenerateReport(data, ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	// Marshal without label
	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}

	// Verify no label field (omitempty)
	var raw map[string]any
	if err := json.Unmarshal(marshaled, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["label"]; exists {
		t.Error("expected no label field in JSON when empty")
	}

	// Unmarshal should still work
	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Label != "" {
		t.Errorf("restored Label = %q, want empty", restored.Label)
	}
}
