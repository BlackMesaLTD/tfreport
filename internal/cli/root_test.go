package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

func TestReadPlanJSONFromFile(t *testing.T) {
	flagPlanFile = "../../testdata/small_plan.json"
	defer func() { flagPlanFile = "" }()

	data, err := readPlanJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty plan JSON")
	}
}

func TestReadPlanJSONMissingFile(t *testing.T) {
	flagPlanFile = "nonexistent.json"
	defer func() { flagPlanFile = "" }()

	_, err := readPlanJSON()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseCustomFlags(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := parseCustomFlags(nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("empty input should return nil, got %v", got)
		}
	})

	t.Run("simple pair", func(t *testing.T) {
		got, err := parseCustomFlags([]string{"owner=platform-team"})
		if err != nil {
			t.Fatal(err)
		}
		if got["owner"] != "platform-team" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("value with equals", func(t *testing.T) {
		// Only the first = splits; extra = stay in the value.
		got, err := parseCustomFlags([]string{"formula=a=b=c"})
		if err != nil {
			t.Fatal(err)
		}
		if got["formula"] != "a=b=c" {
			t.Errorf("got %q", got["formula"])
		}
	})

	t.Run("repeated keys — last wins", func(t *testing.T) {
		got, err := parseCustomFlags([]string{"k=1", "k=2"})
		if err != nil {
			t.Fatal(err)
		}
		if got["k"] != "2" {
			t.Errorf("last-wins failed: %v", got)
		}
	})

	t.Run("missing equals", func(t *testing.T) {
		_, err := parseCustomFlags([]string{"bogus"})
		if err == nil {
			t.Fatal("expected error for missing equals")
		}
	})

	t.Run("empty key", func(t *testing.T) {
		_, err := parseCustomFlags([]string{"=value"})
		if err == nil {
			t.Fatal("expected error for empty key")
		}
	})

	t.Run("whitespace-trimmed key", func(t *testing.T) {
		got, err := parseCustomFlags([]string{"  owner  =value"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["owner"]; !ok {
			t.Errorf("expected trimmed key 'owner', got %v", got)
		}
	})
}

func TestApplyCustomFlags_mergesOntoEmpty(t *testing.T) {
	r := &core.Report{}
	if err := applyCustomFlags(r, []string{"sub_id=abc", "owner=me"}); err != nil {
		t.Fatal(err)
	}
	if r.Custom["sub_id"] != "abc" || r.Custom["owner"] != "me" {
		t.Errorf("merge failed: %v", r.Custom)
	}
}

func TestApplyCustomFlags_cliOverridesReportFileValue(t *testing.T) {
	// Simulates the --report-file x.json case where the loaded report
	// already carries Custom, and --custom on the CLI overrides specific keys.
	r := &core.Report{
		Custom: map[string]string{
			"sub_id": "from-file",
			"owner":  "original-owner",
		},
	}
	if err := applyCustomFlags(r, []string{"sub_id=from-cli"}); err != nil {
		t.Fatal(err)
	}
	// CLI wins on sub_id
	if r.Custom["sub_id"] != "from-cli" {
		t.Errorf("CLI should override file value: got %q", r.Custom["sub_id"])
	}
	// File value preserved for untouched keys
	if r.Custom["owner"] != "original-owner" {
		t.Errorf("untouched file value should survive: got %q", r.Custom["owner"])
	}
}

func TestApplyCustomFlags_noFlagsIsNoOp(t *testing.T) {
	r := &core.Report{Custom: map[string]string{"k": "v"}}
	if err := applyCustomFlags(r, nil); err != nil {
		t.Fatal(err)
	}
	if r.Custom["k"] != "v" || len(r.Custom) != 1 {
		t.Errorf("no-op failed: %v", r.Custom)
	}
}

func TestApplyCustomFlags_propagatesParseError(t *testing.T) {
	r := &core.Report{}
	err := applyCustomFlags(r, []string{"no-equals-sign"})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// TestExecute_rejectsCustomInMultiReportMode exercises the runtime guard in
// run() that prevents --custom from being silently ignored when multiple
// --report-file inputs are supplied (metadata must be set per-report at
// prepare time, not at aggregation time).
func TestExecute_rejectsCustomInMultiReportMode(t *testing.T) {
	// Create two valid report JSONs.
	writeTemp := func(t *testing.T, body string) string {
		t.Helper()
		f, err := os.CreateTemp("", "report-*.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(body); err != nil {
			t.Fatal(err)
		}
		f.Close()
		return f.Name()
	}
	reportJSON := `{"module_groups":[],"total_resources":0,"action_counts":{},"max_impact":""}`
	f1 := writeTemp(t, reportJSON)
	f2 := writeTemp(t, reportJSON)
	defer os.Remove(f1)
	defer os.Remove(f2)

	// Reset global flags between runs — rootCmd keeps state across Execute calls.
	flagReportFiles = nil
	flagCustom = nil
	defer func() {
		flagReportFiles = nil
		flagCustom = nil
	}()

	rootCmd.SetArgs([]string{
		"--report-file", f1,
		"--report-file", f2,
		"--custom", "sub_id=abc",
		"--quiet",
	})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for --custom in multi-report mode")
	}
	if !strings.Contains(err.Error(), "multi-report mode") {
		t.Errorf("error should mention multi-report mode: %v", err)
	}
}

func TestExecuteWithFile(t *testing.T) {
	// Create a temp file with valid JSON
	f, err := os.CreateTemp("", "plan-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(`{"format_version":"1.2","resource_changes":[]}`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	rootCmd.SetArgs([]string{"--plan-file", f.Name(), "--quiet"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteInvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "plan-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(`not json`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	rootCmd.SetArgs([]string{"--plan-file", f.Name(), "--quiet"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadPlanJSONFromStdin(t *testing.T) {
	// Create a pipe to simulate stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// Write test data to the pipe
	planData := `{"format_version":"1.2","resource_changes":[],"configuration":{"root_module":{}}}`
	go func() {
		w.WriteString(planData)
		w.Close()
	}()

	// Replace stdin temporarily
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	flagPlanFile = "" // ensure we read from stdin
	defer func() { flagPlanFile = "" }()

	data, err := readPlanJSON()
	if err != nil {
		t.Fatalf("unexpected error reading from stdin: %v", err)
	}

	if string(data) != planData {
		t.Errorf("got %q, want %q", string(data), planData)
	}
}

func TestExecuteWithAllTargets(t *testing.T) {
	targets := []string{"markdown", "github-pr-body", "github-pr-comment", "github-step-summary", "json"}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			rootCmd.SetArgs([]string{
				"--plan-file", "../../testdata/small_plan.json",
				"--target", target,
				"--quiet",
			})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("target %q: %v", target, err)
			}
		})
	}
}

func TestExecuteChangedOnly(t *testing.T) {
	rootCmd.SetArgs([]string{
		"--plan-file", "../../testdata/small_plan.json",
		"--changed-only",
		"--quiet",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
