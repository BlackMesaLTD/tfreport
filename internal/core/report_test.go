package core

import (
	"encoding/json"
	"os"
	"strings"
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

// --- Layer 1: Sensitive round-trip + backward-compat ---

func TestSensitiveRoundTrip(t *testing.T) {
	// Build a report with one Sensitive attribute via the full pipeline so
	// we exercise both Diff masking AND JSON serialization.
	data, err := os.ReadFile("../../testdata/sensitive_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := GenerateReport(data, ReportOptions{ChangedOnly: false})
	if err != nil {
		t.Fatal(err)
	}

	// Find the password attribute — must be Sensitive and value-masked.
	var found bool
	for _, mg := range report.ModuleGroups {
		for _, rc := range mg.Changes {
			for _, a := range rc.ChangedAttributes {
				if rc.Address == "mock_secret_holder.password_store" && a.Key == "password" {
					found = true
					if !a.Sensitive {
						t.Error("password attr should be Sensitive post-Diff")
					}
					if a.OldValue != SensitiveMask || a.NewValue != SensitiveMask {
						t.Errorf("password values should be masked: old=%v new=%v", a.OldValue, a.NewValue)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("password attribute not found in fixture report")
	}

	// Round-trip: marshal and unmarshal.
	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}

	// The canary must not be in the JSON — nowhere.
	for _, canary := range []string{"LEAK_CANARY_OLD", "LEAK_CANARY_NEW", "LEAK_CANARY_NESTED_OLD", "LEAK_CANARY_NESTED_NEW", "LEAK_CANARY_LIST_OLD", "LEAK_CANARY_LIST_NEW"} {
		if strings.Contains(string(marshaled), canary) {
			t.Errorf("canary %q leaked into JSON output", canary)
		}
	}

	// The "sensitive": true flag must be in the JSON for the password attr.
	if !strings.Contains(string(marshaled), `"sensitive": true`) {
		t.Error("JSON should contain 'sensitive: true' for at least one attribute")
	}

	// Unmarshal — Sensitive flag survives.
	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatal(err)
	}
	var restoredSensitive bool
	for _, mg := range restored.ModuleGroups {
		for _, rc := range mg.Changes {
			if rc.Address == "mock_secret_holder.password_store" {
				for _, a := range rc.ChangedAttributes {
					if a.Key == "password" && a.Sensitive {
						restoredSensitive = true
					}
				}
			}
		}
	}
	if !restoredSensitive {
		t.Error("restored report should have password.Sensitive=true")
	}
}

func TestSensitiveOmittedFromJSONWhenFalse(t *testing.T) {
	// Sensitive=false uses omitempty, so the key should be absent from JSON
	// for non-sensitive attributes (keeps the JSON tight).
	report := &Report{
		TotalResources: 1,
		ActionCounts:   map[Action]int{ActionCreate: 1},
		MaxImpact:      ImpactLow,
		ModuleGroups: []ModuleGroup{
			{
				Name:         "m",
				Path:         "module.m",
				ActionCounts: map[Action]int{ActionCreate: 1},
				Changes: []ResourceChange{
					{
						Address:           "a",
						Action:            ActionCreate,
						Impact:            ImpactLow,
						ChangedAttributes: []ChangedAttribute{{Key: "name", Sensitive: false}},
					},
				},
			},
		},
	}

	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(marshaled), `"sensitive":`) {
		t.Errorf("JSON should omit 'sensitive' key when false, got:\n%s", string(marshaled))
	}
}

// --- Layer 2: Preserved round-trip ---

func TestPreservedRoundTrip(t *testing.T) {
	// Run GenerateReport with PreserveAttributes on the sensitive fixture;
	// assert the resulting JSON contains non-sensitive preserved values
	// AND never contains LEAK_CANARY sentinels.
	data, err := os.ReadFile("../../testdata/sensitive_plan.json")
	if err != nil {
		t.Fatal(err)
	}

	var warnings []string
	report, err := GenerateReport(data, ReportOptions{
		ChangedOnly:        false,
		PreserveAttributes: []string{"id", "location", "password", "tags.env", "tags.secret_key"},
		Warn:               func(m string) { warnings = append(warnings, m) },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Find the password_store resource — `id` and `location` preserved, password absent.
	var ps *ResourceChange
	for _, mg := range report.ModuleGroups {
		for i := range mg.Changes {
			if mg.Changes[i].Address == "mock_secret_holder.password_store" {
				ps = &mg.Changes[i]
			}
		}
	}
	if ps == nil {
		t.Fatal("password_store resource missing from report")
	}
	if ps.Preserved["id"] != "store-1" {
		t.Errorf("id should be preserved: %v", ps.Preserved["id"])
	}
	if _, ok := ps.Preserved["password"]; ok {
		t.Error("sensitive password must NOT be in Preserved")
	}

	// Warning emitted for sensitive skip.
	foundWarn := false
	for _, w := range warnings {
		if strings.Contains(w, "password") && strings.Contains(w, "mock_secret_holder.password_store") {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("expected warning for sensitive password skip; warnings=%v", warnings)
	}

	// JSON round-trip: no LEAK_CANARY sentinels, Preserved survives.
	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"LEAK_CANARY_OLD", "LEAK_CANARY_NEW", "LEAK_CANARY_NESTED_OLD", "LEAK_CANARY_NESTED_NEW", "LEAK_CANARY_LIST_OLD", "LEAK_CANARY_LIST_NEW"} {
		if strings.Contains(string(marshaled), canary) {
			t.Errorf("canary %q leaked into Preserved JSON output", canary)
		}
	}
	if !strings.Contains(string(marshaled), `"preserved":`) {
		t.Error("JSON should contain 'preserved' key")
	}

	restored, err := UnmarshalReport(marshaled)
	if err != nil {
		t.Fatal(err)
	}
	var restoredPS *ResourceChange
	for _, mg := range restored.ModuleGroups {
		for i := range mg.Changes {
			if mg.Changes[i].Address == "mock_secret_holder.password_store" {
				restoredPS = &mg.Changes[i]
			}
		}
	}
	if restoredPS == nil || restoredPS.Preserved["id"] != "store-1" {
		t.Error("restored Preserved.id missing")
	}
}

func TestPreservedOmittedFromJSONWhenEmpty(t *testing.T) {
	// Empty Preserved → key absent via omitempty.
	report := &Report{
		TotalResources: 1,
		ActionCounts:   map[Action]int{ActionCreate: 1},
		MaxImpact:      ImpactLow,
		ModuleGroups: []ModuleGroup{
			{
				Name: "m", Path: "module.m",
				ActionCounts: map[Action]int{ActionCreate: 1},
				Changes: []ResourceChange{
					{Address: "a", Action: ActionCreate, Impact: ImpactLow},
				},
			},
		},
	}
	marshaled, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(marshaled), `"preserved":`) {
		t.Errorf("empty Preserved should be omitted, got:\n%s", marshaled)
	}
}

func TestLegacyReportBackwardCompat(t *testing.T) {
	// Load a legacy report JSON (no 'sensitive' / 'preserved' fields) and
	// confirm it parses cleanly — Sensitive defaults to false, Custom nil,
	// no errors.
	data, err := os.ReadFile("../../testdata/old_report.json")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := UnmarshalReport(data)
	if err != nil {
		t.Fatalf("legacy report parse failed: %v", err)
	}
	if restored.Label != "legacy-sub" {
		t.Errorf("label lost on legacy parse: %q", restored.Label)
	}
	if len(restored.ModuleGroups) != 1 {
		t.Fatalf("expected 1 module group, got %d", len(restored.ModuleGroups))
	}
	for _, a := range restored.ModuleGroups[0].Changes[0].ChangedAttributes {
		if a.Sensitive {
			t.Errorf("legacy attr should default Sensitive=false, got true for key %q", a.Key)
		}
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
