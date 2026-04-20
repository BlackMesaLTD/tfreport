package formatter

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tfreport/tfreport/internal/core"
	"github.com/tfreport/tfreport/internal/formatter/blocks"
)

var update = flag.Bool("update", false, "update golden files")

// loadReport loads a plan fixture and returns the generated report.
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

// loadReportWithLabel loads a plan fixture and stamps the Label field.
func loadReportWithLabel(t *testing.T, path, label string) *core.Report {
	r := loadReport(t, path)
	r.Label = label
	return r
}

// newFormatter returns a TemplateFormatter pre-wired with a zero-config
// BlockContext so tests render identically to a CLI invocation without
// .tf-report.yml.
func newFormatter(t *testing.T, target string) *TemplateFormatter {
	t.Helper()
	f := NewTemplateFormatter(target)
	f.Context = &blocks.BlockContext{
		Target: target,
		Output: blocks.OutputOptions{CodeFormat: "diff", MaxResourcesInSummary: 50},
	}
	return f
}

// goldenCheck compares actual output against a golden file, updating the
// file instead when -update is passed.
func goldenCheck(t *testing.T, name, output string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(output), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file %s not found (run with -update to create): %v", goldenPath, err)
	}
	if output != string(expected) {
		t.Errorf("output differs from %s\nRun: go test -run %s -update\n\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, t.Name(), output, string(expected))
	}
}

// TestGoldenSingleReport renders each non-JSON target against small_plan.json
// and compares to a golden file. Run with -update to regenerate.
func TestGoldenSingleReport(t *testing.T) {
	cases := []struct {
		target string
		golden string
	}{
		{"markdown", "markdown.golden"},
		{"github-pr-body", "github_pr_body.golden"},
		{"github-pr-comment", "github_pr_comment.golden"},
		{"github-step-summary", "github_step_summary.golden"},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			r := loadReport(t, "../../testdata/small_plan.json")
			out, err := newFormatter(t, c.target).Format(r)
			if err != nil {
				t.Fatal(err)
			}
			goldenCheck(t, c.golden, out)
		})
	}
}

// TestGoldenMultiReport exercises multi-report aggregation for every target
// that's expected to be meaningful in multi mode (all four).
func TestGoldenMultiReport(t *testing.T) {
	cases := []struct {
		target string
		golden string
	}{
		{"markdown", "markdown_multi.golden"},
		{"github-pr-body", "github_pr_body_multi.golden"},
		{"github-pr-comment", "github_pr_comment_multi.golden"},
		{"github-step-summary", "github_step_summary_multi.golden"},
	}
	reportA := loadReportWithLabel(t, "../../testdata/small_plan.json", "sub-a")
	reportB := loadReportWithLabel(t, "../../testdata/medium_plan.json", "sub-b")
	reports := []*core.Report{reportA, reportB}

	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			out, err := newFormatter(t, c.target).FormatMulti(reports)
			if err != nil {
				t.Fatal(err)
			}
			goldenCheck(t, c.golden, out)
		})
	}
}

// TestJSONFormatter preserves the interchange-format test that it did before;
// json is unchanged.
func TestJSONFormatter(t *testing.T) {
	r := loadReport(t, "../../testdata/small_plan.json")
	f := &JSONFormatter{}
	output, err := f.Format(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output, "{") {
		t.Errorf("json must start with {, got %q", output[:min(40, len(output))])
	}
	goldenCheck(t, "json.golden", output)
}

// TestGet_validTargets ensures every ValidTargets() entry resolves to a
// formatter (TemplateFormatter or JSONFormatter).
func TestGet_validTargets(t *testing.T) {
	for _, target := range ValidTargets() {
		t.Run(target, func(t *testing.T) {
			f, err := Get(target)
			if err != nil {
				t.Fatal(err)
			}
			if f == nil {
				t.Error("expected non-nil formatter")
			}
		})
	}
}

func TestGet_unknownTarget(t *testing.T) {
	_, err := Get("unknown-target")
	if err == nil {
		t.Fatal("want error for unknown target")
	}
}

// TestMultiReportInterface ensures every non-json target implements
// MultiReportFormatter — the refactor upgraded markdown + step-summary to
// support multi-report aggregation.
func TestMultiReportInterface(t *testing.T) {
	for _, target := range []string{"markdown", "github-pr-body", "github-pr-comment", "github-step-summary"} {
		f, err := Get(target)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := f.(MultiReportFormatter); !ok {
			t.Errorf("%s does not implement MultiReportFormatter", target)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
