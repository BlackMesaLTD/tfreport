package preserve

import (
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct{ name, want string }{
		{"checkbox", "checkbox"},
		{"radio", "radio"},
		{"text", "text"},
		{"block", "block"},
		{"", "block"},
		{"unknown-kind", "block"},
	}
	for _, tc := range cases {
		got := Resolve(tc.name).Name()
		if got != tc.want {
			t.Errorf("Resolve(%q).Name() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIsKnownKind(t *testing.T) {
	for _, k := range KnownKinds() {
		if !IsKnownKind(k) {
			t.Errorf("IsKnownKind(%q) = false, want true", k)
		}
	}
	if IsKnownKind("dropdown") {
		t.Error("IsKnownKind(dropdown) = true, want false")
	}
}

func TestCheckboxMerge(t *testing.T) {
	// Current body is the canonical rendered structure; merge should
	// preserve the tick from prior but rebuild structure from current.
	cur := Region{Kind: "checkbox", Body: "\n- [ ] "}
	cases := []struct {
		prior, want string
	}{
		{"\n- [x] ", "\n- [x] "},
		{"\n- [ ] ", "\n- [ ] "},
		{"\n- [X] ", "\n- [x] "},                    // normalised
		{"- [x]", "\n- [x] "},                        // prior missing surround → structure rebuilt
		{"[x]", "\n- [x] "},                          // user deleted dash → dash restored
		{"\n [x]", "\n- [x] "},                       // user deleted dash (space kept) → restored
		{"\n- [x] CANCEL", "\n- [x] "},               // user added note → stripped, tick kept
		{"garbage", "\n- [ ] "},                      // no bracket token → current default
		{"", "\n- [ ] "},                             // empty prior → current default
		{"\n- [x] alpha\n- [ ] beta", "\n- [ ] "},    // ambiguous (two tokens) → current default
	}
	k := checkboxKind{}
	for _, tc := range cases {
		got := k.Merge(Region{Body: tc.prior}, cur)
		if got != tc.want {
			t.Errorf("Merge(prior=%q) = %q, want %q", tc.prior, got, tc.want)
		}
	}
}

func TestRadioMerge_preserveSelection(t *testing.T) {
	prior := Region{Kind: "radio", Body: "- [ ] platform\n- [x] security\n- [ ] hold"}
	current := Region{Kind: "radio", Body: "- [ ] platform\n- [ ] security\n- [ ] hold"}
	got := radioKind{}.Merge(prior, current)
	want := "- [ ] platform\n- [x] security\n- [ ] hold"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRadioMerge_optionDropped(t *testing.T) {
	// Prior had "security" ticked; current no longer offers it.
	prior := Region{Kind: "radio", Body: "- [ ] platform\n- [x] security\n- [ ] hold"}
	current := Region{Kind: "radio", Body: "- [ ] platform\n- [ ] secops\n- [ ] hold"}
	got := radioKind{}.Merge(prior, current)
	// All unticked — safer than moving the tick.
	if strings.Count(got, "[x]") != 0 {
		t.Errorf("want all unticked when prior option gone, got %q", got)
	}
}

func TestRadioMerge_noPriorSelection(t *testing.T) {
	prior := Region{Kind: "radio", Body: "- [ ] a\n- [ ] b\n- [ ] c"}
	current := Region{Kind: "radio", Body: "- [ ] a\n- [ ] b\n- [ ] c"}
	got := radioKind{}.Merge(prior, current)
	if got != current.Body {
		t.Errorf("no prior selection should return current, got %q", got)
	}
}

func TestTextMerge(t *testing.T) {
	prior := Region{Kind: "text", Body: "Reviewed by Sarah"}
	current := Region{Kind: "text", Body: "placeholder"}
	got := textKind{}.Merge(prior, current)
	if got != prior.Body {
		t.Errorf("text merge should carry prior verbatim, got %q", got)
	}
}

func TestBlockMerge(t *testing.T) {
	prior := Region{Kind: "block", Body: "line1\nline2\nline3"}
	current := Region{Kind: "block", Body: "default"}
	got := blockKind{}.Merge(prior, current)
	if got != prior.Body {
		t.Errorf("block merge should carry prior verbatim, got %q", got)
	}
}
