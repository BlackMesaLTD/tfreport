package cli

import (
	"os"
	"testing"
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
