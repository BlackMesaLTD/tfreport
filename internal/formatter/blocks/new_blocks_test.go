package blocks

import (
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
	"github.com/BlackMesaLTD/tfreport/internal/preserve"
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

// ----- summary_table columns arg -----

func TestSummaryTable_columnsSubset_module(t *testing.T) {
	r := syntheticReport("a",
		syntheticGroup("m1", core.ResourceChange{Address: "a", Action: core.ActionCreate}),
		syntheticGroup("m2", core.ResourceChange{Address: "b", Action: core.ActionUpdate}),
	)
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "module", "columns": "module,resources"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Module | Resources |") {
		t.Errorf("want only Module+Resources header, got:\n%s", out)
	}
	if strings.Contains(out, "Actions") {
		t.Errorf("Actions column should be dropped, got:\n%s", out)
	}
}

func TestSummaryTable_columnsSubset_action(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", Action: core.ActionCreate},
		core.ResourceChange{Address: "b", Action: core.ActionDelete},
	))
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "action", "columns": "action,count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Action | Count |") {
		t.Errorf("want Action+Count header only, got:\n%s", out)
	}
	if strings.Contains(out, "Impact") {
		t.Errorf("Impact column should be dropped, got:\n%s", out)
	}
}

func TestSummaryTable_columnsSubset_resourceType(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", ResourceType: "azurerm_subnet", Action: core.ActionUpdate},
	))
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "resource_type", "columns": "resource_type,count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Resource Type | Count |") {
		t.Errorf("want Resource Type+Count header only, got:\n%s", out)
	}
	if strings.Contains(out, "Actions") {
		t.Errorf("Actions column should be dropped, got:\n%s", out)
	}
}

func TestSummaryTable_unknownColumn(t *testing.T) {
	r := syntheticReport("a",
		syntheticGroup("m1", core.ResourceChange{Address: "a", Action: core.ActionCreate}),
	)
	_, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "module", "columns": "module,bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the offending column: %v", err)
	}
	if !strings.Contains(err.Error(), "valid:") {
		t.Errorf("error should list valid columns: %v", err)
	}
}

func TestSummaryTable_columnsExplicitDescriptionOnEmpty(t *testing.T) {
	// When callers explicitly request `description`, we show it even when
	// no row carries one (renders "—"). Default behavior still auto-hides.
	r := syntheticReport("a",
		syntheticGroup("m1",
			core.ResourceChange{Address: "a", ResourceType: "t", Action: core.ActionUpdate},
		),
	)
	r.ModuleSources = map[string]string{"m1": "registry.tf/foo/bar/azurerm"}
	// Default: no description in output (auto-hidden).
	out, err := SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "module_type"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Description") {
		t.Errorf("default should hide Description when empty, got:\n%s", out)
	}
	// Explicit request: show it.
	out, err = SummaryTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"group": "module_type", "columns": "module_type,description,resources"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Description") {
		t.Errorf("explicit request should include Description header, got:\n%s", out)
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

// TestKeyChanges_TreeAndFallbackProduceIdenticalOutput locks in parity
// between the tree-backed KeyChange collector and the legacy
// allReports-loop fallback. If the two ever diverge this test fails
// first, before any golden comparison.
func TestKeyChanges_TreeAndFallbackProduceIdenticalOutput(t *testing.T) {
	r := &core.Report{
		KeyChanges: []core.KeyChange{
			{Text: "🔴 critical one", Impact: core.ImpactCritical},
			{Text: "🔴 high one", Impact: core.ImpactHigh},
			{Text: "🟡 medium one", Impact: core.ImpactMedium},
			{Text: "🟢 low one", Impact: core.ImpactLow},
		},
	}

	fallbackCtx := &BlockContext{Target: "markdown", Report: r}
	treeCtx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	cases := []map[string]any{
		nil,
		{"impact": "critical,high"},
		{"max": 2},
		{"max": 1, "impact": "medium,low"},
	}
	for _, args := range cases {
		fallback, err := (KeyChanges{}).Render(fallbackCtx, args)
		if err != nil {
			t.Fatalf("fallback args=%v: %v", args, err)
		}
		tree, err := (KeyChanges{}).Render(treeCtx, args)
		if err != nil {
			t.Fatalf("tree args=%v: %v", args, err)
		}
		if tree != fallback {
			t.Errorf("tree/fallback divergence args=%v:\n--- tree ---\n%s\n--- fallback ---\n%s", args, tree, fallback)
		}
	}
}

// TestKeyChanges_MultiReportTreeOrder verifies that the tree-backed
// collector preserves the per-report, in-report-order sequence the
// allReports fallback emits. Matters because the tree visits reports
// in Reports.Children order, and each Report's KeyChange children
// come before any ModuleCall in buildReportNode.
func TestKeyChanges_MultiReportTreeOrder(t *testing.T) {
	ra := &core.Report{
		Label:      "sub-a",
		KeyChanges: []core.KeyChange{{Text: "A1", Impact: core.ImpactHigh}, {Text: "A2", Impact: core.ImpactLow}},
	}
	rb := &core.Report{
		Label:      "sub-b",
		KeyChanges: []core.KeyChange{{Text: "B1", Impact: core.ImpactCritical}},
	}
	ctx := &BlockContext{
		Target:  "markdown",
		Reports: []*core.Report{ra, rb},
		Tree:    core.BuildTree(ra, rb),
	}
	out, err := (KeyChanges{}).Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Order must be A1, A2, B1
	a1 := strings.Index(out, "A1")
	a2 := strings.Index(out, "A2")
	b1 := strings.Index(out, "B1")
	if a1 < 0 || a2 < 0 || b1 < 0 {
		t.Fatalf("missing bullet in output:\n%s", out)
	}
	if !(a1 < a2 && a2 < b1) {
		t.Errorf("multi-report order wrong (want A1<A2<B1, got %d/%d/%d)\n%s", a1, a2, b1, out)
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

// ----- submodule_group (Phase 5a) -----

func sgReport() *core.Report {
	return syntheticReport("a",
		core.ModuleGroup{
			Name: "app", Path: "module.vnet.module.app",
			Changes: []core.ResourceChange{
				{Address: "module.vnet.module.app.azurerm_subnet.a", ResourceType: "azurerm_subnet", ResourceName: "a", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
			},
			ActionCounts: map[core.Action]int{core.ActionUpdate: 1},
		},
		core.ModuleGroup{
			Name: "db", Path: "module.vnet.module.db",
			Changes: []core.ResourceChange{
				{Address: "module.vnet.module.db.azurerm_subnet.b", ResourceType: "azurerm_subnet", ResourceName: "b", Action: core.ActionDelete},
			},
			ActionCounts: map[core.Action]int{core.ActionDelete: 1},
		},
		core.ModuleGroup{
			Name: "other", Path: "module.nsg",
			Changes: []core.ResourceChange{
				{Address: "module.nsg.azurerm_nsg.x", ResourceType: "azurerm_nsg", ResourceName: "x", Action: core.ActionCreate},
			},
			ActionCounts: map[core.Action]int{core.ActionCreate: 1},
		},
	)
}

func TestSubmoduleGroup_requiresInstance(t *testing.T) {
	r := sgReport()
	_, err := SubmoduleGroup{}.Render(&BlockContext{Target: "github-step-summary", Report: r}, nil)
	if err == nil {
		t.Fatal("expected error when instance missing")
	}
}

func TestSubmoduleGroup_rendersNestedDetails(t *testing.T) {
	r := sgReport()
	out, err := SubmoduleGroup{}.Render(&BlockContext{Target: "github-step-summary", Report: r},
		map[string]any{"instance": "vnet"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<details><summary>app (1 update)</summary>") {
		t.Errorf("want app sub-group header, got:\n%s", out)
	}
	if !strings.Contains(out, "<details><summary>db (1 delete)</summary>") {
		t.Errorf("want db sub-group header, got:\n%s", out)
	}
	if strings.Contains(out, "nsg") {
		t.Errorf("other instance shouldn't appear, got:\n%s", out)
	}
}

func TestSubmoduleGroup_formatList(t *testing.T) {
	r := sgReport()
	out, err := SubmoduleGroup{}.Render(&BlockContext{Target: "github-step-summary", Report: r},
		map[string]any{"instance": "vnet", "format": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- ⚠️ `module.vnet.module.app.azurerm_subnet.a`") {
		t.Errorf("want list format with address bullets, got:\n%s", out)
	}
	if strings.Contains(out, "```diff") {
		t.Errorf("format=list should not emit diff fence, got:\n%s", out)
	}
}

func TestSubmoduleGroup_unknownFormat(t *testing.T) {
	r := sgReport()
	_, err := SubmoduleGroup{}.Render(&BlockContext{Target: "github-step-summary", Report: r},
		map[string]any{"instance": "vnet", "format": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestSubmoduleGroup_unknownInstanceReturnsEmpty(t *testing.T) {
	r := sgReport()
	out, err := SubmoduleGroup{}.Render(&BlockContext{Target: "github-step-summary", Report: r},
		map[string]any{"instance": "does-not-exist"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("expected empty for unknown instance, got:\n%s", out)
	}
}

// ----- attribute_diff (Phase 4c) -----

func adReport() *core.Report {
	return syntheticReport("a",
		syntheticGroup("m",
			core.ResourceChange{
				Address: "module.m.azurerm_subnet.app", ResourceType: "azurerm_subnet",
				ResourceName: "app", Action: core.ActionUpdate, Impact: core.ImpactMedium,
				ChangedAttributes: []core.ChangedAttribute{
					{Key: "tags", OldValue: map[string]string{"env": "dev"}, NewValue: map[string]string{"env": "prod"}, Description: "Resource tags"},
					{Key: "name", OldValue: "old", NewValue: "new"},
				},
			},
			core.ResourceChange{
				Address: "module.m.azurerm_vm.web", ResourceType: "azurerm_virtual_machine",
				ResourceName: "web", Action: core.ActionReplace,
				ChangedAttributes: []core.ChangedAttribute{
					{Key: "size", OldValue: "Small", NewValue: "Large"},
					{Key: "ip_address", Computed: true},
				},
			},
		),
	)
}

func TestAttributeDiff_tableDefault(t *testing.T) {
	r := adReport()
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Attribute | Before | After |") {
		t.Errorf("want default table header, got:\n%s", out)
	}
	if !strings.Contains(out, "`tags`") || !strings.Contains(out, "`size`") {
		t.Errorf("want rows for each attribute, got:\n%s", out)
	}
}

func TestAttributeDiff_computedValue(t *testing.T) {
	r := adReport()
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(known after apply)") {
		t.Errorf("computed attribute should render `(known after apply)`, got:\n%s", out)
	}
}

func TestAttributeDiff_listFormat(t *testing.T) {
	r := adReport()
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- **tags**:") {
		t.Errorf("list format should use `- **key**:` style, got:\n%s", out)
	}
	if !strings.Contains(out, "→") {
		t.Errorf("list format should use → arrow, got:\n%s", out)
	}
}

func TestAttributeDiff_inlineFormat(t *testing.T) {
	r := adReport()
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tags(") || !strings.Contains(out, "→") {
		t.Errorf("inline format should be compact key(old→new), got:\n%s", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("inline format should be one line, got:\n%s", out)
	}
}

func TestAttributeDiff_addressFilter(t *testing.T) {
	r := adReport()
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"addresses": "module.m.azurerm_subnet.app"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tags") || !strings.Contains(out, "name") {
		t.Errorf("want subnet's attrs, got:\n%s", out)
	}
	if strings.Contains(out, "size") || strings.Contains(out, "ip_address") {
		t.Errorf("vm's attrs should be filtered out, got:\n%s", out)
	}
}

func TestAttributeDiff_unknownFormat(t *testing.T) {
	r := adReport()
	_, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestAttributeDiff_columnsSubset(t *testing.T) {
	r := adReport()
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "key,address"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Attribute | Address |") {
		t.Errorf("want Attribute+Address only, got:\n%s", out)
	}
	if strings.Contains(out, "Before") || strings.Contains(out, "After") {
		t.Errorf("Before/After should be dropped, got:\n%s", out)
	}
}

func TestAttributeDiff_truncateLongValue(t *testing.T) {
	long := strings.Repeat("x", 200)
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{
			Address: "a", Action: core.ActionUpdate,
			ChangedAttributes: []core.ChangedAttribute{{Key: "k", OldValue: long, NewValue: "ok"}},
		},
	))
	out, err := AttributeDiff{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"truncate": 10})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("long value should truncate with …, got:\n%s", out)
	}
}

// ----- banner (Phase 4b) -----

func TestBanner_requiredText(t *testing.T) {
	_, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}}, nil)
	if err == nil {
		t.Fatal("expected error when text missing")
	}
}

func TestBanner_unknownStyle(t *testing.T) {
	_, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}},
		map[string]any{"text": "hi", "style": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown style")
	}
}

func TestBanner_noTriggersAlwaysFires(t *testing.T) {
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}},
		map[string]any{"text": "always"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "always") {
		t.Errorf("no-triggers should always fire, got:\n%s", out)
	}
	if !strings.HasPrefix(out, "⛔ ") {
		t.Errorf("default style=alert uses ⛔, got:\n%s", out)
	}
}

func TestBanner_ifImpactMatch(t *testing.T) {
	r := &core.Report{MaxImpact: core.ImpactHigh}
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"text": "risky", "if_impact": "critical,high"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "risky") {
		t.Errorf("if_impact should match high and fire, got:\n%s", out)
	}
}

func TestBanner_ifImpactNoMatch(t *testing.T) {
	r := &core.Report{MaxImpact: core.ImpactLow}
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"text": "risky", "if_impact": "critical,high"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("if_impact should not fire for low, got:\n%s", out)
	}
}

func TestBanner_ifActionGtFires(t *testing.T) {
	r := &core.Report{ActionCounts: map[core.Action]int{core.ActionDelete: 2}}
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"text": "deletes", "if_action_gt": "delete:0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "deletes") {
		t.Errorf("delete:0 should fire when delete=2, got:\n%s", out)
	}
}

func TestBanner_ifActionGtBelowThreshold(t *testing.T) {
	r := &core.Report{ActionCounts: map[core.Action]int{core.ActionDelete: 1}}
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"text": "x", "if_action_gt": "delete:5"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("delete:5 shouldn't fire for delete=1, got:\n%s", out)
	}
}

func TestBanner_ifActionGtMalformed(t *testing.T) {
	_, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}},
		map[string]any{"text": "x", "if_action_gt": "delete"})
	if err == nil {
		t.Fatal("expected error for malformed if_action_gt")
	}
}

func TestBanner_iconOverride(t *testing.T) {
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}},
		map[string]any{"text": "hi", "icon": "🚨"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "🚨 ") {
		t.Errorf("icon override should replace default, got:\n%s", out)
	}
}

func TestBanner_successStyle(t *testing.T) {
	// no triggers → always fires; confirm ✅ is the default icon for success.
	out, err := Banner{}.Render(&BlockContext{Target: "markdown", Report: &core.Report{}},
		map[string]any{"text": "clean", "style": "success"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "✅ ") {
		t.Errorf("success style should use ✅, got:\n%s", out)
	}
}

// ----- changed_attrs_display arg across blocks (create/delete field diffs) -----

func changedDisplayReport() *core.Report {
	return syntheticReport("a",
		core.ModuleGroup{
			Name: "m", Path: "module.m",
			Changes: []core.ResourceChange{
				{Address: "module.m.azurerm_subnet.new", ResourceType: "azurerm_subnet", ResourceName: "new", Action: core.ActionCreate,
					ChangedAttributes: []core.ChangedAttribute{{Key: "a"}, {Key: "b"}, {Key: "c"}}},
				{Address: "module.m.azurerm_subnet.gone", ResourceType: "azurerm_subnet", ResourceName: "gone", Action: core.ActionDelete,
					ChangedAttributes: []core.ChangedAttribute{{Key: "x"}, {Key: "y"}}},
				{Address: "module.m.azurerm_subnet.upd", ResourceType: "azurerm_subnet", ResourceName: "upd", Action: core.ActionUpdate,
					ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
			},
			ActionCounts: map[core.Action]int{core.ActionCreate: 1, core.ActionDelete: 1, core.ActionUpdate: 1},
		},
	)
}

func TestModuleDetails_changedAttrsDefault_createDelete(t *testing.T) {
	r := changedDisplayReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "create,delete,update"})
	if err != nil {
		t.Fatal(err)
	}
	// Create row: should show — instead of "a, b, c"
	if !strings.Contains(out, "| ✅ create | — |") {
		t.Errorf("create row should show — for Changed; got:\n%s", out)
	}
	// Delete row: should show — instead of "x, y"
	if !strings.Contains(out, "| ❗ delete | — |") {
		t.Errorf("delete row should show —; got:\n%s", out)
	}
	// Update row: keys list preserved.
	if !strings.Contains(out, "| ⚠️ update | tags |") {
		t.Errorf("update row should preserve keys list; got:\n%s", out)
	}
}

func TestModuleDetails_changedAttrsWordy(t *testing.T) {
	r := changedDisplayReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "create,delete,update", "changed_attrs_display": "wordy"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| ✅ create | new |") {
		t.Errorf("wordy mode: create → 'new', got:\n%s", out)
	}
	if !strings.Contains(out, "| ❗ delete | removed |") {
		t.Errorf("wordy mode: delete → 'removed', got:\n%s", out)
	}
	if !strings.Contains(out, "| ⚠️ update | tags |") {
		t.Errorf("wordy mode: update unchanged, got:\n%s", out)
	}
}

func TestModuleDetails_changedAttrsCount(t *testing.T) {
	r := changedDisplayReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "create,delete,update", "changed_attrs_display": "count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| ✅ create | 3 attrs |") {
		t.Errorf("count mode: create → '3 attrs', got:\n%s", out)
	}
	if !strings.Contains(out, "| ❗ delete | 2 attrs |") {
		t.Errorf("count mode: delete → '2 attrs', got:\n%s", out)
	}
}

func TestModuleDetails_changedAttrsList_legacy(t *testing.T) {
	r := changedDisplayReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "create,delete,update", "changed_attrs_display": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| ✅ create | a, b, c |") {
		t.Errorf("list mode: create → full keys list, got:\n%s", out)
	}
	if !strings.Contains(out, "| ❗ delete | x, y |") {
		t.Errorf("list mode: delete → full keys list, got:\n%s", out)
	}
}

func TestModuleDetails_changedAttrsUnknownMode(t *testing.T) {
	r := changedDisplayReport()
	_, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"changed_attrs_display": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown changed_attrs_display mode")
	}
}

func TestModuleDetails_changedAttrsContextDefault(t *testing.T) {
	r := changedDisplayReport()
	// Set ctx-level default; no arg → ctx wins.
	out, err := ModuleDetails{}.Render(&BlockContext{
		Target: "markdown", Report: r,
		Output: OutputOptions{ChangedAttrsDisplay: "wordy"},
	}, map[string]any{"actions": "create,delete,update"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| ✅ create | new |") {
		t.Errorf("ctx wordy default: create → 'new', got:\n%s", out)
	}
	// Arg overrides ctx.
	out, err = ModuleDetails{}.Render(&BlockContext{
		Target: "markdown", Report: r,
		Output: OutputOptions{ChangedAttrsDisplay: "wordy"},
	}, map[string]any{"actions": "create,delete,update", "changed_attrs_display": "count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| ✅ create | 3 attrs |") {
		t.Errorf("arg overrides ctx: create → '3 attrs', got:\n%s", out)
	}
}

func TestModuleDetails_changedAttrsDiffFormat(t *testing.T) {
	r := changedDisplayReport()
	// Default mode → create/delete should NOT carry [attrs] in the diff block
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "create,delete,update", "format": "diff"})
	if err != nil {
		t.Fatal(err)
	}
	// Create: "+ type: label" with no [attrs]
	if !strings.Contains(out, "+ azurerm_subnet: new\n") {
		t.Errorf("diff create should have no [attrs] suffix, got:\n%s", out)
	}
	// Delete: "- type: label" with no [attrs]
	if !strings.Contains(out, "- azurerm_subnet: gone\n") {
		t.Errorf("diff delete should have no [attrs] suffix, got:\n%s", out)
	}
	// Update: keeps [attrs]
	if !strings.Contains(out, "! azurerm_subnet: upd [tags]\n") {
		t.Errorf("diff update should keep [attrs], got:\n%s", out)
	}
}

func TestModuleDetails_changedAttrsDiffFormatListMode(t *testing.T) {
	r := changedDisplayReport()
	// List mode: create/delete regain their [attrs] suffix in diff
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "create,delete,update", "format": "diff", "changed_attrs_display": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+ azurerm_subnet: new [a, b, c]") {
		t.Errorf("list mode diff should include [attrs] on create, got:\n%s", out)
	}
}

func TestChangedResourcesTable_changedAttrsDefault(t *testing.T) {
	r := changedDisplayReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	// Create + delete rows should carry — for Changed column (dash default).
	if !strings.Contains(out, "| subnet | new |") || !strings.Contains(out, "— | 🟢") {
		// Hard to anchor exactly because the full row has multiple cells;
		// check the create label + dash appearing together.
		if !strings.Contains(out, "| new | — |") {
			t.Errorf("expected create row Changed=—, got:\n%s", out)
		}
	}
	// Update keeps keys list (backticked).
	if !strings.Contains(out, "`tags`") {
		t.Errorf("expected update row to keep `tags`, got:\n%s", out)
	}
}

func TestChangedResourcesTable_changedAttrsWordy(t *testing.T) {
	r := changedDisplayReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "all", "changed_attrs_display": "wordy"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| new |") {
		// Weak but sufficient — 'new' appears as the resource name too;
		// check both create row markers present.
		t.Errorf("wordy mode should emit 'new' for create Changed cell, got:\n%s", out)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("wordy mode should emit 'removed' for delete Changed cell, got:\n%s", out)
	}
}

func TestChangedResourcesTable_changedAttrsUnknownMode(t *testing.T) {
	r := changedDisplayReport()
	_, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"changed_attrs_display": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestModulesTable_changedAttrsUnionExcludesCreateDelete(t *testing.T) {
	r := changedDisplayReport()
	// Default mode: mixed group → union excludes create/delete's attrs.
	out, err := ModulesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "module,changed_attrs"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "`tags`") {
		t.Errorf("union should keep update's tags attr, got:\n%s", out)
	}
	for _, excluded := range []string{"`a`", "`b`", "`c`", "`x`", "`y`"} {
		if strings.Contains(out, excluded) {
			t.Errorf("union should exclude create/delete attrs (%s), got:\n%s", excluded, out)
		}
	}
}

func TestModulesTable_changedAttrsAllCreateGroupWordy(t *testing.T) {
	// A module group that's 100% create resources.
	r := syntheticReport("a", core.ModuleGroup{
		Name: "m", Path: "module.m",
		Changes: []core.ResourceChange{
			{Address: "a", Action: core.ActionCreate, ChangedAttributes: []core.ChangedAttribute{{Key: "a"}, {Key: "b"}}},
		},
		ActionCounts: map[core.Action]int{core.ActionCreate: 1},
	})
	out, err := ModulesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "module,changed_attrs", "changed_attrs_display": "wordy"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| new |") {
		t.Errorf("all-create group wordy → 'new', got:\n%s", out)
	}
}

func TestModulesTable_changedAttrsMixedCreateDeleteWordy(t *testing.T) {
	r := syntheticReport("a", core.ModuleGroup{
		Name: "m", Path: "module.m",
		Changes: []core.ResourceChange{
			{Address: "a", Action: core.ActionCreate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
			{Address: "b", Action: core.ActionDelete, ChangedAttributes: []core.ChangedAttribute{{Key: "z"}}},
		},
		ActionCounts: map[core.Action]int{core.ActionCreate: 1, core.ActionDelete: 1},
	})
	out, err := ModulesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "module,changed_attrs", "changed_attrs_display": "wordy"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "new+removed") {
		t.Errorf("mixed create+delete group wordy → 'new+removed', got:\n%s", out)
	}
}

func TestModulesTable_changedAttrsListModeLegacy(t *testing.T) {
	r := changedDisplayReport()
	out, err := ModulesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "module,changed_attrs", "changed_attrs_display": "list"})
	if err != nil {
		t.Fatal(err)
	}
	// Legacy: every key across the group including create/delete attrs.
	for _, want := range []string{"`a`", "`b`", "`c`", "`tags`", "`x`", "`y`"} {
		if !strings.Contains(out, want) {
			t.Errorf("list mode should include all attrs (missing %s), got:\n%s", want, out)
		}
	}
}

func TestModulesTable_changedAttrsUnknownMode(t *testing.T) {
	r := changedDisplayReport()
	_, err := ModulesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"changed_attrs_display": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

// ----- imports_list (Phase 4a) -----

func importsReport() *core.Report {
	return syntheticReport("a",
		core.ModuleGroup{
			Name: "vnet", Path: "module.vnet",
			Changes: []core.ResourceChange{
				{Address: "module.vnet.azurerm_subnet.app", ModulePath: "module.vnet", ResourceType: "azurerm_subnet", ResourceName: "app", Action: core.ActionNoOp, IsImport: true},
				{Address: "module.vnet.azurerm_subnet.db", ModulePath: "module.vnet", ResourceType: "azurerm_subnet", ResourceName: "db", Action: core.ActionNoOp, IsImport: true},
				{Address: "module.vnet.azurerm_route.x", ModulePath: "module.vnet", ResourceType: "azurerm_route", ResourceName: "x", Action: core.ActionCreate}, // not an import
			},
			ActionCounts: map[core.Action]int{core.ActionNoOp: 2, core.ActionCreate: 1},
		},
	)
}

func TestImportsList_listDefault(t *testing.T) {
	r := importsReport()
	out, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- `module.vnet.azurerm_subnet.app`") {
		t.Errorf("want bullet for import, got:\n%s", out)
	}
	if strings.Contains(out, "azurerm_route.x") {
		t.Errorf("non-import should be filtered out, got:\n%s", out)
	}
	if strings.Contains(out, "| Address |") {
		t.Errorf("default format=list should not emit a table, got:\n%s", out)
	}
}

func TestImportsList_tableFormat(t *testing.T) {
	r := importsReport()
	out, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "table"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Address | Resource Type | Module |") {
		t.Errorf("want default table header, got:\n%s", out)
	}
	if !strings.Contains(out, "azurerm_subnet.app") {
		t.Errorf("want subnet row, got:\n%s", out)
	}
}

func TestImportsList_tableColumnsSubset(t *testing.T) {
	r := importsReport()
	out, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "table", "columns": "address,module_path"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Address | Module Path |") {
		t.Errorf("want Address+Module Path only, got:\n%s", out)
	}
	if strings.Contains(out, "Resource Type") {
		t.Errorf("Resource Type should be dropped, got:\n%s", out)
	}
}

func TestImportsList_unknownFormat(t *testing.T) {
	r := importsReport()
	_, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestImportsList_unknownColumn(t *testing.T) {
	r := importsReport()
	_, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "table", "columns": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestImportsList_noImports(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", Action: core.ActionCreate},
	))
	out, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("want empty when no imports, got:\n%s", out)
	}
}

func TestImportsList_maxTruncates(t *testing.T) {
	r := importsReport()
	out, err := ImportsList{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"max": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1 more imports") {
		t.Errorf("want truncation marker, got:\n%s", out)
	}
}

// TestImportsList_TreeAndFallbackProduceIdenticalOutput locks in parity
// between the tree-backed row collector and the legacy ModuleGroups
// iteration. If the two paths ever diverge this test fails first,
// before any golden comparison.
func TestImportsList_TreeAndFallbackProduceIdenticalOutput(t *testing.T) {
	r := importsReport()

	fallbackCtx := &BlockContext{Target: "markdown", Report: r}
	treeCtx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	cases := []map[string]any{
		nil,
		{"format": "table"},
		{"format": "table", "columns": "address,module_path"},
		{"format": "list", "max": 1},
	}
	for _, args := range cases {
		fallback, err := (ImportsList{}).Render(fallbackCtx, args)
		if err != nil {
			t.Fatalf("fallback args=%v: %v", args, err)
		}
		tree, err := (ImportsList{}).Render(treeCtx, args)
		if err != nil {
			t.Fatalf("tree args=%v: %v", args, err)
		}
		if tree != fallback {
			t.Errorf("tree/fallback divergence args=%v:\n--- tree ---\n%s\n--- fallback ---\n%s", args, tree, fallback)
		}
	}
}

// ----- diff_groups / deploy_checklist / risk_histogram columns (Phase 3c) -----

func TestDiffGroups_columnsSubset(t *testing.T) {
	changes := []core.ResourceChange{
		{Address: "a", ResourceType: "t", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
		{Address: "b", ResourceType: "t", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
	}
	r := syntheticReport("a", syntheticGroup("m", changes...))
	out, err := DiffGroups{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "pattern,count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Pattern | Count |") {
		t.Errorf("want Pattern+Count only, got:\n%s", out)
	}
	if strings.Contains(out, "Sample") {
		t.Errorf("Sample column should be dropped, got:\n%s", out)
	}
}

func TestDiffGroups_WhereFiltersBeforeBucketing(t *testing.T) {
	// Four resources: two critical (impact-filtered), two medium. With
	// where=critical, only the critical two should bucket together — the
	// medium pair should be ignored entirely, not added to a sub-threshold
	// bucket.
	changes := []core.ResourceChange{
		{Address: "a", ModulePath: "", ResourceType: "t", Action: core.ActionReplace, Impact: core.ImpactCritical, ChangedAttributes: []core.ChangedAttribute{{Key: "x"}}},
		{Address: "b", ModulePath: "", ResourceType: "t", Action: core.ActionReplace, Impact: core.ImpactCritical, ChangedAttributes: []core.ChangedAttribute{{Key: "x"}}},
		{Address: "c", ModulePath: "", ResourceType: "t", Action: core.ActionUpdate, Impact: core.ImpactMedium, ChangedAttributes: []core.ChangedAttribute{{Key: "y"}}},
		{Address: "d", ModulePath: "", ResourceType: "t", Action: core.ActionUpdate, Impact: core.ImpactMedium, ChangedAttributes: []core.ChangedAttribute{{Key: "y"}}},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (DiffGroups{}).Render(ctx, map[string]any{
		"actions": "all",
		"where":   `self.impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "**Deduplicated changes:**") {
		t.Errorf("want collapsed section:\n%s", out)
	}
	// The critical bucket (count=2) should be present; the medium rows
	// should not appear at all (neither as a bucket nor an individual).
	if strings.Contains(out, "`c`") || strings.Contains(out, "`d`") {
		t.Errorf("medium resources should be filtered out:\n%s", out)
	}
}

func TestDiffGroups_unknownColumn(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
		core.ResourceChange{Address: "b", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
	))
	_, err := DiffGroups{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestDeployChecklist_defaults(t *testing.T) {
	out, err := DeployChecklist{}.Render(&BlockContext{
		Target: "github-pr-body",
		Reports: []*core.Report{
			{Label: "sub-a", MaxImpact: core.ImpactHigh, ActionCounts: map[core.Action]int{core.ActionCreate: 1}},
			{Label: "sub-b", MaxImpact: core.ImpactMedium, ActionCounts: map[core.Action]int{core.ActionUpdate: 2}},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- [ ] **sub-a** (high) — 1 create") {
		t.Errorf("want default format, got:\n%s", out)
	}
	if !strings.Contains(out, "- [ ] **sub-b** (medium) — 2 update") {
		t.Errorf("want second row, got:\n%s", out)
	}
}

func TestDeployChecklist_columnsSubset(t *testing.T) {
	out, err := DeployChecklist{}.Render(&BlockContext{
		Target: "github-pr-body",
		Reports: []*core.Report{
			{Label: "sub-a", MaxImpact: core.ImpactHigh, ActionCounts: map[core.Action]int{core.ActionCreate: 1}, KeyChanges: []core.KeyChange{{Text: "x"}}},
		},
	}, map[string]any{"columns": "subscription,key_changes_count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "**sub-a**") {
		t.Errorf("want subscription, got:\n%s", out)
	}
	if !strings.Contains(out, "1 key changes") {
		t.Errorf("want key_changes_count, got:\n%s", out)
	}
	if strings.Contains(out, "(high)") {
		t.Errorf("impact column should be dropped, got:\n%s", out)
	}
}

func TestDeployChecklist_unknownColumn(t *testing.T) {
	_, err := DeployChecklist{}.Render(&BlockContext{
		Target:  "github-pr-body",
		Reports: []*core.Report{{Label: "a"}},
	}, map[string]any{"columns": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestDeployChecklist_preserveFalseByDefault(t *testing.T) {
	out, err := DeployChecklist{}.Render(&BlockContext{
		Target:  "github-pr-body",
		Reports: []*core.Report{{Label: "sub-a", MaxImpact: core.ImpactHigh, ActionCounts: map[core.Action]int{core.ActionCreate: 1}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "tfreport:preserve-begin") {
		t.Errorf("preserve=false (default) should NOT emit markers, got:\n%s", out)
	}
}

func TestDeployChecklist_preserveRequiresPriorRegions(t *testing.T) {
	// preserve=true but PriorRegions nil → silently downgrade to no markers.
	out, err := DeployChecklist{}.Render(&BlockContext{
		Target:       "github-pr-body",
		Reports:      []*core.Report{{Label: "sub-a", MaxImpact: core.ImpactHigh, ActionCounts: map[core.Action]int{core.ActionCreate: 1}}},
		PriorRegions: nil,
	}, map[string]any{"preserve": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "tfreport:preserve-begin") {
		t.Errorf("preserve=true + nil PriorRegions should NOT emit markers, got:\n%s", out)
	}
}

func TestDeployChecklist_preserveEmitsMarkers(t *testing.T) {
	out, err := DeployChecklist{}.Render(&BlockContext{
		Target:       "github-pr-body",
		Reports:      []*core.Report{{Label: "sub-a", MaxImpact: core.ImpactHigh, ActionCounts: map[core.Action]int{core.ActionCreate: 1}}},
		PriorRegions: map[string]preserve.Region{},
	}, map[string]any{"preserve": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<!-- tfreport:preserve-begin id="deploy:sub-a" kind="checkbox" -->`) {
		t.Errorf("want begin marker with id=deploy:sub-a, got:\n%s", out)
	}
	if !strings.Contains(out, `<!-- tfreport:preserve-end id="deploy:sub-a" -->`) {
		t.Errorf("want end marker, got:\n%s", out)
	}
	if !strings.Contains(out, "[ ]") {
		t.Errorf("default body should be [ ], got:\n%s", out)
	}
	// GFM task-list detection needs `- [ ] ` contiguous at the start of a
	// line — any HTML comment between `-` and `]` breaks checkbox rendering.
	var taskLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[ ]") {
			taskLine = line
			break
		}
	}
	if !strings.HasPrefix(taskLine, "- [ ] ") {
		t.Errorf("task-list line must start with `- [ ] ` (no HTML comment between marker and bracket), got: %q", taskLine)
	}
}

func TestDeployChecklist_preserveSlugifiesLabel(t *testing.T) {
	out, err := DeployChecklist{}.Render(&BlockContext{
		Target:       "github-pr-body",
		Reports:      []*core.Report{{Label: "sub alpha!", MaxImpact: core.ImpactHigh, ActionCounts: map[core.Action]int{core.ActionCreate: 1}}},
		PriorRegions: map[string]preserve.Region{},
	}, map[string]any{"preserve": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `id="deploy:sub-alpha-"`) {
		t.Errorf("want slugified id deploy:sub-alpha-, got:\n%s", out)
	}
}

func TestRiskHistogram_columnsSubset(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", Action: core.ActionDelete, Impact: core.ImpactHigh},
	))
	out, err := RiskHistogram{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"style": "bar", "columns": "impact,count"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Impact | Count |") {
		t.Errorf("want Impact+Count only, got:\n%s", out)
	}
	if strings.Contains(out, "Bar") {
		t.Errorf("Bar column should be dropped, got:\n%s", out)
	}
}

func TestRiskHistogram_unknownColumn(t *testing.T) {
	r := syntheticReport("a", syntheticGroup("m",
		core.ResourceChange{Address: "a", Action: core.ActionDelete, Impact: core.ImpactHigh},
	))
	_, err := RiskHistogram{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"style": "bar", "columns": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}

// ----- module_details: format + columns + filters (Phase 3b) -----

func mdReport() *core.Report {
	return syntheticReport("a",
		core.ModuleGroup{
			Name: "vnet", Path: "module.vnet",
			Changes: []core.ResourceChange{
				{Address: "module.vnet.azurerm_subnet.app", ResourceType: "azurerm_subnet", ResourceName: "app", Action: core.ActionUpdate, Impact: core.ImpactMedium, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
				{Address: "module.vnet.azurerm_subnet.db", ResourceType: "azurerm_subnet", ResourceName: "db", Action: core.ActionDelete, Impact: core.ImpactHigh, ChangedAttributes: []core.ChangedAttribute{{Key: "name"}}},
				{Address: "module.vnet.azurerm_route.x", ResourceType: "azurerm_route", ResourceName: "x", Action: core.ActionCreate, Impact: core.ImpactLow},
			},
			ActionCounts: map[core.Action]int{core.ActionUpdate: 1, core.ActionDelete: 1, core.ActionCreate: 1},
		},
	)
}

func TestModuleDetails_formatTableDefault(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Resource | Action | Changed Attributes |") {
		t.Errorf("default table header missing, got:\n%s", out)
	}
}

func TestModuleDetails_formatDiff(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "diff"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "```diff") {
		t.Errorf("format=diff missing code fence, got:\n%s", out)
	}
	if strings.Contains(out, "| Resource |") {
		t.Errorf("format=diff should not render a table, got:\n%s", out)
	}
}

func TestModuleDetails_formatList(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- ") {
		t.Errorf("format=list should emit bullets, got:\n%s", out)
	}
	if strings.Contains(out, "```diff") {
		t.Errorf("format=list should not emit code fence, got:\n%s", out)
	}
}

func TestModuleDetails_perResourceAlias(t *testing.T) {
	r := mdReport()
	// Old-style `per_resource=true` must still produce a diff block.
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"per_resource": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "```diff") {
		t.Errorf("per_resource=true alias should render diff, got:\n%s", out)
	}
}

func TestModuleDetails_unknownFormat(t *testing.T) {
	r := mdReport()
	_, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"format": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestModuleDetails_columnsSubset(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "address,action"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Address | Action |") {
		t.Errorf("want Address+Action header, got:\n%s", out)
	}
	if strings.Contains(out, "Changed Attributes") {
		t.Errorf("Changed Attributes column should be dropped, got:\n%s", out)
	}
}

func TestModuleDetails_unknownColumn(t *testing.T) {
	r := mdReport()
	_, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestModuleDetails_filterActions(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"actions": "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "db") {
		t.Errorf("delete filter should keep db resource, got:\n%s", out)
	}
	if strings.Contains(out, "azurerm_route.x") {
		t.Errorf("delete filter should drop create, got:\n%s", out)
	}
}

func TestModuleDetails_filterImpact(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"impact": "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "db") {
		t.Errorf("impact=high should keep db, got:\n%s", out)
	}
	if strings.Contains(out, "azurerm_subnet.app") {
		t.Errorf("impact=high should drop medium, got:\n%s", out)
	}
}

func TestModuleDetails_maxTruncates(t *testing.T) {
	r := mdReport()
	out, err := ModuleDetails{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"max": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_... 2 more resources_") {
		t.Errorf("expected truncation marker, got:\n%s", out)
	}
}

// ----- changed_resources_table: columns + filters (Phase 3a) -----

func crtReport() *core.Report {
	// Two modules, six resources with varied action/impact/type/is_import.
	// rc.ModulePath populated (matches production output of the grouper)
	// so both tree and fallback collectors find the enclosing mg.
	return syntheticReport("prod",
		core.ModuleGroup{
			Name: "vnet", Path: "module.vnet",
			Changes: []core.ResourceChange{
				{Address: "module.vnet.azurerm_subnet.app", ModulePath: "module.vnet", ResourceType: "azurerm_subnet", ResourceName: "app", Action: core.ActionUpdate, Impact: core.ImpactMedium, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
				{Address: "module.vnet.azurerm_subnet.db", ModulePath: "module.vnet", ResourceType: "azurerm_subnet", ResourceName: "db", Action: core.ActionDelete, Impact: core.ImpactHigh, ChangedAttributes: []core.ChangedAttribute{{Key: "name"}}},
				{Address: "module.vnet.azurerm_vnet.main", ModulePath: "module.vnet", ResourceType: "azurerm_virtual_network", ResourceName: "main", Action: core.ActionUpdate, Impact: core.ImpactLow, IsImport: true, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
			},
			ActionCounts: map[core.Action]int{core.ActionUpdate: 2, core.ActionDelete: 1},
		},
		core.ModuleGroup{
			Name: "nsg", Path: "module.nsg",
			Changes: []core.ResourceChange{
				{Address: "module.nsg.azurerm_nsg.web", ModulePath: "module.nsg", ResourceType: "azurerm_network_security_group", ResourceName: "web", Action: core.ActionReplace, Impact: core.ImpactCritical, ChangedAttributes: []core.ChangedAttribute{{Key: "rules"}}},
				{Address: "module.nsg.azurerm_route.old", ModulePath: "module.nsg", ResourceType: "azurerm_route", ResourceName: "old", Action: core.ActionDelete, Impact: core.ImpactHigh, ChangedAttributes: []core.ChangedAttribute{{Key: "next_hop"}}},
			},
			ActionCounts: map[core.Action]int{core.ActionReplace: 1, core.ActionDelete: 1},
		},
	)
}

// TestBanner_ShowIfFiresOnImpact verifies the HCL predicate replaces
// the `if_impact` CSV trigger with the idiomatic terraform shape —
// `contains([...], self.max_impact)` — and binds `self` to the Report
// tree root.
// bannerReport fabricates a Report whose tree aggregates actually
// reflect the intended MaxImpact and ActionCounts. Tree aggregates
// are rolled up from Resource children, so forcing fields on the
// Report struct without matching ModuleGroups produces a tree whose
// `self.max_impact` is "" — not what users expect.
func bannerReport(label string, impact core.Impact, action core.Action) *core.Report {
	changes := []core.ResourceChange{
		{Address: label + "/a", ModulePath: "", ResourceType: "t", Action: action, Impact: impact},
	}
	return &core.Report{
		Label:        label,
		ModuleGroups: core.GroupByModule(changes),
		MaxImpact:    impact,
		ActionCounts: map[core.Action]int{action: 1},
	}
}

func TestBanner_ShowIfFiresOnImpact(t *testing.T) {
	r := bannerReport("prod", core.ImpactCritical, core.ActionReplace)
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (Banner{}).Render(ctx, map[string]any{
		"text":    "Critical changes present",
		"show_if": `contains(["critical", "high"], self.max_impact)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Critical changes present") {
		t.Errorf("expected banner to fire, got:\n%s", out)
	}
}

func TestBanner_ShowIfNegativeStaysQuiet(t *testing.T) {
	r := bannerReport("x", core.ImpactLow, core.ActionCreate)
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (Banner{}).Render(ctx, map[string]any{
		"text":    "Critical only",
		"show_if": `self.max_impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("banner should stay silent, got:\n%s", out)
	}
}

func TestBanner_ShowIfComposesOrWithCSVTriggers(t *testing.T) {
	// CSV trigger: if_action_gt "delete:0" — would NOT fire (no deletes).
	// show_if: matches critical — DOES fire.
	// Combined: should fire (OR semantics).
	r := bannerReport("prod", core.ImpactCritical, core.ActionReplace)
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (Banner{}).Render(ctx, map[string]any{
		"text":         "Fires on either trigger",
		"if_action_gt": "delete:0",
		"show_if":      `self.max_impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Fires on either trigger") {
		t.Errorf("either trigger should have fired:\n%s", out)
	}
}

func TestBanner_ShowIfActionCounts(t *testing.T) {
	// Natural terraform idiom — accessing action_counts through self.
	// Three deletes drive action_counts.delete = 3 in the tree rollup.
	changes := []core.ResourceChange{
		{Address: "a", ModulePath: "", ResourceType: "t", Action: core.ActionDelete, Impact: core.ImpactHigh},
		{Address: "b", ModulePath: "", ResourceType: "t", Action: core.ActionDelete, Impact: core.ImpactHigh},
		{Address: "c", ModulePath: "", ResourceType: "t", Action: core.ActionDelete, Impact: core.ImpactHigh},
	}
	r := &core.Report{
		ModuleGroups: core.GroupByModule(changes),
		MaxImpact:    core.ImpactHigh,
		ActionCounts: map[core.Action]int{core.ActionDelete: 3},
	}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (Banner{}).Render(ctx, map[string]any{
		"text":    "Deletions detected",
		"show_if": `self.action_counts.delete > 0`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Deletions detected") {
		t.Errorf("banner should fire on action_counts.delete > 0:\n%s", out)
	}
}

func TestBanner_ShowIfMultiReportOR(t *testing.T) {
	// Two reports. One low, one critical. Banner should fire because
	// at least one report matches.
	ra := bannerReport("a", core.ImpactLow, core.ActionCreate)
	rb := bannerReport("b", core.ImpactCritical, core.ActionReplace)
	ctx := &BlockContext{
		Target:  "markdown",
		Reports: []*core.Report{ra, rb},
		Tree:    core.BuildTree(ra, rb),
	}

	out, err := (Banner{}).Render(ctx, map[string]any{
		"text":    "Any-critical fires",
		"show_if": `self.max_impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Any-critical fires") {
		t.Errorf("banner should fire when any report matches:\n%s", out)
	}
}

func TestBanner_ShowIfBadSyntaxErrors(t *testing.T) {
	r := bannerReport("x", core.ImpactLow, core.ActionCreate)
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	_, err := (Banner{}).Render(ctx, map[string]any{
		"text":    "x",
		"show_if": "self.max_impact ==",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "banner") {
		t.Errorf("error should name the block: %v", err)
	}
}

func TestRiskHistogram_WhereFiltersTally(t *testing.T) {
	// Four resources; two are imports.
	changes := []core.ResourceChange{
		{Address: "a", ModulePath: "", ResourceType: "azurerm_subnet", Action: core.ActionUpdate, Impact: core.ImpactMedium},
		{Address: "b", ModulePath: "", ResourceType: "azurerm_subnet", Action: core.ActionUpdate, Impact: core.ImpactHigh, IsImport: true},
		{Address: "c", ModulePath: "", ResourceType: "azurerm_subnet", Action: core.ActionDelete, Impact: core.ImpactHigh},
		{Address: "d", ModulePath: "", ResourceType: "azurerm_nsg", Action: core.ActionCreate, Impact: core.ImpactLow, IsImport: true},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (RiskHistogram{}).Render(ctx, map[string]any{
		"style": "inline",
		"where": "self.is_import",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only 2 imports survive: one high (b), one low (d).
	// Inline format: "🔴 0 · 🔴 1 · 🟡 0 · 🟢 1"
	if !strings.Contains(out, "🔴 1") {
		t.Errorf("want single high from import filter: %q", out)
	}
	if !strings.Contains(out, "🟢 1") {
		t.Errorf("want single low from import filter: %q", out)
	}
	// Medium should be 0 (the non-import medium filtered out)
	if !strings.Contains(out, "🟡 0") {
		t.Errorf("medium should be zero after filter: %q", out)
	}
}

func TestRiskHistogram_WhereContainsResourceType(t *testing.T) {
	changes := []core.ResourceChange{
		{Address: "a", ModulePath: "", ResourceType: "azurerm_subnet", Action: core.ActionUpdate, Impact: core.ImpactMedium},
		{Address: "b", ModulePath: "", ResourceType: "azurerm_nsg", Action: core.ActionReplace, Impact: core.ImpactCritical},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (RiskHistogram{}).Render(ctx, map[string]any{
		"style": "inline",
		"where": `contains(["azurerm_subnet"], self.resource_type)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only the subnet survives: 1 medium, 0 critical
	if !strings.Contains(out, "🟡 1") {
		t.Errorf("want single medium: %q", out)
	}
	if !strings.Contains(out, "🔴 0") {
		t.Errorf("critical should be zero: %q", out)
	}
}

func TestRiskHistogram_NoWherePreservesDefault(t *testing.T) {
	// Golden byte-parity test: without where, the tally must match
	// the pre-where implementation exactly.
	changes := []core.ResourceChange{
		{Address: "a", ModulePath: "", Action: core.ActionUpdate, Impact: core.ImpactHigh},
		{Address: "b", ModulePath: "", Action: core.ActionCreate, Impact: core.ImpactLow},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	withTree := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}
	withoutTree := &BlockContext{Target: "markdown", Report: r}

	a, err := (RiskHistogram{}).Render(withTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := (RiskHistogram{}).Render(withoutTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("tree/no-tree parity broken:\n--- with-tree ---\n%s\n--- without-tree ---\n%s", a, b)
	}
}

// TestModuleDetails_WhereFiltersRows verifies the HCL predicate
// composes with the existing action+impact CSV filters and drops a
// whole module section when every row is filtered out.
func TestModuleDetails_WhereFiltersRows(t *testing.T) {
	changes := []core.ResourceChange{
		{Address: "module.a.azurerm_subnet.app", ModulePath: "module.a", ResourceType: "azurerm_subnet", ResourceName: "app", Action: core.ActionUpdate, Impact: core.ImpactMedium, IsImport: false, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
		{Address: "module.a.azurerm_subnet.db", ModulePath: "module.a", ResourceType: "azurerm_subnet", ResourceName: "db", Action: core.ActionDelete, Impact: core.ImpactHigh, IsImport: false, ChangedAttributes: []core.ChangedAttribute{{Key: "name"}}},
		{Address: "module.b.azurerm_route.old", ModulePath: "module.b", ResourceType: "azurerm_route", ResourceName: "old", Action: core.ActionUpdate, Impact: core.ImpactLow, IsImport: true, ChangedAttributes: []core.ChangedAttribute{{Key: "hop"}}},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	// Keep only imports. Module "a" has zero imports; "b" has one. Whole
	// "a" section should disappear.
	out, err := (ModuleDetails{}).Render(ctx, map[string]any{
		"where": "self.is_import",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "**a**") {
		t.Errorf("module a should be absent when every row filtered:\n%s", out)
	}
	if !strings.Contains(out, "**b**") {
		t.Errorf("module b should be present:\n%s", out)
	}
	if !strings.Contains(out, "azurerm_route.old") {
		t.Errorf("imported route should render:\n%s", out)
	}
}

func TestModuleDetails_WhereComposesWithActionsAndImpact(t *testing.T) {
	changes := []core.ResourceChange{
		{Address: "module.a.azurerm_subnet.app", ModulePath: "module.a", ResourceType: "azurerm_subnet", ResourceName: "app", Action: core.ActionUpdate, Impact: core.ImpactMedium, ChangedAttributes: []core.ChangedAttribute{{Key: "tags"}}},
		{Address: "module.a.azurerm_subnet.db", ModulePath: "module.a", ResourceType: "azurerm_subnet", ResourceName: "db", Action: core.ActionDelete, Impact: core.ImpactHigh, ChangedAttributes: []core.ChangedAttribute{{Key: "name"}}},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	// actions=update narrows to 1 (app). where=high narrows to 1 (db).
	// Intersection is empty — section should disappear.
	out, err := (ModuleDetails{}).Render(ctx, map[string]any{
		"actions": "update",
		"where":   `self.impact == "high"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("intersection is empty; want no output; got:\n%s", out)
	}
}

func TestAttributeDiff_WhereOnAttributeFields(t *testing.T) {
	changes := []core.ResourceChange{
		{
			Address: "azurerm_key_vault.main", ModulePath: "", ResourceType: "azurerm_key_vault", ResourceName: "main",
			Action: core.ActionUpdate, Impact: core.ImpactMedium,
			ChangedAttributes: []core.ChangedAttribute{
				{Key: "name"},
				{Key: "access_policy", Sensitive: true},
				{Key: "location", Computed: true},
			},
		},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	// Keep only sensitive attributes.
	out, err := (AttributeDiff{}).Render(ctx, map[string]any{
		"where": "self.sensitive",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "access_policy") {
		t.Errorf("want sensitive attr in output:\n%s", out)
	}
	for _, drop := range []string{"`name`", "`location`"} {
		if strings.Contains(out, drop) {
			t.Errorf("%q should be filtered out:\n%s", drop, out)
		}
	}
}

func TestAttributeDiff_WhereContainsKey(t *testing.T) {
	changes := []core.ResourceChange{
		{
			Address: "azurerm_subnet.app", ModulePath: "", ResourceType: "azurerm_subnet", ResourceName: "app",
			Action: core.ActionUpdate, Impact: core.ImpactMedium,
			ChangedAttributes: []core.ChangedAttribute{
				{Key: "name"}, {Key: "location"}, {Key: "tags"}, {Key: "address_prefixes"},
			},
		},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (AttributeDiff{}).Render(ctx, map[string]any{
		"where": `contains(["tags", "location"], self.key)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"`tags`", "`location`"} {
		if !strings.Contains(out, keep) {
			t.Errorf("want %q in output:\n%s", keep, out)
		}
	}
	for _, drop := range []string{"`name`", "`address_prefixes`"} {
		if strings.Contains(out, drop) {
			t.Errorf("%q should be filtered out:\n%s", drop, out)
		}
	}
}

func TestAttributeDiff_WhereBadSyntaxErrors(t *testing.T) {
	changes := []core.ResourceChange{
		{Address: "a", ModulePath: "", Action: core.ActionUpdate, ChangedAttributes: []core.ChangedAttribute{{Key: "k"}}},
	}
	r := &core.Report{ModuleGroups: core.GroupByModule(changes)}
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	_, err := (AttributeDiff{}).Render(ctx, map[string]any{"where": "self.sensitive =="})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "attribute_diff") {
		t.Errorf("error should name the block: %v", err)
	}
}

// TestChangedResourcesTable_WhereSimplePredicate verifies the HCL
// `where=` arg picks the expected rows on its own (no CSV filters).
// Uses the canonical "impact in [...]" idiom a terraform user would
// naturally write.
func TestChangedResourcesTable_WhereSimplePredicate(t *testing.T) {
	r := crtReport()
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (ChangedResourcesTable{}).Render(ctx, map[string]any{
		"actions": "all",
		"where":   `self.impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web") {
		t.Errorf("want nsg.web (critical) in output:\n%s", out)
	}
	for _, drop := range []string{"app", "db", "main", "old"} {
		if strings.Contains(out, drop) {
			t.Errorf("non-critical row %q should be filtered out:\n%s", drop, out)
		}
	}
}

// TestChangedResourcesTable_WhereContainsIdiom exercises the
// terraform-stdlib `contains()` function — the idiom terraform users
// reach for when they want "x is one of [a, b, c]" without writing an
// `||` chain. The function is registered in core.DefaultFunctions.
func TestChangedResourcesTable_WhereContainsIdiom(t *testing.T) {
	r := crtReport()
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	out, err := (ChangedResourcesTable{}).Render(ctx, map[string]any{
		"actions": "all",
		"where":   `contains(["critical", "high"], self.impact) && !self.is_import`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// db (high, not import), web (critical, not import), old (high, not import) — keep
	// main (low, import) — drop on both legs
	// app (medium) — drop on impact
	for _, keep := range []string{"db", "web", "old"} {
		if !strings.Contains(out, keep) {
			t.Errorf("expected %q in output:\n%s", keep, out)
		}
	}
	for _, drop := range []string{"app", "main"} {
		if strings.Contains(out, drop) {
			t.Errorf("%q should be filtered out:\n%s", drop, out)
		}
	}
}

// TestChangedResourcesTable_WhereComposesWithCSV confirms CSV filters
// and the HCL predicate AND together — a row must satisfy both.
func TestChangedResourcesTable_WhereComposesWithCSV(t *testing.T) {
	r := crtReport()
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	// CSV narrows to vnet module; where narrows to update action.
	// Intersection: vnet + update = app (update/medium) and main (update/low, import)
	out, err := (ChangedResourcesTable{}).Render(ctx, map[string]any{
		"actions": "all",
		"modules": "vnet",
		"where":   `self.action == "update"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"app", "main"} {
		if !strings.Contains(out, keep) {
			t.Errorf("expected vnet+update row %q:\n%s", keep, out)
		}
	}
	for _, drop := range []string{"db", "web", "old"} {
		if strings.Contains(out, drop) {
			t.Errorf("%q should be filtered out:\n%s", drop, out)
		}
	}
}

// TestChangedResourcesTable_WhereBadSyntaxErrors verifies that a
// malformed predicate fails at parse time with a helpful error anchored
// to the block name.
func TestChangedResourcesTable_WhereBadSyntaxErrors(t *testing.T) {
	r := crtReport()
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	_, err := (ChangedResourcesTable{}).Render(ctx, map[string]any{
		"where": `self.impact ==`,
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "changed_resources_table") {
		t.Errorf("error should name the block: %v", err)
	}
}

// TestChangedResourcesTable_WhereNonBoolErrors guards against a
// predicate that returns a non-bool value — caught at eval time.
func TestChangedResourcesTable_WhereNonBoolErrors(t *testing.T) {
	r := crtReport()
	ctx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	_, err := (ChangedResourcesTable{}).Render(ctx, map[string]any{
		"where": `self.address`, // returns a string
	})
	if err == nil {
		t.Fatal("expected non-bool eval error")
	}
}

// TestChangedResourcesTable_WhereWithoutTree confirms the block builds
// a tree on-demand when `where=` is set but the context didn't supply
// one. Users authoring from YAML won't know or care whether a tree is
// bound; the block just works.
func TestChangedResourcesTable_WhereWithoutTree(t *testing.T) {
	r := crtReport()
	ctx := &BlockContext{Target: "markdown", Report: r} // no Tree

	out, err := (ChangedResourcesTable{}).Render(ctx, map[string]any{
		"actions": "all",
		"where":   `self.impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web") {
		t.Errorf("want nsg.web row from tree-less context:\n%s", out)
	}
}

// TestChangedResourcesTable_TreeAndFallbackProduceIdenticalOutput locks
// in parity between the tree-backed collector and the legacy
// ModuleGroups iteration. Any divergence in filter ordering, row
// enumeration, or module-group lookup fails this test before any golden.
func TestChangedResourcesTable_TreeAndFallbackProduceIdenticalOutput(t *testing.T) {
	r := crtReport()
	fallbackCtx := &BlockContext{Target: "markdown", Report: r}
	treeCtx := &BlockContext{Target: "markdown", Report: r, Tree: core.BuildTree(r)}

	cases := []map[string]any{
		nil,
		{"actions": "all"},
		{"actions": "all", "impact": "critical,high"},
		{"actions": "all", "modules": "vnet"},
		{"actions": "all", "resource_types": "azurerm_subnet"},
		{"actions": "all", "is_import": "true"},
		{"actions": "all", "is_import": "false"},
		{"actions": "all", "max": 3},
		{"columns": "address,module,action", "actions": "all"},
	}
	for _, args := range cases {
		fallback, err := (ChangedResourcesTable{}).Render(fallbackCtx, args)
		if err != nil {
			t.Fatalf("fallback args=%v: %v", args, err)
		}
		tree, err := (ChangedResourcesTable{}).Render(treeCtx, args)
		if err != nil {
			t.Fatalf("tree args=%v: %v", args, err)
		}
		if tree != fallback {
			t.Errorf("tree/fallback divergence args=%v:\n--- tree ---\n%s\n--- fallback ---\n%s", args, tree, fallback)
		}
	}
}

func TestChangedResourcesTable_columnsSubset(t *testing.T) {
	r := crtReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "address,action", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Address | Action |") {
		t.Errorf("want only Address+Action header, got:\n%s", out)
	}
	if strings.Contains(out, "Resource |") || strings.Contains(out, "Impact |") {
		t.Errorf("other default columns should be excluded, got:\n%s", out)
	}
}

func TestChangedResourcesTable_unknownColumn(t *testing.T) {
	r := crtReport()
	_, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the offending column: %v", err)
	}
}

func TestChangedResourcesTable_filterImpact(t *testing.T) {
	r := crtReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"impact": "critical,high", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"db", "web", "old"} {
		if !strings.Contains(out, keep) {
			t.Errorf("impact filter dropped %q which should be kept:\n%s", keep, out)
		}
	}
	for _, drop := range []string{"app", "main"} {
		if strings.Contains(out, "| "+drop+" ") {
			t.Errorf("impact filter kept %q which should be dropped:\n%s", drop, out)
		}
	}
}

func TestChangedResourcesTable_filterModules(t *testing.T) {
	r := crtReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"modules": "nsg", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web") || !strings.Contains(out, "old") {
		t.Errorf("expected nsg's resources present, got:\n%s", out)
	}
	if strings.Contains(out, "app") || strings.Contains(out, " db ") {
		t.Errorf("expected vnet's resources dropped, got:\n%s", out)
	}
}

func TestChangedResourcesTable_filterResourceTypes(t *testing.T) {
	r := crtReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"resource_types": "azurerm_subnet", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "app") || !strings.Contains(out, "db") {
		t.Errorf("expected subnet rows kept, got:\n%s", out)
	}
	if strings.Contains(out, "web") || strings.Contains(out, "old") || strings.Contains(out, "main") {
		t.Errorf("expected non-subnet rows dropped, got:\n%s", out)
	}
}

func TestChangedResourcesTable_filterIsImport(t *testing.T) {
	r := crtReport()
	// Only imports.
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"is_import": "true", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "main") {
		t.Errorf("is_import=true should keep the imported row, got:\n%s", out)
	}
	if strings.Contains(out, "app") || strings.Contains(out, " db ") {
		t.Errorf("is_import=true should drop non-imports, got:\n%s", out)
	}
	// Only non-imports.
	out, err = ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"is_import": "false", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "main") {
		t.Errorf("is_import=false should drop the imported row, got:\n%s", out)
	}
}

func TestChangedResourcesTable_isImportColumn(t *testing.T) {
	r := crtReport()
	out, err := ChangedResourcesTable{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"columns": "name,is_import", "actions": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "♻️ yes") {
		t.Errorf("want ♻️ yes for imported resource, got:\n%s", out)
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

// ----- per_report -----

func makePerReportReport(label string) *core.Report {
	return &core.Report{
		Label:          label,
		TotalResources: 4,
		MaxImpact:      core.ImpactHigh,
		KeyChanges: []core.KeyChange{
			{Text: "✅ New private endpoint: pe-web", Impact: core.ImpactLow},
			{Text: "❗ Removing route: legacy-route", Impact: core.ImpactHigh},
		},
	}
}

func TestPerReport_missingReportArg(t *testing.T) {
	_, err := PerReport{}.Render(&BlockContext{Target: "markdown"}, nil)
	if err == nil {
		t.Fatal("expected error for missing 'report' arg")
	}
	if !strings.Contains(err.Error(), "report") {
		t.Errorf("error should mention 'report': %v", err)
	}
}

func TestPerReport_wrongReportType(t *testing.T) {
	_, err := PerReport{}.Render(&BlockContext{Target: "markdown"},
		map[string]any{"report": "not a pointer"})
	if err == nil {
		t.Fatal("expected error for non-pointer report")
	}
}

func TestPerReport_unknownShowItem(t *testing.T) {
	r := makePerReportReport("sub-a")
	_, err := PerReport{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"report": r, "show": "bogus_block"})
	if err == nil {
		t.Fatal("expected error for unknown show item")
	}
	if !strings.Contains(err.Error(), "bogus_block") {
		t.Errorf("error should name the bad item: %v", err)
	}
}

func TestPerReport_markdownH2WithBullets(t *testing.T) {
	r := makePerReportReport("sub-a")
	out, err := PerReport{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"report": r})
	if err != nil {
		t.Fatal(err)
	}
	// Markdown target: H2 header, subtitle, raw bullets (no "## Key Changes").
	if !strings.HasPrefix(out, "## sub-a") {
		t.Errorf("want H2 header prefix, got:\n%s", out)
	}
	if !strings.Contains(out, "**4 resources**") {
		t.Errorf("want resources subtitle, got:\n%s", out)
	}
	if strings.Contains(out, "## Key Changes") {
		t.Errorf("markdown per_report must NOT emit nested 'Key Changes' header, got:\n%s", out)
	}
	if !strings.Contains(out, "- ✅ New private endpoint: pe-web") {
		t.Errorf("want key changes bullets, got:\n%s", out)
	}
	if strings.Contains(out, "<details>") {
		t.Errorf("markdown must not be collapsed, got:\n%s", out)
	}
}

func TestPerReport_prBodyCollapsedWithKeyChangesHeader(t *testing.T) {
	r := makePerReportReport("sub-a")
	out, err := PerReport{}.Render(&BlockContext{Target: "github-pr-body", Report: r},
		map[string]any{"report": r})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "<details><summary>sub-a — 4 resources</summary>") {
		t.Errorf("want pr-body summary prefix, got:\n%s", out)
	}
	if !strings.Contains(out, "**Key changes:**") {
		t.Errorf("pr-body per_report should include 'Key changes:' header (from key_changes block), got:\n%s", out)
	}
	if !strings.HasSuffix(out, "</details>") {
		t.Errorf("want </details> suffix, got:\n%s", out)
	}
}

func TestPerReport_prCommentCollapsedBulletsOnly(t *testing.T) {
	r := makePerReportReport("sub-a")
	out, err := PerReport{}.Render(&BlockContext{Target: "github-pr-comment", Report: r},
		map[string]any{"report": r})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "<details><summary>sub-a — 4 resources</summary>") {
		t.Errorf("want pr-comment summary prefix, got:\n%s", out)
	}
	if strings.Contains(out, "**Key changes:**") {
		t.Errorf("pr-comment must NOT emit 'Key changes:' label, got:\n%s", out)
	}
}

func TestPerReport_stepSummaryIncludesImpact(t *testing.T) {
	r := makePerReportReport("sub-a")
	out, err := PerReport{}.Render(&BlockContext{Target: "github-step-summary", Report: r},
		map[string]any{"report": r})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sub-a — 4 resources, high impact") {
		t.Errorf("step-summary summary should include impact, got:\n%s", out)
	}
}

func TestPerReport_emptyLabelFallsBackToDefault(t *testing.T) {
	r := &core.Report{TotalResources: 0}
	out, err := PerReport{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"report": r})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## default") {
		t.Errorf("empty label should fall back to 'default', got:\n%s", out)
	}
}

func TestPerReport_collapseOverride(t *testing.T) {
	r := makePerReportReport("sub-a")
	// Force collapse on markdown.
	out, err := PerReport{}.Render(&BlockContext{Target: "markdown", Report: r},
		map[string]any{"report": r, "collapse": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<details>") {
		t.Errorf("collapse=true should force <details> even on markdown, got:\n%s", out)
	}
	// Force uncollapse on pr-body.
	out, err = PerReport{}.Render(&BlockContext{Target: "github-pr-body", Report: r},
		map[string]any{"report": r, "collapse": false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<details>") {
		t.Errorf("collapse=false should suppress <details> even on pr-body, got:\n%s", out)
	}
	if !strings.Contains(out, "## sub-a") {
		t.Errorf("collapse=false should fall back to H2 header, got:\n%s", out)
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
