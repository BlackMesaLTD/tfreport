package blocks

import (
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// keysFormatter is a test fixture for the formatter callback: returns a
// comma-joined key list (matching the shape most callers pass in).
func keysFormatter(attrs []core.ChangedAttribute) string {
	parts := make([]string, len(attrs))
	for i, a := range attrs {
		parts[i] = a.Key
	}
	return strings.Join(parts, ", ")
}

func TestRenderChangedCell_updateAlwaysUsesFormatter(t *testing.T) {
	attrs := []core.ChangedAttribute{{Key: "tags"}, {Key: "name"}}
	// All modes must render the keys-list for update.
	for _, mode := range []string{"", "dash", "wordy", "count", "list"} {
		got := renderChangedCell(core.ActionUpdate, attrs, mode, keysFormatter)
		if got != "tags, name" {
			t.Errorf("mode=%q update: want keys-list, got %q", mode, got)
		}
	}
}

func TestRenderChangedCell_replaceAlwaysUsesFormatter(t *testing.T) {
	attrs := []core.ChangedAttribute{{Key: "size"}}
	for _, mode := range []string{"", "dash", "wordy", "count", "list"} {
		got := renderChangedCell(core.ActionReplace, attrs, mode, keysFormatter)
		if got != "size" {
			t.Errorf("mode=%q replace: want keys-list, got %q", mode, got)
		}
	}
}

func TestRenderChangedCell_createModes(t *testing.T) {
	attrs := []core.ChangedAttribute{{Key: "a"}, {Key: "b"}, {Key: "c"}}
	cases := map[string]string{
		"":      "—",
		"dash":  "—",
		"wordy": "new",
		"count": "3 attrs",
		"list":  "a, b, c",
	}
	for mode, want := range cases {
		got := renderChangedCell(core.ActionCreate, attrs, mode, keysFormatter)
		if got != want {
			t.Errorf("mode=%q create: want %q, got %q", mode, want, got)
		}
	}
}

func TestRenderChangedCell_deleteModes(t *testing.T) {
	attrs := []core.ChangedAttribute{{Key: "x"}, {Key: "y"}}
	cases := map[string]string{
		"":      "—",
		"dash":  "—",
		"wordy": "removed",
		"count": "2 attrs",
		"list":  "x, y",
	}
	for mode, want := range cases {
		got := renderChangedCell(core.ActionDelete, attrs, mode, keysFormatter)
		if got != want {
			t.Errorf("mode=%q delete: want %q, got %q", mode, want, got)
		}
	}
}

func TestRenderChangedCell_readAndNoOpFallToDash(t *testing.T) {
	attrs := []core.ChangedAttribute{{Key: "k"}}
	// Read and no-op aren't create/delete/update/replace; wordy mode should
	// return "—" (not "new"/"removed") to avoid lying.
	for _, action := range []core.Action{core.ActionRead, core.ActionNoOp} {
		got := renderChangedCell(action, attrs, "wordy", keysFormatter)
		if got != "—" {
			t.Errorf("action=%q wordy: want —, got %q", action, got)
		}
	}
}

func TestRenderChangedCell_emptyAttrsOnUpdateFallsBackToDash(t *testing.T) {
	got := renderChangedCell(core.ActionUpdate, nil, "dash", keysFormatter)
	if got != "—" {
		t.Errorf("empty update should fall back to —, got %q", got)
	}
}

func TestRenderChangedCell_emptyAttrsOnListModeFallsBackToDash(t *testing.T) {
	got := renderChangedCell(core.ActionCreate, nil, "list", keysFormatter)
	if got != "—" {
		t.Errorf("empty create in list mode should fall back to —, got %q", got)
	}
}

func TestRenderChangedCell_countZero(t *testing.T) {
	got := renderChangedCell(core.ActionCreate, nil, "count", keysFormatter)
	if got != "0 attrs" {
		t.Errorf("empty create in count mode should show 0 attrs, got %q", got)
	}
}

func TestValidChangedAttrsMode_acceptsValid(t *testing.T) {
	for _, mode := range []string{"", "dash", "wordy", "count", "list"} {
		if err := validChangedAttrsMode("test_block", mode); err != nil {
			t.Errorf("mode=%q should be valid, got %v", mode, err)
		}
	}
}

func TestValidChangedAttrsMode_rejectsUnknown(t *testing.T) {
	err := validChangedAttrsMode("test_block", "bogus")
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name offending mode: %v", err)
	}
	if !strings.Contains(err.Error(), "valid: dash, wordy, count, list") {
		t.Errorf("error should list valid modes: %v", err)
	}
	if !strings.Contains(err.Error(), "test_block") {
		t.Errorf("error should name the block: %v", err)
	}
}

func TestResolveChangedAttrsMode_precedence(t *testing.T) {
	ctx := &BlockContext{Output: OutputOptions{ChangedAttrsDisplay: "wordy"}}

	// Arg overrides ctx.
	if got := resolveChangedAttrsMode(ctx, "count"); got != "count" {
		t.Errorf("arg should override ctx: got %q", got)
	}
	// Empty arg falls to ctx.
	if got := resolveChangedAttrsMode(ctx, ""); got != "wordy" {
		t.Errorf("empty arg should fall to ctx: got %q", got)
	}
	// Empty arg + empty ctx → default.
	nilCtx := &BlockContext{}
	if got := resolveChangedAttrsMode(nilCtx, ""); got != "dash" {
		t.Errorf("empty arg + empty ctx should default to dash: got %q", got)
	}
}
