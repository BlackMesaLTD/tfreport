package main

import (
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/formatter/blocks"
)

// TestRenderAll_deterministic guarantees byte-identical output across two
// runs with the same registry. Map iteration order in Go is randomized; if
// any code path in docgen skips sorting, this test will flap.
func TestRenderAll_deterministic(t *testing.T) {
	reg := blocks.Default()
	first := renderAll(reg)
	for i := 0; i < 10; i++ {
		if got := renderAll(reg); got != first {
			t.Fatalf("docgen output differs between runs (iteration %d) — missing sort somewhere", i)
		}
	}
}

// TestRenderAll_containsEveryBlock guards against the docgen skipping blocks
// by accident (e.g. early-return inside the loop).
func TestRenderAll_containsEveryBlock(t *testing.T) {
	reg := blocks.Default()
	out := renderAll(reg)
	for _, name := range reg.Names() {
		marker := "### `" + name + "`"
		if !strings.Contains(out, marker) {
			t.Errorf("rendered output missing heading for block %q", name)
		}
	}
}

// TestRenderBlock_argsTable verifies the args table appears for a block
// that has args, using modules_table as a reference (its Doc() is fully
// populated).
func TestRenderBlock_argsTable(t *testing.T) {
	reg := blocks.Default()
	b, ok := reg.Get("modules_table")
	if !ok {
		t.Fatal("modules_table not registered")
	}
	frag := renderBlock(b.Doc())
	if !strings.Contains(frag, "**Args:**") {
		t.Error("expected Args section for modules_table")
	}
	if !strings.Contains(frag, "**Columns**") {
		t.Error("expected Columns section for modules_table")
	}
	// The example embedded in modules_table's Doc() should appear.
	if !strings.Contains(frag, "**Example:**") {
		t.Error("expected Example section for modules_table")
	}
}

// TestEscapeCell handles pipes and newlines that would otherwise break the
// Markdown table grammar.
func TestEscapeCell(t *testing.T) {
	cases := map[string]string{
		"":                 "—",
		"plain":            "plain",
		"one|two":          `one\|two`,
		"line1\nline2":     "line1 line2",
		"mix | a\nb":       `mix \| a b`,
	}
	for in, want := range cases {
		if got := escapeCell(in); got != want {
			t.Errorf("escapeCell(%q) = %q, want %q", in, got, want)
		}
	}
}
