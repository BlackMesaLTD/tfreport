package preserve

import (
	"strings"
	"testing"
)

func TestReconcile_noPrior(t *testing.T) {
	rendered := `<!-- tfreport:preserve-begin id="a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="a" -->`
	got, err := Reconcile(rendered, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != rendered {
		t.Errorf("nil prior should pass through unchanged")
	}
}

func TestReconcile_mergesCheckbox(t *testing.T) {
	prior := map[string]Region{
		"deploy:a": {ID: "deploy:a", Kind: "checkbox", Body: "[x]"},
	}
	rendered := `- <!-- tfreport:preserve-begin id="deploy:a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="deploy:a" --> rest`
	got, err := Reconcile(rendered, prior)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `[x]<!-- tfreport:preserve-end id="deploy:a"`) {
		t.Errorf("want reconciled [x], got %q", got)
	}
	if !strings.Contains(got, " rest") {
		t.Errorf("want surrounding text preserved, got %q", got)
	}
}

func TestReconcile_dropVanishedPrior(t *testing.T) {
	prior := map[string]Region{
		"removed:a": {ID: "removed:a", Kind: "checkbox", Body: "[x]"},
	}
	rendered := `<!-- tfreport:preserve-begin id="deploy:b" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="deploy:b" -->`
	got, err := Reconcile(rendered, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got != rendered {
		t.Errorf("vanished prior ids should be silently dropped; got %q", got)
	}
}

func TestReconcile_newRegionKeepsDefault(t *testing.T) {
	prior := map[string]Region{
		"deploy:a": {ID: "deploy:a", Kind: "checkbox", Body: "[x]"},
	}
	rendered := strings.Join([]string{
		`<!-- tfreport:preserve-begin id="deploy:a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="deploy:a" -->`,
		`<!-- tfreport:preserve-begin id="deploy:b" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="deploy:b" -->`,
	}, "\n")
	got, err := Reconcile(rendered, prior)
	if err != nil {
		t.Fatal(err)
	}
	// deploy:a → [x], deploy:b → [ ] (new region; default)
	if !strings.Contains(got, `id="deploy:a" kind="checkbox" -->[x]<!--`) {
		t.Errorf("deploy:a should be preserved as [x]; got %q", got)
	}
	if !strings.Contains(got, `id="deploy:b" kind="checkbox" -->[ ]<!--`) {
		t.Errorf("deploy:b should stay as default [ ]; got %q", got)
	}
}

func TestReconcile_idempotent(t *testing.T) {
	prior := map[string]Region{
		"deploy:a": {ID: "deploy:a", Kind: "checkbox", Body: "[x]"},
	}
	rendered := `<!-- tfreport:preserve-begin id="deploy:a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="deploy:a" -->`
	once, err := Reconcile(rendered, prior)
	if err != nil {
		t.Fatal(err)
	}
	// After reconcile, the output's region now has [x] body. Running again
	// with the same prior should produce identical output.
	twice, err := Reconcile(once, prior)
	if err != nil {
		t.Fatal(err)
	}
	if once != twice {
		t.Errorf("reconcile not idempotent:\nonce: %q\ntwice: %q", once, twice)
	}
}

func TestReconcile_multipleRegions(t *testing.T) {
	prior := map[string]Region{
		"a": {ID: "a", Kind: "checkbox", Body: "[x]"},
		"c": {ID: "c", Kind: "text", Body: "Reviewed by Sarah"},
	}
	rendered := strings.Join([]string{
		`<!-- tfreport:preserve-begin id="a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="a" -->`,
		`<!-- tfreport:preserve-begin id="b" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="b" -->`,
		`<!-- tfreport:preserve-begin id="c" kind="text" -->placeholder<!-- tfreport:preserve-end id="c" -->`,
	}, "\n")
	got, err := Reconcile(rendered, prior)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `id="a" kind="checkbox" -->[x]<!--`) {
		t.Errorf("a not preserved as [x]: %q", got)
	}
	if !strings.Contains(got, `id="b" kind="checkbox" -->[ ]<!--`) {
		t.Errorf("b should stay default [ ]: %q", got)
	}
	if !strings.Contains(got, `id="c" kind="text" -->Reviewed by Sarah<!--`) {
		t.Errorf("c text not preserved: %q", got)
	}
}

func TestReconcile_radioMergeFullFlow(t *testing.T) {
	prior := map[string]Region{
		"approver": {
			ID:   "approver",
			Kind: "radio",
			Body: "\n- [ ] platform\n- [x] security\n- [ ] hold\n",
		},
	}
	rendered := `<!-- tfreport:preserve-begin id="approver" kind="radio" options="platform,security,hold" -->
- [ ] platform
- [ ] security
- [ ] hold
<!-- tfreport:preserve-end id="approver" -->`
	got, err := Reconcile(rendered, prior)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[x] security") {
		t.Errorf("radio selection 'security' not preserved: %q", got)
	}
	if strings.Count(got, "[x]") != 1 {
		t.Errorf("radio should have exactly one [x], got %q", got)
	}
}

func TestReconcile_malformedRendered(t *testing.T) {
	prior := map[string]Region{
		"a": {ID: "a", Kind: "checkbox", Body: "[x]"},
	}
	rendered := `<!-- tfreport:preserve-begin id="a" kind="checkbox" -->unterminated`
	_, err := Reconcile(rendered, prior)
	if err == nil {
		t.Fatal("want error on malformed rendered input")
	}
}
