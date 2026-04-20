package blocks

import (
	"strings"
	"testing"

	"github.com/tfreport/tfreport/internal/core"
)

// syntheticReport builds a Report with the supplied module groups for use
// in block unit tests without touching plan-JSON fixtures.
func syntheticReport(label string, groups ...core.ModuleGroup) *core.Report {
	counts := map[core.Action]int{}
	total := 0
	var max core.Impact
	for _, mg := range groups {
		for a, c := range mg.ActionCounts {
			counts[a] += c
			total += c
		}
		if imp := core.MaxImpactForGroup(mg); core.ImpactSeverity(imp) > core.ImpactSeverity(max) {
			max = imp
		}
	}
	return &core.Report{
		Label:          label,
		ModuleGroups:   groups,
		TotalResources: total,
		ActionCounts:   counts,
		MaxImpact:      max,
	}
}

// syntheticGroup builds a ModuleGroup with the supplied changes, with
// ActionCounts aggregated automatically.
func syntheticGroup(name string, changes ...core.ResourceChange) core.ModuleGroup {
	counts := map[core.Action]int{}
	for _, c := range changes {
		counts[c.Action]++
	}
	return core.ModuleGroup{
		Name:         name,
		Path:         "module." + name,
		Changes:      changes,
		ActionCounts: counts,
	}
}

// ----- risk_histogram -----

func TestRiskHistogram_barStyle(t *testing.T) {
	r := syntheticReport("a",
		syntheticGroup("m",
			core.ResourceChange{Address: "a", ResourceType: "t", Action: core.ActionReplace, Impact: core.ImpactCritical},
			core.ResourceChange{Address: "b", ResourceType: "t", Action: core.ActionDelete, Impact: core.ImpactHigh},
			core.ResourceChange{Address: "c", ResourceType: "t", Action: core.ActionDelete, Impact: core.ImpactHigh},
			core.ResourceChange{Address: "d", ResourceType: "t", Action: core.ActionUpdate, Impact: core.ImpactMedium},
			core.ResourceChange{Address: "e", ResourceType: "t", Action: core.ActionCreate, Impact: core.ImpactLow},
		),
	)
	out, err := RiskHistogram{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| 🔴 critical | 1 | █ |") {
		t.Errorf("expected one critical bar, got:\n%s", out)
	}
	if !strings.Contains(out, "| 🔴 high | 2 | ██ |") {
		t.Errorf("expected two-wide high bar, got:\n%s", out)
	}
	if !strings.Contains(out, "| 🟢 low | 1 | █ |") {
		t.Errorf("expected low row, got:\n%s", out)
	}
}

func TestRiskHistogram_inlineStyle(t *testing.T) {
	r := syntheticReport("a",
		syntheticGroup("m",
			core.ResourceChange{Address: "a", Action: core.ActionDelete, Impact: core.ImpactHigh},
			core.ResourceChange{Address: "b", Action: core.ActionUpdate, Impact: core.ImpactMedium},
		),
	)
	out, err := RiskHistogram{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"style": "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "·") {
		t.Errorf("inline style should use · separator: %q", out)
	}
	if strings.Contains(out, "|") {
		t.Errorf("inline style should NOT contain table pipes: %q", out)
	}
}

func TestRiskHistogram_maxBarCap(t *testing.T) {
	// 50 high-impact resources; max_bar=10 → expect 10 blocks + "+"
	var changes []core.ResourceChange
	for i := 0; i < 50; i++ {
		changes = append(changes, core.ResourceChange{
			Address: "addr", Action: core.ActionDelete, Impact: core.ImpactHigh,
		})
	}
	r := syntheticReport("a", syntheticGroup("m", changes...))
	out, err := RiskHistogram{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"max_bar": "10"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, strings.Repeat("█", 10)+"+") {
		t.Errorf("expected 10-block bar + '+', got:\n%s", out)
	}
}

func TestRiskHistogram_unknownStyle(t *testing.T) {
	_, err := RiskHistogram{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}}, map[string]any{"style": "bogus"})
	if err == nil {
		t.Error("expected error for unknown style")
	}
}

// ----- diff_groups -----

func TestDiffGroups_collapsesIdentical(t *testing.T) {
	var changes []core.ResourceChange
	for i := 0; i < 3; i++ {
		changes = append(changes, core.ResourceChange{
			Address: "module.m.azurerm_subnet.sn" + string(rune('0'+i)),
			ResourceType: "azurerm_subnet",
			Action: core.ActionUpdate,
			ChangedAttributes: []core.ChangedAttribute{{Key: "tags", OldValue: map[string]string{"x": "1"}, NewValue: map[string]string{"x": "2"}}},
		})
	}
	// Unique resource with different attr set
	changes = append(changes, core.ResourceChange{
		Address:      "module.m.azurerm_route.legacy",
		ResourceType: "azurerm_route",
		Action:       core.ActionDelete,
		ChangedAttributes: []core.ChangedAttribute{{Key: "address_prefix"}},
	})
	r := syntheticReport("a", syntheticGroup("m", changes...))
	out, err := DiffGroups{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| ⚠️ update [tags] | 3 |") {
		t.Errorf("expected collapsed row with count 3, got:\n%s", out)
	}
	if !strings.Contains(out, "1 resource with unique changes") {
		t.Errorf("expected individual-changes footer, got:\n%s", out)
	}
	if !strings.Contains(out, "legacy") {
		t.Errorf("expected unique route row, got:\n%s", out)
	}
}

func TestDiffGroups_thresholdRespected(t *testing.T) {
	// Two identical resources but threshold=3 → not collapsed
	changes := []core.ResourceChange{
		{Address: "a", ResourceType: "t", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
		{Address: "b", ResourceType: "t", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
	}
	r := syntheticReport("a", syntheticGroup("m", changes...))
	out, err := DiffGroups{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"threshold": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Deduplicated changes") {
		t.Errorf("threshold=3 should not collapse 2 resources, got:\n%s", out)
	}
	if !strings.Contains(out, "2 resources with unique changes") {
		t.Errorf("expected 2 individual-changes footer, got:\n%s", out)
	}
}

// ----- fleet_homogeneity -----

func TestFleetHomogeneity_homogeneous(t *testing.T) {
	mk := func(label string) *core.Report {
		return &core.Report{
			Label: label,
			KeyChanges: []core.KeyChange{
				{Text: "✅ New subnet: foo", Impact: core.ImpactLow},
				{Text: "⚠️ Tags updated", Impact: core.ImpactMedium},
			},
		}
	}
	out, err := FleetHomogeneity{}.Render(&BlockContext{
		Target:  "github-pr-body",
		Reports: []*core.Report{mk("a"), mk("b"), mk("c")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Fleet uniform") {
		t.Errorf("expected 'Fleet uniform' banner, got:\n%s", out)
	}
	if !strings.Contains(out, "Applies to: a, b, c") {
		t.Errorf("expected applied-to list, got:\n%s", out)
	}
}

func TestFleetHomogeneity_divergent(t *testing.T) {
	a := &core.Report{Label: "a", KeyChanges: []core.KeyChange{{Text: "same"}}}
	b := &core.Report{Label: "b", KeyChanges: []core.KeyChange{{Text: "same"}}}
	outlier := &core.Report{Label: "c", KeyChanges: []core.KeyChange{{Text: "different"}}}
	out, err := FleetHomogeneity{}.Render(&BlockContext{
		Target:  "github-pr-body",
		Reports: []*core.Report{a, b, outlier},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Fleet divergent") {
		t.Errorf("expected 'Fleet divergent', got:\n%s", out)
	}
	if !strings.Contains(out, "1 of 3") {
		t.Errorf("expected outlier count 1 of 3, got:\n%s", out)
	}
	if !strings.Contains(out, "**c**") {
		t.Errorf("expected outlier c to be listed, got:\n%s", out)
	}
}

func TestFleetHomogeneity_singleReportReturnsEmpty(t *testing.T) {
	out, err := FleetHomogeneity{}.Render(&BlockContext{
		Target: "github-pr-body",
		Report: &core.Report{Label: "solo"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" && !strings.Contains(out, "single-report") {
		t.Errorf("single-report should either be empty or signal; got:\n%s", out)
	}
}

// ----- glossary -----

func TestGlossary_defaultIncludesActionsAndImpacts(t *testing.T) {
	out, err := Glossary{}.Render(&BlockContext{Target: "markdown"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "**Actions**") {
		t.Error("expected Actions section by default")
	}
	if !strings.Contains(out, "**Impact**") {
		t.Error("expected Impact section by default")
	}
	if strings.Contains(out, "**Imports**") {
		t.Error("imports section should NOT appear by default")
	}
}

func TestGlossary_includeImports(t *testing.T) {
	out, err := Glossary{}.Render(&BlockContext{Target: "markdown"}, map[string]any{"include": "imports"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "**Imports**") {
		t.Error("expected imports section when include=imports")
	}
	if strings.Contains(out, "**Actions**") {
		t.Error("actions should NOT appear when include=imports only")
	}
}

// ----- summary_table new groupings -----

func TestSummaryTable_groupAction(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", Action: core.ActionCreate},
		core.ResourceChange{Address: "b", Action: core.ActionCreate},
		core.ResourceChange{Address: "c", Action: core.ActionDelete},
	))
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"group": "action"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Action | Count | Impact |") {
		t.Errorf("expected action table header, got:\n%s", out)
	}
	if !strings.Contains(out, "| ✅ create | 2 |") {
		t.Errorf("expected 2 creates, got:\n%s", out)
	}
	if !strings.Contains(out, "| ❗ delete | 1 |") {
		t.Errorf("expected 1 delete, got:\n%s", out)
	}
}

func TestSummaryTable_groupResourceType(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", ResourceType: "azurerm_subnet", Action: core.ActionUpdate},
		core.ResourceChange{Address: "b", ResourceType: "azurerm_subnet", Action: core.ActionUpdate},
		core.ResourceChange{Address: "c", ResourceType: "azurerm_route", Action: core.ActionDelete},
	))
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"group": "resource_type"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Resource Type | Count | Actions |") {
		t.Errorf("expected resource_type table header, got:\n%s", out)
	}
	if !strings.Contains(out, "azurerm_subnet`) | 2 |") {
		t.Errorf("expected 2 subnets, got:\n%s", out)
	}
	if !strings.Contains(out, "azurerm_route`) | 1 |") {
		t.Errorf("expected 1 route, got:\n%s", out)
	}
}

func TestSummaryTable_resourceTypeImportOnlyLabel(t *testing.T) {
	// Regression test for GAP-004: no-op import-only resources must not
	// render with an empty Actions cell.
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", ResourceType: "azurerm_rg", Action: core.ActionNoOp, IsImport: true},
		core.ResourceChange{Address: "b", ResourceType: "azurerm_vnet", Action: core.ActionUpdate, IsImport: true},
		core.ResourceChange{Address: "c", ResourceType: "azurerm_subnet", Action: core.ActionCreate},
	))
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"group": "resource_type"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "♻️ 1 import-only") {
		t.Errorf("expected 'import-only' label for no-op import, got:\n%s", out)
	}
	if !strings.Contains(out, "♻️ 1 imported") {
		t.Errorf("expected 'imported' annotation on update+import row, got:\n%s", out)
	}
	// Cell must never be empty
	if strings.Contains(out, "|  |") {
		t.Errorf("empty Actions cell detected, got:\n%s", out)
	}
}

func TestSummaryTable_unknownGroup(t *testing.T) {
	_, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}}, map[string]any{"group": "bogus"})
	if err == nil {
		t.Error("expected error for unknown group")
	}
}

// ----- key_changes impact filter -----

func TestKeyChanges_impactFilter(t *testing.T) {
	r := &core.Report{
		KeyChanges: []core.KeyChange{
			{Text: "🔴 high one", Impact: core.ImpactHigh},
			{Text: "🟡 medium one", Impact: core.ImpactMedium},
			{Text: "🟢 low one", Impact: core.ImpactLow},
			{Text: "🔴 critical one", Impact: core.ImpactCritical},
		},
	}
	out, err := KeyChanges{}.Render(&BlockContext{Target: "github-pr-comment", Report: r}, map[string]any{"impact": "critical,high"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "high one") {
		t.Error("expected high sentence kept")
	}
	if !strings.Contains(out, "critical one") {
		t.Error("expected critical sentence kept")
	}
	if strings.Contains(out, "medium one") {
		t.Error("medium sentence should be filtered out")
	}
	if strings.Contains(out, "low one") {
		t.Error("low sentence should be filtered out")
	}
}

func TestKeyChanges_impactFilterEmptyResult(t *testing.T) {
	r := &core.Report{
		KeyChanges: []core.KeyChange{
			{Text: "🟢 low", Impact: core.ImpactLow},
		},
	}
	out, err := KeyChanges{}.Render(&BlockContext{Target: "markdown", Report: r}, map[string]any{"impact": "critical"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("expected empty when filter excludes every entry, got:\n%s", out)
	}
}

// ----- max arg: summary_table, changed_resources_table, instance_detail -----

func TestSummaryTable_maxModule(t *testing.T) {
	r := syntheticReport("a",
		syntheticGroup("m1", core.ResourceChange{Address: "a", Action: core.ActionCreate}),
		syntheticGroup("m2", core.ResourceChange{Address: "b", Action: core.ActionCreate}),
		syntheticGroup("m3", core.ResourceChange{Address: "c", Action: core.ActionCreate}),
		syntheticGroup("m4", core.ResourceChange{Address: "d", Action: core.ActionCreate}),
	)
	out, err := SummaryTable{}.Render(
		&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "module", "max": 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| m1 |") || !strings.Contains(out, "| m2 |") {
		t.Errorf("expected first two modules kept, got:\n%s", out)
	}
	if strings.Contains(out, "| m3 |") || strings.Contains(out, "| m4 |") {
		t.Errorf("expected m3/m4 truncated, got:\n%s", out)
	}
	if !strings.Contains(out, "_... 2 more modules_") {
		t.Errorf("expected truncation marker, got:\n%s", out)
	}
}

func TestSummaryTable_maxResourceType(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", ResourceType: "aaa_t", Action: core.ActionUpdate},
		core.ResourceChange{Address: "b", ResourceType: "bbb_t", Action: core.ActionUpdate},
		core.ResourceChange{Address: "c", ResourceType: "ccc_t", Action: core.ActionUpdate},
	))
	out, err := SummaryTable{}.Render(
		&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "resource_type", "max": 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_... 1 more resource types_") {
		t.Errorf("expected truncation marker, got:\n%s", out)
	}
}

func TestSummaryTable_maxUnlimited(t *testing.T) {
	// max=0 (default) keeps every row.
	r := syntheticReport("a",
		syntheticGroup("m1", core.ResourceChange{Address: "a", Action: core.ActionCreate}),
		syntheticGroup("m2", core.ResourceChange{Address: "b", Action: core.ActionCreate}),
		syntheticGroup("m3", core.ResourceChange{Address: "c", Action: core.ActionCreate}),
	)
	out, err := SummaryTable{}.Render(
		&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "module"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| m1 |", "| m2 |", "| m3 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in unlimited output, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "_... ") {
		t.Errorf("unexpected truncation marker with max=0, got:\n%s", out)
	}
}

func TestChangedResourcesTable_max(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", ResourceType: "t", ResourceName: "one", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
		core.ResourceChange{Address: "b", ResourceType: "t", ResourceName: "two", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
		core.ResourceChange{Address: "c", ResourceType: "t", ResourceName: "three", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
	))
	out, err := ChangedResourcesTable{}.Render(
		&BlockContext{Target: "github-step-summary", Report: r},
		map[string]any{"max": 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_... 1 more resources_") {
		t.Errorf("expected truncation marker, got:\n%s", out)
	}
	// Last row must have been dropped.
	if strings.Contains(out, "three") {
		t.Errorf("expected third row dropped, got:\n%s", out)
	}
}

func TestInstanceDetail_max(t *testing.T) {
	// Build a report with three distinct top-level module instances.
	r := syntheticReport("a",
		core.ModuleGroup{
			Name: "inst1", Path: "module.inst1",
			Changes:      []core.ResourceChange{{Address: "module.inst1.r.a", Action: core.ActionCreate, Impact: core.ImpactLow}},
			ActionCounts: map[core.Action]int{core.ActionCreate: 1},
		},
		core.ModuleGroup{
			Name: "inst2", Path: "module.inst2",
			Changes:      []core.ResourceChange{{Address: "module.inst2.r.b", Action: core.ActionCreate, Impact: core.ImpactLow}},
			ActionCounts: map[core.Action]int{core.ActionCreate: 1},
		},
		core.ModuleGroup{
			Name: "inst3", Path: "module.inst3",
			Changes:      []core.ResourceChange{{Address: "module.inst3.r.c", Action: core.ActionCreate, Impact: core.ImpactLow}},
			ActionCounts: map[core.Action]int{core.ActionCreate: 1},
		},
	)
	out, err := InstanceDetail{}.Render(
		&BlockContext{Target: "github-step-summary", Report: r, Output: OutputOptions{MaxResourcesInSummary: 50}},
		map[string]any{"max": 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_... 2 more instances_") {
		t.Errorf("expected truncation marker, got:\n%s", out)
	}
}
