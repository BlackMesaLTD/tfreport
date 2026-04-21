package blocks

import (
	"os"
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

func loadReport(t *testing.T, path string) *core.Report {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := core.GenerateReport(data, core.ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func fixtureCtx(t *testing.T, target string) *BlockContext {
	t.Helper()
	r := loadReport(t, "../../../testdata/small_plan.json")
	return &BlockContext{
		Target: target,
		Report: r,
		Output: OutputOptions{CodeFormat: "diff", MaxResourcesInSummary: 50},
	}
}

func TestTitle_allTargets(t *testing.T) {
	cases := []struct {
		target   string
		contains string
	}{
		{"markdown", "# Terraform Plan Report"},
		{"github-pr-body", "## Infrastructure Change Summary"},
		{"github-pr-comment", "### Terraform Plan"},
		{"github-step-summary", "Terraform Plan detected changes"},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			ctx := fixtureCtx(t, c.target)
			out, err := Title{}.Render(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, c.contains) {
				t.Errorf("target=%q: %q missing from %q", c.target, c.contains, out)
			}
		})
	}
}

func TestPlanCounts(t *testing.T) {
	ctx := fixtureCtx(t, "github-step-summary")
	out, err := PlanCounts{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Plan:") {
		t.Errorf("want 'Plan:' prefix, got %q", out)
	}
}

// TestPlanCounts_explicitReport confirms the `report` arg overrides the
// ctx-default report selection — essential for `range $r := .Reports` usage.
func TestPlanCounts_explicitReport(t *testing.T) {
	// Two differently-shaped reports in ctx.Reports. Without explicit arg,
	// currentReport() returns the first. With arg, caller's choice wins.
	r1 := &core.Report{
		TotalResources: 3,
		ActionCounts: map[core.Action]int{
			core.ActionCreate: 1, core.ActionUpdate: 2,
		},
	}
	r2 := &core.Report{
		TotalResources: 5,
		ActionCounts: map[core.Action]int{
			core.ActionDelete: 5,
		},
	}
	ctx := &BlockContext{Target: "markdown", Reports: []*core.Report{r1, r2}}

	// Default → first report.
	out, err := PlanCounts{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1 to add") || !strings.Contains(out, "2 to change") {
		t.Errorf("default: wanted r1 counts, got %q", out)
	}

	// Explicit r2 → second report.
	out, err = PlanCounts{}.Render(ctx, map[string]any{"report": r2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "5 to destroy") {
		t.Errorf("explicit r2: wanted r2 counts, got %q", out)
	}
	if strings.Contains(out, "to add") || strings.Contains(out, "to change") {
		t.Errorf("explicit r2 should not show r1 counts, got %q", out)
	}
}

func TestPlanCounts_includeImports(t *testing.T) {
	r := &core.Report{
		TotalResources: 2,
		ActionCounts:   map[core.Action]int{core.ActionUpdate: 2},
		ModuleGroups: []core.ModuleGroup{
			{Changes: []core.ResourceChange{
				{Address: "a", Action: core.ActionUpdate, IsImport: true},
				{Address: "b", Action: core.ActionUpdate, IsImport: true},
				{Address: "c", Action: core.ActionUpdate}, // not imported
			}},
		},
	}
	ctx := &BlockContext{Target: "markdown", Report: r}

	// Default: no imports suffix.
	out, err := PlanCounts{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "imported") {
		t.Errorf("default should omit imports, got %q", out)
	}

	// include_imports=true: "2 imported" appended.
	out, err = PlanCounts{}.Render(ctx, map[string]any{"include_imports": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2 imported") {
		t.Errorf("include_imports=true should append '2 imported', got %q", out)
	}
}

func TestPlanCounts_includeImportsWithZeroImports(t *testing.T) {
	// include_imports=true but no imports in report: suffix omitted.
	r := &core.Report{
		TotalResources: 1,
		ActionCounts:   map[core.Action]int{core.ActionCreate: 1},
		ModuleGroups: []core.ModuleGroup{
			{Changes: []core.ResourceChange{{Address: "a", Action: core.ActionCreate}}},
		},
	}
	ctx := &BlockContext{Target: "markdown", Report: r}
	out, err := PlanCounts{}.Render(ctx, map[string]any{"include_imports": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "imported") {
		t.Errorf("zero imports should omit suffix, got %q", out)
	}
}

func TestPlanCounts_wrongReportType(t *testing.T) {
	ctx := &BlockContext{Target: "markdown"}
	_, err := PlanCounts{}.Render(ctx, map[string]any{"report": "not a pointer"})
	if err == nil {
		t.Fatal("expected error for non-pointer report")
	}
}

func TestKeyChanges_empty(t *testing.T) {
	ctx := &BlockContext{Target: "markdown", Report: &core.Report{}}
	out, err := KeyChanges{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("want empty for report with no key changes, got %q", out)
	}
}

func TestKeyChanges_markdownHeader(t *testing.T) {
	ctx := fixtureCtx(t, "markdown")
	out, err := KeyChanges{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Sample plan has key changes; markdown target should prepend "## Key Changes".
	if len(ctx.Report.KeyChanges) > 0 && !strings.Contains(out, "## Key Changes") {
		t.Errorf("markdown target: want '## Key Changes' header, got %q", out)
	}
}

func TestSummaryTable_module(t *testing.T) {
	ctx := fixtureCtx(t, "markdown")
	out, err := SummaryTable{}.Render(ctx, map[string]any{"group": "module"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Module |") {
		t.Errorf("want Module table header, got %q", out)
	}
}

func TestTextPlan_noBlocks(t *testing.T) {
	ctx := fixtureCtx(t, "github-step-summary")
	// fixture has no text plan blocks populated
	out, err := TextPlan{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("want empty with no text plan blocks, got %q", out)
	}
}

func TestInstanceDetail_renders(t *testing.T) {
	ctx := fixtureCtx(t, "github-step-summary")
	out, err := InstanceDetail{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("expected non-empty instance detail")
	}
	if !strings.Contains(out, "<details>") {
		t.Errorf("want <details> in step-summary target, got %q", out)
	}
}

// TestAllBlocksHaveValidDoc asserts every registered block returns a BlockDoc
// whose Name matches the registry key. This catches drift between Block.Name()
// and BlockDoc.Name that would otherwise corrupt generated docs.
func TestAllBlocksHaveValidDoc(t *testing.T) {
	reg := Default()
	names := reg.Names()
	if len(names) == 0 {
		t.Fatal("default registry is empty — init() registrations missing?")
	}
	for _, name := range names {
		b, ok := reg.Get(name)
		if !ok {
			t.Fatalf("registry reported %q then failed to Get", name)
		}
		doc := b.Doc()
		if doc.Name == "" {
			t.Errorf("block %q: Doc().Name is empty", name)
		}
		if doc.Name != name {
			t.Errorf("block %q: Doc().Name=%q (mismatch)", name, doc.Name)
		}
		if doc.Summary == "" {
			t.Errorf("block %q: Doc().Summary is empty", name)
		}
	}
}

func TestFooter(t *testing.T) {
	ctx := fixtureCtx(t, "github-pr-body")
	out, err := Footer{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Generated by tfreport") {
		t.Errorf("want credit in footer, got %q", out)
	}
}
