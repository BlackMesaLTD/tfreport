package template

import (
	"os"
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
	"github.com/BlackMesaLTD/tfreport/internal/formatter/blocks"
)

func loadReport(t *testing.T) *core.Report {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/small_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := core.GenerateReport(data, core.ReportOptions{ChangedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestEngine_renderZeroArgProperties(t *testing.T) {
	r := loadReport(t)
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ .Title }}|{{ .PlanCounts }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: r,
		Output: blocks.OutputOptions{CodeFormat: "diff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# Terraform Plan Report|Plan:") {
		t.Errorf("got %q", out)
	}
}

func TestEngine_parameterizedBlock(t *testing.T) {
	r := loadReport(t)
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ summary_table "group" "module" }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: r,
		Output: blocks.OutputOptions{CodeFormat: "diff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Module |") {
		t.Errorf("want module table, got %q", out)
	}
}

func TestEngine_sprigAvailable(t *testing.T) {
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ upper "hello" }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: &core.Report{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "HELLO" {
		t.Errorf("sprig upper failed: %q", out)
	}
}

func TestEngine_rawDataEscape(t *testing.T) {
	r := loadReport(t)
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ .Target }}-{{ .Report.TotalResources }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "markdown-") {
		t.Errorf("raw data access failed: %q", out)
	}
}

func TestEngine_parseError(t *testing.T) {
	engine := New(blocks.Default())
	_, err := engine.Render(`{{ .Broken`, &blocks.BlockContext{Target: "markdown", Report: &core.Report{}})
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("want parse error, got %v", err)
	}
}

func TestEngine_badBlockArgs(t *testing.T) {
	engine := New(blocks.Default())
	_, err := engine.Render(`{{ summary_table "group" }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: &core.Report{},
	})
	if err == nil {
		t.Error("want error for odd-count args")
	}
}

func TestEngine_actionCount(t *testing.T) {
	r := &core.Report{
		ActionCounts: map[core.Action]int{
			core.ActionCreate: 3,
			core.ActionDelete: 1,
		},
	}
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ action_count "create" }}|{{ action_count "delete" }}|{{ action_count "unknown" }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "3|1|0" {
		t.Errorf("action_count = %q, want 3|1|0", out)
	}
}

func TestEngine_importCount(t *testing.T) {
	r := &core.Report{
		ModuleGroups: []core.ModuleGroup{
			{Changes: []core.ResourceChange{
				{Address: "a", IsImport: true},
				{Address: "b", IsImport: false},
				{Address: "c", IsImport: true},
			}},
		},
	}
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ import_count }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "2" {
		t.Errorf("import_count = %q, want 2", out)
	}
}

func TestEngine_importCountAcrossReports(t *testing.T) {
	mk := func(n int) *core.Report {
		changes := make([]core.ResourceChange, n)
		for i := range changes {
			changes[i] = core.ResourceChange{Address: "a", IsImport: true}
		}
		return &core.Report{ModuleGroups: []core.ModuleGroup{{Changes: changes}}}
	}
	engine := New(blocks.Default())
	out, err := engine.Render(`{{ import_count }}`, &blocks.BlockContext{
		Target:  "github-pr-body",
		Reports: []*core.Report{mk(2), mk(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "5" {
		t.Errorf("import_count across reports = %q, want 5", out)
	}
}

func TestEngine_sample(t *testing.T) {
	engine := New(blocks.Default())
	out, err := engine.Render(
		`{{ range $s := sample 2 (list "a" "b" "c" "d") }}{{ $s }}-{{ end }}`,
		&blocks.BlockContext{Target: "markdown", Report: &core.Report{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a-b-" {
		t.Errorf("sample 2 = %q, want a-b-", out)
	}
}

func TestEngine_sampleBiggerThanSlice(t *testing.T) {
	engine := New(blocks.Default())
	out, err := engine.Render(
		`{{ range $s := sample 10 (list "a" "b") }}{{ $s }}-{{ end }}`,
		&blocks.BlockContext{Target: "markdown", Report: &core.Report{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a-b-" {
		t.Errorf("sample 10 of 2-slice = %q, want a-b-", out)
	}
}

func TestEngine_impactIs(t *testing.T) {
	engine := New(blocks.Default())
	out, err := engine.Render(
		`{{ if impact_is "critical" "critical" }}YES{{ else }}NO{{ end }}|{{ if impact_is "high" "low" }}YES{{ else }}NO{{ end }}`,
		&blocks.BlockContext{Target: "markdown", Report: &core.Report{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out != "YES|NO" {
		t.Errorf("impact_is = %q, want YES|NO", out)
	}
}

func TestEngine_impactIsWithTypedImpact(t *testing.T) {
	// Regression test: impact_is should compare core.Impact (typed string)
	// and plain string cleanly, without requiring (printf "%s" $imp).
	r := &core.Report{MaxImpact: core.ImpactCritical}
	engine := New(blocks.Default())
	out, err := engine.Render(
		`{{ if impact_is "critical" .Report.MaxImpact }}match{{ end }}`,
		&blocks.BlockContext{Target: "markdown", Report: r},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "match" {
		t.Errorf("impact_is against typed Impact = %q, want match", out)
	}
}

func TestEngine_actionIs(t *testing.T) {
	r := &core.Report{ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionDelete}}}}}
	engine := New(blocks.Default())
	out, err := engine.Render(
		`{{ range $mg := .Report.ModuleGroups }}{{ range $rc := $mg.Changes }}{{ if action_is "delete" $rc.Action }}del{{ end }}{{ end }}{{ end }}`,
		&blocks.BlockContext{Target: "markdown", Report: r},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "del" {
		t.Errorf("action_is = %q, want del", out)
	}
}

func TestEngine_includeBound(t *testing.T) {
	dir := t.TempDir()
	snippet := dir + "/snippet.md"
	if err := os.WriteFile(snippet, []byte("INCLUDED"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := New(blocks.Default()).WithIncludeFunc(MakeIncludeFunc(dir))
	out, err := engine.Render(`{{ include "snippet.md" }}`, &blocks.BlockContext{
		Target: "markdown",
		Report: &core.Report{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "INCLUDED") {
		t.Errorf("include failed: %q", out)
	}
}
