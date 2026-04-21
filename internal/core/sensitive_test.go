package core

import "testing"

func TestIsSensitive_nilMask(t *testing.T) {
	if isSensitive(nil, []string{"any", "path"}) {
		t.Error("nil mask should not be sensitive")
	}
	if isSensitive(nil, nil) {
		t.Error("nil mask + nil path should not be sensitive")
	}
}

func TestIsSensitive_rootTrue(t *testing.T) {
	// A bool true at the root means everything sensitive.
	if !isSensitive(true, []string{"anything", "nested"}) {
		t.Error("root true should propagate to any path")
	}
	if !isSensitive(true, nil) {
		t.Error("root true with empty path should be sensitive")
	}
}

func TestIsSensitive_rootFalse(t *testing.T) {
	if isSensitive(false, []string{"x"}) {
		t.Error("root false means not sensitive")
	}
}

func TestIsSensitive_flatKey(t *testing.T) {
	mask := map[string]any{"password": true, "name": false}
	if !isSensitive(mask, []string{"password"}) {
		t.Error("password should be sensitive")
	}
	if isSensitive(mask, []string{"name"}) {
		t.Error("name should not be sensitive")
	}
	if isSensitive(mask, []string{"missing"}) {
		t.Error("missing key should not be sensitive (safe default)")
	}
}

func TestIsSensitive_nestedKey(t *testing.T) {
	mask := map[string]any{"tags": map[string]any{"secret_key": true, "env": false}}
	if !isSensitive(mask, []string{"tags", "secret_key"}) {
		t.Error("tags.secret_key should be sensitive")
	}
	if isSensitive(mask, []string{"tags", "env"}) {
		t.Error("tags.env should not be sensitive")
	}
	if isSensitive(mask, []string{"tags", "missing"}) {
		t.Error("tags.missing should not be sensitive")
	}
}

func TestIsSensitive_ancestorPropagation(t *testing.T) {
	// Sensitive at ancestor means all descendants sensitive.
	mask := map[string]any{"credentials": true}
	if !isSensitive(mask, []string{"credentials"}) {
		t.Error("credentials whole-subtree should be sensitive")
	}
	if !isSensitive(mask, []string{"credentials", "username"}) {
		t.Error("credentials.username should inherit sensitive from ancestor")
	}
	if !isSensitive(mask, []string{"credentials", "nested", "deeper"}) {
		t.Error("deeply-nested path under sensitive ancestor should be sensitive")
	}
}

func TestIsSensitive_listElement(t *testing.T) {
	mask := map[string]any{"conns": []any{false, true, false}}
	if isSensitive(mask, []string{"conns", "0"}) {
		t.Error("conns[0] should not be sensitive")
	}
	if !isSensitive(mask, []string{"conns", "1"}) {
		t.Error("conns[1] should be sensitive")
	}
	if isSensitive(mask, []string{"conns", "2"}) {
		t.Error("conns[2] should not be sensitive")
	}
	// Out-of-range index is not sensitive (safe default).
	if isSensitive(mask, []string{"conns", "5"}) {
		t.Error("out-of-range index should not be sensitive")
	}
	// Non-integer path step into a list returns not sensitive.
	if isSensitive(mask, []string{"conns", "notanint"}) {
		t.Error("non-integer path into list should not be sensitive")
	}
}

func TestIsSensitive_mixedMapList(t *testing.T) {
	// Mixed nesting: `rules` is a list; second element has a sensitive `name`.
	mask := map[string]any{
		"rules": []any{
			false,
			map[string]any{"name": true, "priority": false},
		},
	}
	if !isSensitive(mask, []string{"rules", "1", "name"}) {
		t.Error("rules[1].name should be sensitive")
	}
	if isSensitive(mask, []string{"rules", "1", "priority"}) {
		t.Error("rules[1].priority should not be sensitive")
	}
	if isSensitive(mask, []string{"rules", "0", "name"}) {
		t.Error("rules[0] is false — rules[0].name should not be sensitive")
	}
}

func TestIsSensitive_deeplyNestedAncestor(t *testing.T) {
	// Multiple levels of ancestor propagation.
	mask := map[string]any{"a": map[string]any{"b": map[string]any{"c": true}}}
	cases := map[string]bool{
		"a,b,c":   true,
		"a,b,c,d": true, // descendants of sensitive c
		"a,b":     false,
		"a":       false,
	}
	for pathStr, want := range cases {
		path := splitForTest(pathStr)
		if got := isSensitive(mask, path); got != want {
			t.Errorf("path=%q: want %v, got %v", pathStr, want, got)
		}
	}
}

func TestIsSensitive_wrongPathType(t *testing.T) {
	// Integer path step into a map should return false (safe default).
	mask := map[string]any{"a": map[string]any{"b": true}}
	if isSensitive(mask, []string{"0"}) {
		t.Error("integer-like key into map without that key should not be sensitive")
	}
}

func TestIsSensitive_malformedMask(t *testing.T) {
	// A bare string where a structure was expected — malformed.
	if isSensitive("not a real mask", []string{"anything"}) {
		t.Error("malformed mask should default to not sensitive")
	}
	// A number — same deal.
	if isSensitive(42, []string{"x"}) {
		t.Error("number mask should default to not sensitive")
	}
}

func TestIsSensitive_missingDeepPath(t *testing.T) {
	mask := map[string]any{"a": map[string]any{"b": map[string]any{"c": true}}}
	if isSensitive(mask, []string{"x", "y", "z"}) {
		t.Error("fully-missing path should not be sensitive")
	}
}

// splitForTest is a tiny helper to build paths from comma-separated strings
// in the compact test-case tables above. Not exported.
func splitForTest(s string) []string {
	if s == "" {
		return nil
	}
	parts := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
