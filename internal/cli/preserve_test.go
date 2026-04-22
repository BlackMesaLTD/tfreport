package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPreviousBody_empty(t *testing.T) {
	got, err := loadPreviousBody("", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("empty path should return nil map, got %v", got)
	}
}

func TestLoadPreviousBody_valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	content := `Some PR body
<!-- tfreport:preserve-begin id="deploy:a" kind="checkbox" -->[x]<!-- tfreport:preserve-end id="deploy:a" -->
more`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	regions, err := loadPreviousBody(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("want 1 region, got %d", len(regions))
	}
	if regions["deploy:a"].Body != "[x]" {
		t.Errorf("body = %q, want [x]", regions["deploy:a"].Body)
	}
}

func TestLoadPreviousBody_malformedLenient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	// Orphan end marker
	content := `plain <!-- tfreport:preserve-end id="a" -->`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadPreviousBody(path, false)
	if err != nil {
		t.Fatalf("lenient mode should not error: %v", err)
	}
	if got != nil {
		t.Errorf("lenient mode should return nil regions on parse error, got %v", got)
	}
}

func TestLoadPreviousBody_malformedStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	content := `plain <!-- tfreport:preserve-end id="a" -->`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadPreviousBody(path, true)
	if err == nil {
		t.Fatal("strict mode should error on malformed prior body")
	}
	if !strings.Contains(err.Error(), "previous body file") {
		t.Errorf("err should name file, got %v", err)
	}
}

func TestLoadPreviousBody_missingFile(t *testing.T) {
	_, err := loadPreviousBody("/nonexistent/nope.md", false)
	if err == nil {
		t.Fatal("want error on missing file")
	}
}

// TestExecute_stdinConflictRejected verifies the guard against consuming
// stdin for both --previous-body-file=- and the plan input path.
func TestExecute_stdinConflictRejected(t *testing.T) {
	defer func() {
		flagPreviousBodyFile = ""
		flagPlanFile = ""
		flagReportFiles = nil
		flagConfig = ""
		flagTarget = "markdown"
		flagQuiet = false
	}()

	rootCmd.SetArgs([]string{"--previous-body-file", "-", "--target", "markdown"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("want error when --previous-body-file=- is combined with stdin plan input")
	}
	if !strings.Contains(err.Error(), "conflicts with stdin plan input") {
		t.Errorf("want actionable error message; got: %v", err)
	}
}

// TestExecute_previousBodyRoundTrip exercises the full CLI path: render once
// with a custom template emitting a preserve region, save stdout as a "prior
// body", tick the checkbox by hand, re-render with --previous-body-file, and
// verify the tick is preserved in the new output.
func TestExecute_previousBodyRoundTrip(t *testing.T) {
	// Reset globals in cleanup — rootCmd preserves flag state across Execute calls
	// and other tests expect defaults.
	defer func() {
		flagPreviousBodyFile = ""
		flagPlanFile = ""
		flagConfig = ""
		flagTarget = "markdown"
		flagReportFiles = nil
		flagCustom = nil
		flagChangedOnly = false
		flagQuiet = false
	}()

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".tfreport.yml")
	configBody := `output:
  targets:
    markdown:
      template: |
        - {{ preserve "deploy:sub-a" "checkbox" }} sub-a
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	planPath := filepath.Join(dir, "plan.json")
	planBody := `{"format_version":"1.2","resource_changes":[]}`
	if err := os.WriteFile(planPath, []byte(planBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// First render — default [ ].
	out1 := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"--plan-file", planPath, "--config", configPath, "--target", "markdown", "--quiet"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out1, "[ ]") {
		t.Fatalf("first render should contain [ ]; got %q", out1)
	}
	if !strings.Contains(out1, `id="deploy:sub-a"`) {
		t.Fatalf("first render should contain the preserve marker; got %q", out1)
	}

	// Operator ticks by hand.
	ticked := strings.Replace(out1, "[ ]", "[x]", 1)
	priorPath := filepath.Join(dir, "prior.md")
	if err := os.WriteFile(priorPath, []byte(ticked), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second render — expect [x] preserved.
	out2 := captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"--plan-file", planPath,
			"--config", configPath,
			"--target", "markdown",
			"--previous-body-file", priorPath,
			"--quiet",
		})
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out2, "[x]") {
		t.Errorf("second render should carry [x] forward; got %q", out2)
	}
	if strings.Contains(out2, "[ ]") {
		t.Errorf("second render should no longer show [ ] for the preserved region; got %q", out2)
	}
}

// captureStdout pipes os.Stdout to a buffer for the duration of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 512)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return <-done
}
