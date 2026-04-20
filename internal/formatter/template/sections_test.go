package template

import (
	"strings"
	"testing"
)

const fixtureTmpl = `preamble text
{{/* block: title */}}
title body
{{/* block: key_changes */}}
key body
{{/* block: footer */}}
footer body
`

func TestApplySections_noSelector(t *testing.T) {
	out, err := ApplySections(fixtureTmpl, SectionSelector{})
	if err != nil {
		t.Fatal(err)
	}
	if out != fixtureTmpl {
		t.Error("no selector should return input unchanged")
	}
}

func TestApplySections_show(t *testing.T) {
	out, err := ApplySections(fixtureTmpl, SectionSelector{Show: []string{"title", "footer"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "title body") {
		t.Errorf("want title kept, got %q", out)
	}
	if strings.Contains(out, "key body") {
		t.Errorf("want key_changes dropped, got %q", out)
	}
	if !strings.Contains(out, "footer body") {
		t.Errorf("want footer kept, got %q", out)
	}
	if !strings.Contains(out, "preamble text") {
		t.Error("preamble must be preserved")
	}
}

func TestApplySections_hide(t *testing.T) {
	out, err := ApplySections(fixtureTmpl, SectionSelector{Hide: []string{"footer"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "footer body") {
		t.Errorf("want footer dropped, got %q", out)
	}
	if !strings.Contains(out, "title body") {
		t.Errorf("want title kept, got %q", out)
	}
	if !strings.Contains(out, "key body") {
		t.Errorf("want key body kept, got %q", out)
	}
}

func TestApplySections_unknownIgnored(t *testing.T) {
	out, err := ApplySections(fixtureTmpl, SectionSelector{Hide: []string{"future_block"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != fixtureTmpl {
		t.Error("unknown hide name should be silently ignored")
	}
}

func TestApplySections_mutexError(t *testing.T) {
	_, err := ApplySections(fixtureTmpl, SectionSelector{Show: []string{"title"}, Hide: []string{"footer"}})
	if err == nil {
		t.Error("want error when both show and hide set")
	}
}

func TestApplySections_noMarkers(t *testing.T) {
	raw := "plain text, no markers"
	out, err := ApplySections(raw, SectionSelector{Show: []string{"title"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != raw {
		t.Error("template without markers should be returned unchanged")
	}
}
