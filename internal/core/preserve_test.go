package core

import (
	"strings"
	"testing"
)

// captureWarn returns a closure that appends into the supplied slice, for
// tests to inspect warning emission.
func captureWarn(sink *[]string) func(string) {
	return func(m string) { *sink = append(*sink, m) }
}

func TestPreserveAttributes_simpleTopLevel(t *testing.T) {
	rc := &ResourceChange{
		Address: "mock.x",
		Action:  ActionCreate,
		After: map[string]any{
			"id":       "/subscriptions/aaa",
			"location": "uksouth",
		},
	}
	PreserveAttributes(rc, []string{"id", "location"}, nil)
	if rc.Preserved["id"] != "/subscriptions/aaa" {
		t.Errorf("id: %v", rc.Preserved["id"])
	}
	if rc.Preserved["location"] != "uksouth" {
		t.Errorf("location: %v", rc.Preserved["location"])
	}
}

func TestPreserveAttributes_dottedPath(t *testing.T) {
	rc := &ResourceChange{
		Address: "mock.x",
		Action:  ActionUpdate,
		After: map[string]any{
			"tags": map[string]any{
				"env":   "prod",
				"owner": "platform",
			},
		},
	}
	PreserveAttributes(rc, []string{"tags.env", "tags.owner"}, nil)
	if rc.Preserved["tags.env"] != "prod" {
		t.Errorf("tags.env: %v", rc.Preserved["tags.env"])
	}
	if rc.Preserved["tags.owner"] != "platform" {
		t.Errorf("tags.owner: %v", rc.Preserved["tags.owner"])
	}
}

func TestPreserveAttributes_missingPathIsAbsent(t *testing.T) {
	rc := &ResourceChange{
		Address: "mock.x",
		After:   map[string]any{"id": "abc"},
	}
	PreserveAttributes(rc, []string{"not_present", "also.missing"}, nil)
	if _, ok := rc.Preserved["not_present"]; ok {
		t.Error("missing path should not be in Preserved")
	}
	if _, ok := rc.Preserved["also.missing"]; ok {
		t.Error("missing nested path should not be in Preserved")
	}
}

func TestPreserveAttributes_sensitiveIsAbsentAndWarns(t *testing.T) {
	rc := &ResourceChange{
		Address: "mock.secret",
		Action:  ActionUpdate,
		After: map[string]any{
			"password": "LEAK_CANARY_VALUE",
			"id":       "safe-id",
		},
		AfterSensitive: map[string]any{"password": true},
	}
	var warnings []string
	PreserveAttributes(rc, []string{"password", "id"}, captureWarn(&warnings))

	if _, ok := rc.Preserved["password"]; ok {
		t.Error("sensitive password must NOT be in Preserved")
	}
	if rc.Preserved["id"] != "safe-id" {
		t.Error("non-sensitive id should be preserved")
	}
	// Warning emitted.
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	// Warning contains the path and address but NOT the value — CANARY check.
	w := warnings[0]
	if !strings.Contains(w, "password") {
		t.Error("warning should name the path")
	}
	if !strings.Contains(w, "mock.secret") {
		t.Error("warning should name the resource address")
	}
	if strings.Contains(w, "LEAK_CANARY_VALUE") {
		t.Errorf("CANARY leak in warning message: %q", w)
	}
}

func TestPreserveAttributes_sensitiveEitherSide(t *testing.T) {
	// Value sensitive in Before but not After (e.g. rotating a password);
	// must still be absent.
	rc := &ResourceChange{
		Address:         "mock.rotated",
		Action:          ActionUpdate,
		Before:          map[string]any{"token": "LEAK_CANARY_ROTATED_OLD"},
		After:           map[string]any{"token": "not-sensitive-anymore"},
		BeforeSensitive: map[string]any{"token": true},
		AfterSensitive:  map[string]any{},
	}
	var warnings []string
	PreserveAttributes(rc, []string{"token"}, captureWarn(&warnings))

	if _, ok := rc.Preserved["token"]; ok {
		t.Error("either-side sensitive should still be absent from Preserved")
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
}

func TestPreserveAttributes_computedStoresSentinel(t *testing.T) {
	// On create, `id` is typically only in AfterUnknown — not After.
	rc := &ResourceChange{
		Address:      "mock.new",
		Action:       ActionCreate,
		After:        map[string]any{"name": "newresource"},
		AfterUnknown: map[string]any{"id": true},
	}
	PreserveAttributes(rc, []string{"id", "name"}, nil)
	if rc.Preserved["id"] != KnownAfterApply {
		t.Errorf("computed id should be %q, got %v", KnownAfterApply, rc.Preserved["id"])
	}
	if rc.Preserved["name"] != "newresource" {
		t.Errorf("name: %v", rc.Preserved["name"])
	}
}

func TestPreserveAttributes_deleteFallsBackToBefore(t *testing.T) {
	// Delete: After is nil; must use Before.
	rc := &ResourceChange{
		Address: "mock.gone",
		Action:  ActionDelete,
		Before:  map[string]any{"id": "doomed-id", "name": "bye"},
		After:   nil,
	}
	PreserveAttributes(rc, []string{"id", "name"}, nil)
	if rc.Preserved["id"] != "doomed-id" {
		t.Errorf("id: %v", rc.Preserved["id"])
	}
	if rc.Preserved["name"] != "bye" {
		t.Errorf("name: %v", rc.Preserved["name"])
	}
}

func TestPreserveAttributes_listPathReturnsNothing(t *testing.T) {
	// Path walks into a list → v1 doesn't support, silently absent.
	rc := &ResourceChange{
		Address: "mock.listy",
		Action:  ActionUpdate,
		After: map[string]any{
			"rules": []any{
				map[string]any{"name": "r1"},
			},
		},
	}
	PreserveAttributes(rc, []string{"rules.name", "rules.0.name"}, nil)
	if _, ok := rc.Preserved["rules.name"]; ok {
		t.Error("list path should not resolve in v1")
	}
	if _, ok := rc.Preserved["rules.0.name"]; ok {
		t.Error("indexed list path should not resolve in v1")
	}
}

func TestPreserveAttributes_wholeListPreservedAsIs(t *testing.T) {
	// Allowlisting `rules` (without descending) preserves the list as a value.
	rules := []any{map[string]any{"name": "r1"}}
	rc := &ResourceChange{
		Address: "mock.listy",
		Action:  ActionUpdate,
		After:   map[string]any{"rules": rules},
	}
	PreserveAttributes(rc, []string{"rules"}, nil)
	if got, ok := rc.Preserved["rules"]; !ok {
		t.Error("rules list should be preserved as-is")
	} else if _, isList := got.([]any); !isList {
		t.Errorf("rules preserved as wrong type: %T", got)
	}
}

func TestPreserveAttributes_sensitiveWinsOverComputed(t *testing.T) {
	// Both sensitive AND computed — sensitive wins (absent, not sentinelled).
	rc := &ResourceChange{
		Address:        "mock.sensComp",
		Action:         ActionCreate,
		After:          map[string]any{"name": "x"},
		AfterUnknown:   map[string]any{"secret_id": true},
		AfterSensitive: map[string]any{"secret_id": true},
	}
	var warnings []string
	PreserveAttributes(rc, []string{"secret_id"}, captureWarn(&warnings))
	if _, ok := rc.Preserved["secret_id"]; ok {
		t.Error("sensitive+computed should be absent (sensitive wins)")
	}
	if len(warnings) != 1 {
		t.Errorf("expected sensitivity warning, got %d warnings", len(warnings))
	}
}

func TestPreserveAttributes_emptyPathListIsNoOp(t *testing.T) {
	rc := &ResourceChange{Address: "x", After: map[string]any{"id": "a"}}
	PreserveAttributes(rc, nil, nil)
	if rc.Preserved != nil {
		t.Errorf("empty paths should leave Preserved nil, got %v", rc.Preserved)
	}
}

func TestPreserveAttributes_nilWarnAcceptable(t *testing.T) {
	// Nil warn func should not panic on a sensitive skip.
	rc := &ResourceChange{
		Address:        "x",
		After:          map[string]any{"s": "LEAK_CANARY"},
		AfterSensitive: map[string]any{"s": true},
	}
	PreserveAttributes(rc, []string{"s"}, nil) // nil warn — must not panic
	if _, ok := rc.Preserved["s"]; ok {
		t.Error("s should still be absent")
	}
}
