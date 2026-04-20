package core

import (
	"testing"
)

func TestDiffUpdate(t *testing.T) {
	before := map[string]any{
		"name":             "app-subnet",
		"address_prefixes": []any{"10.0.1.0/24"},
		"tags":             map[string]any{"env": "prod"},
	}
	after := map[string]any{
		"name":             "app-subnet",
		"address_prefixes": []any{"10.0.1.0/24"},
		"tags":             map[string]any{"env": "prod", "managed_by": "terraform"},
	}

	changes := Diff(before, after, nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Key != "tags" {
		t.Errorf("changed key = %q, want %q", changes[0].Key, "tags")
	}
}

func TestDiffCreate(t *testing.T) {
	after := map[string]any{
		"name":     "pe-web",
		"location": "uksouth",
	}
	afterUnknown := map[string]any{
		"id": true,
	}

	changes := Diff(nil, after, afterUnknown)

	// "id" is fully computed (only in afterUnknown) and skipped for creates
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (name, location), got %d: %+v", len(changes), changes)
	}

	keys := ChangedAttributeKeys(changes)
	if keys[0] != "location" || keys[1] != "name" {
		t.Errorf("keys = %v, want [location name]", keys)
	}
}

func TestDiffCreateComputedNilFiltered(t *testing.T) {
	after := map[string]any{
		"name":     "test-subnet",
		"timeouts": nil, // present in after as nil + computed → filtered
		"location": "uksouth",
	}
	afterUnknown := map[string]any{
		"id":       true, // only in afterUnknown → filtered
		"timeouts": true, // nil in after + computed → filtered
	}

	changes := Diff(nil, after, afterUnknown)

	// "id" (fully computed) and "timeouts" (nil + computed) both filtered
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (location, name), got %d: %+v", len(changes), changes)
	}

	keys := ChangedAttributeKeys(changes)
	if keys[0] != "location" || keys[1] != "name" {
		t.Errorf("keys = %v, want [location name]", keys)
	}
}

func TestDiffDelete(t *testing.T) {
	before := map[string]any{
		"name":           "legacy-route",
		"address_prefix": "10.99.0.0/16",
	}

	changes := Diff(before, nil, nil)

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	keys := ChangedAttributeKeys(changes)
	if keys[0] != "address_prefix" || keys[1] != "name" {
		t.Errorf("keys = %v, want [address_prefix name]", keys)
	}
}

func TestDiffNoChanges(t *testing.T) {
	m := map[string]any{"name": "test", "value": float64(42)}
	changes := Diff(m, m, nil)

	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d: %+v", len(changes), changes)
	}
}

func TestDiffBothNil(t *testing.T) {
	changes := Diff(nil, nil, nil)
	if changes != nil {
		t.Errorf("expected nil, got %+v", changes)
	}
}

func TestDiffAttributeAdded(t *testing.T) {
	before := map[string]any{"name": "test"}
	after := map[string]any{"name": "test", "new_attr": "value"}

	changes := Diff(before, after, nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Key != "new_attr" {
		t.Errorf("key = %q, want %q", changes[0].Key, "new_attr")
	}
	if changes[0].OldValue != nil {
		t.Errorf("old value = %v, want nil", changes[0].OldValue)
	}
}

func TestDiffAttributeRemoved(t *testing.T) {
	before := map[string]any{"name": "test", "old_attr": "value"}
	after := map[string]any{"name": "test"}

	changes := Diff(before, after, nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Key != "old_attr" {
		t.Errorf("key = %q, want %q", changes[0].Key, "old_attr")
	}
	if changes[0].NewValue != nil {
		t.Errorf("new value = %v, want nil", changes[0].NewValue)
	}
}

func TestDiffNestedMap(t *testing.T) {
	before := map[string]any{
		"tags": map[string]any{"a": "1", "b": "2"},
	}
	after := map[string]any{
		"tags": map[string]any{"a": "1", "b": "3"},
	}

	changes := Diff(before, after, nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Key != "tags" {
		t.Errorf("key = %q, want %q", changes[0].Key, "tags")
	}
}

func TestDiffComputedAttribute(t *testing.T) {
	before := map[string]any{"name": "test"}
	after := map[string]any{"name": "test"}
	afterUnknown := map[string]any{"etag": true}

	changes := Diff(before, after, afterUnknown)

	// etag is not in before or after, but is in afterUnknown
	// Since both before and after have "name" with same value, no change there
	// etag doesn't appear in the diffUpdate path since it's not in before or after
	if len(changes) != 0 {
		t.Errorf("expected 0 changes (afterUnknown keys not in before/after are not reported for updates), got %d", len(changes))
	}
}

func TestFormatChangedAttribute(t *testing.T) {
	normal := ChangedAttribute{Key: "tags"}
	if got := FormatChangedAttribute(normal); got != "tags" {
		t.Errorf("got %q, want %q", got, "tags")
	}

	computed := ChangedAttribute{Key: "id", Computed: true}
	if got := FormatChangedAttribute(computed); got != "id (computed)" {
		t.Errorf("got %q, want %q", got, "id (computed)")
	}
}
