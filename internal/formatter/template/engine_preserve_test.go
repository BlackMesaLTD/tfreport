package template

import (
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/formatter/blocks"
	"github.com/BlackMesaLTD/tfreport/internal/preserve"
)

func renderPreserve(t *testing.T, tmpl string, ctx *blocks.BlockContext) (string, error) {
	t.Helper()
	if ctx == nil {
		ctx = &blocks.BlockContext{Target: "markdown"}
	}
	return New(blocks.Default()).Render(tmpl, ctx)
}

func TestPreserve_checkboxDefault(t *testing.T) {
	out, err := renderPreserve(t, `{{ preserve "deploy:a" "checkbox" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Inline checkbox emits the full GFM task-list atom — begin on its own
	// line, `- [ ] ` contiguous on the next, end tucked after the trailing
	// space. The whole `\n- [ ] ` sits inside the region body.
	want := "<!-- tfreport:preserve-begin id=\"deploy:a\" kind=\"checkbox\" -->\n- [ ] <!-- tfreport:preserve-end id=\"deploy:a\" -->"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestPreserve_checkboxDefaultTicked(t *testing.T) {
	out, err := renderPreserve(t, `{{ preserve "deploy:a" "checkbox" "[x]" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n- [x] ") {
		t.Errorf("want \\n- [x]  in body, got %q", out)
	}
}

func TestPreserve_checkboxInvalidDefault(t *testing.T) {
	_, err := renderPreserve(t, `{{ preserve "deploy:a" "checkbox" "maybe" }}`, nil)
	if err == nil {
		t.Fatal("want error for invalid checkbox default")
	}
}

func TestPreserve_radio(t *testing.T) {
	out, err := renderPreserve(t, `{{ preserve "approver" "radio" (list "platform" "security" "hold") }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `options="platform,security,hold"`) {
		t.Errorf("want options attribute, got %q", out)
	}
	if strings.Count(out, "- [ ]") != 3 {
		t.Errorf("want 3 unticked rows, got %q", out)
	}
}

func TestPreserve_radioDefaultSelected(t *testing.T) {
	out, err := renderPreserve(t, `{{ preserve "approver" "radio" (list "a" "b" "c") "b" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- [x] b") {
		t.Errorf("want b ticked by default, got %q", out)
	}
}

func TestPreserve_radioMissingOptions(t *testing.T) {
	_, err := renderPreserve(t, `{{ preserve "approver" "radio" }}`, nil)
	if err == nil {
		t.Fatal("want error when radio options omitted")
	}
}

func TestPreserve_text(t *testing.T) {
	out, err := renderPreserve(t, `{{ preserve "note:x" "text" "placeholder" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<!-- tfreport:preserve-begin id="note:x" kind="text" -->placeholder<!-- tfreport:preserve-end id="note:x" -->`
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestPreserve_blockRejectedInline(t *testing.T) {
	_, err := renderPreserve(t, `{{ preserve "x" "block" }}`, nil)
	if err == nil {
		t.Fatal("want error when block kind used inline")
	}
	if !strings.Contains(err.Error(), "preserve_begin") {
		t.Errorf("error should point to preserve_begin, got %v", err)
	}
}

func TestPreserve_beginEnd(t *testing.T) {
	tmpl := `{{ preserve_begin "x" "block" }}body here
spanning lines
{{ preserve_end "x" }}`
	out, err := renderPreserve(t, tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<!-- tfreport:preserve-begin id="x" kind="block" -->`) {
		t.Errorf("missing begin marker in %q", out)
	}
	if !strings.Contains(out, `<!-- tfreport:preserve-end id="x" -->`) {
		t.Errorf("missing end marker in %q", out)
	}
	if !strings.Contains(out, "body here") {
		t.Errorf("body missing in %q", out)
	}
}

func TestPreserve_invalidID(t *testing.T) {
	_, err := renderPreserve(t, `{{ preserve "bad id" "checkbox" }}`, nil)
	if err == nil {
		t.Fatal("want error for invalid id")
	}
}

func TestPreserve_unknownKind(t *testing.T) {
	_, err := renderPreserve(t, `{{ preserve "x" "dropdown" }}`, nil)
	if err == nil {
		t.Fatal("want error for unknown kind")
	}
}

func TestPrior_returnsPriorBody(t *testing.T) {
	ctx := &blocks.BlockContext{
		Target: "markdown",
		PriorRegions: map[string]preserve.Region{
			"x": {ID: "x", Body: "Sarah's note"},
		},
	}
	out, err := renderPreserve(t, `{{ prior "x" }}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Sarah's note" {
		t.Errorf("got %q, want Sarah's note", out)
	}
}

func TestPrior_missingReturnsEmpty(t *testing.T) {
	out, err := renderPreserve(t, `{{ prior "nope" }}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("got %q, want empty string", out)
	}
}

func TestHasPrior(t *testing.T) {
	ctx := &blocks.BlockContext{
		Target: "markdown",
		PriorRegions: map[string]preserve.Region{
			"x": {ID: "x", Body: "v"},
		},
	}
	out, err := renderPreserve(t, `{{ if has_prior "x" }}yes{{ else }}no{{ end }}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out != "yes" {
		t.Errorf("got %q, want yes", out)
	}

	out, err = renderPreserve(t, `{{ if has_prior "nope" }}yes{{ else }}no{{ end }}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out != "no" {
		t.Errorf("got %q, want no", out)
	}
}
