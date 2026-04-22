package preserve

import (
	"strings"
	"testing"
)

func TestValidateID(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"deploy:sub-alpha", false},
		{"simple", false},
		{"with.dots_and-hyphens", false},
		{"", true},
		{"bad id with spaces", true},
		{"bad\"quote", true},
		{"bad-->injection", true},
	}
	for _, tc := range tests {
		err := ValidateID(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateID(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestSlugifyID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"prod-hub", "prod-hub"},
		{"sub alpha", "sub-alpha"},
		{"sub@alpha!", "sub-alpha-"},
		{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000001"},
		{"", ""},
		{"has/slashes", "has-slashes"},
		{"colons:allowed", "colons:allowed"},
	}
	for _, tc := range tests {
		got := SlugifyID(tc.in)
		if got != tc.want {
			t.Errorf("SlugifyID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseRegions_single(t *testing.T) {
	text := `Hello <!-- tfreport:preserve-begin id="deploy:x" kind="checkbox" -->[x]<!-- tfreport:preserve-end id="deploy:x" --> world`
	got, err := ParseRegions(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 region, got %d", len(got))
	}
	r, ok := got["deploy:x"]
	if !ok {
		t.Fatal("missing id deploy:x")
	}
	if r.Kind != "checkbox" {
		t.Errorf("kind = %q, want checkbox", r.Kind)
	}
	if r.Body != "[x]" {
		t.Errorf("body = %q, want [x]", r.Body)
	}
}

func TestParseRegions_multipleAndAttrs(t *testing.T) {
	text := strings.Join([]string{
		`<!-- tfreport:preserve-begin id="a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="a" -->`,
		`<!-- tfreport:preserve-begin id="b" kind="radio" options="x,y,z" -->`,
		`- [x] x`,
		`- [ ] y`,
		`- [ ] z`,
		`<!-- tfreport:preserve-end id="b" -->`,
	}, "\n")
	got, err := ParseRegions(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 regions, got %d", len(got))
	}
	if got["b"].Attrs["options"] != "x,y,z" {
		t.Errorf("b.Attrs[options] = %q, want x,y,z", got["b"].Attrs["options"])
	}
}

func TestParseRegions_duplicateID(t *testing.T) {
	text := strings.Join([]string{
		`<!-- tfreport:preserve-begin id="a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="a" -->`,
		`<!-- tfreport:preserve-begin id="a" kind="checkbox" -->[x]<!-- tfreport:preserve-end id="a" -->`,
	}, "\n")
	_, err := ParseRegions(text)
	if err == nil {
		t.Fatal("want error on duplicate id")
	}
	if !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("err = %v, want 'duplicate id'", err)
	}
}

func TestParseRegions_orphanEnd(t *testing.T) {
	text := `text <!-- tfreport:preserve-end id="a" --> more`
	_, err := ParseRegions(text)
	if err == nil {
		t.Fatal("want error on orphan end marker")
	}
	if !strings.Contains(err.Error(), "no matching preserve-begin") {
		t.Errorf("err = %v, want 'no matching preserve-begin'", err)
	}
}

func TestParseRegions_mismatchedIDs(t *testing.T) {
	text := `<!-- tfreport:preserve-begin id="a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="b" -->`
	_, err := ParseRegions(text)
	if err == nil {
		t.Fatal("want error on mismatched ids")
	}
}

func TestParseRegions_missingID(t *testing.T) {
	text := `<!-- tfreport:preserve-begin kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="a" -->`
	_, err := ParseRegions(text)
	if err == nil {
		t.Fatal("want error when begin lacks id")
	}
}

func TestParseRegions_empty(t *testing.T) {
	got, err := ParseRegions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want empty map, got %d entries", len(got))
	}
}

func TestParseRegions_noRegions(t *testing.T) {
	got, err := ParseRegions("plain text, no markers")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want empty map, got %d entries", len(got))
	}
}

func TestRenderBegin(t *testing.T) {
	got, err := RenderBegin("deploy:x", "checkbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<!-- tfreport:preserve-begin id="deploy:x" kind="checkbox" -->`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBegin_attrs(t *testing.T) {
	got, err := RenderBegin("approver", "radio", map[string]string{"options": "a,b,c"})
	if err != nil {
		t.Fatal(err)
	}
	want := `<!-- tfreport:preserve-begin id="approver" kind="radio" options="a,b,c" -->`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBegin_invalidID(t *testing.T) {
	_, err := RenderBegin("bad id", "checkbox", nil)
	if err == nil {
		t.Fatal("want error for invalid id")
	}
}

func TestRenderEnd(t *testing.T) {
	got, err := RenderEnd("deploy:x")
	if err != nil {
		t.Fatal(err)
	}
	want := `<!-- tfreport:preserve-end id="deploy:x" -->`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	// Produce markers, parse, confirm fields.
	begin, _ := RenderBegin("x", "checkbox", nil)
	end, _ := RenderEnd("x")
	text := begin + "[x]" + end
	got, err := ParseRegions(text)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := got["x"]
	if !ok || r.Kind != "checkbox" || r.Body != "[x]" {
		t.Errorf("round-trip mismatch: %+v", r)
	}
}
