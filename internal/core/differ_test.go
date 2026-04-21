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

	changes := Diff(before, after, nil, nil, nil)

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

	changes := Diff(nil, after, afterUnknown, nil, nil)

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

	changes := Diff(nil, after, afterUnknown, nil, nil)

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

	changes := Diff(before, nil, nil, nil, nil)

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
	changes := Diff(m, m, nil, nil, nil)

	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d: %+v", len(changes), changes)
	}
}

func TestDiffBothNil(t *testing.T) {
	changes := Diff(nil, nil, nil, nil, nil)
	if changes != nil {
		t.Errorf("expected nil, got %+v", changes)
	}
}

func TestDiffAttributeAdded(t *testing.T) {
	before := map[string]any{"name": "test"}
	after := map[string]any{"name": "test", "new_attr": "value"}

	changes := Diff(before, after, nil, nil, nil)

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

	changes := Diff(before, after, nil, nil, nil)

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

	changes := Diff(before, after, nil, nil, nil)

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

	changes := Diff(before, after, afterUnknown, nil, nil)

	// etag is not in before or after, but is in afterUnknown
	// Since both before and after have "name" with same value, no change there
	// etag doesn't appear in the diffUpdate path since it's not in before or after
	if len(changes) != 0 {
		t.Errorf("expected 0 changes (afterUnknown keys not in before/after are not reported for updates), got %d", len(changes))
	}
}

// --- sensitivity masking (Layer 1) ---

func TestDiff_sensitiveTopLevel(t *testing.T) {
	before := map[string]any{"password": "LEAK_CANARY_OLD", "name": "store-1"}
	after := map[string]any{"password": "LEAK_CANARY_NEW", "name": "store-1"}
	afterSens := map[string]any{"password": true}

	changes := Diff(before, after, nil, nil, afterSens)

	// password should be Sensitive, values masked
	var pw *ChangedAttribute
	for i := range changes {
		if changes[i].Key == "password" {
			pw = &changes[i]
			break
		}
	}
	if pw == nil {
		t.Fatal("expected password attribute in changes")
	}
	if !pw.Sensitive {
		t.Error("password should be marked Sensitive")
	}
	if pw.OldValue != SensitiveMask || pw.NewValue != SensitiveMask {
		t.Errorf("sensitive values not masked: old=%v new=%v", pw.OldValue, pw.NewValue)
	}
	// Defence-in-depth: raw canary must not appear anywhere in the diff output
	for _, c := range changes {
		if s, ok := c.OldValue.(string); ok && (s == "LEAK_CANARY_OLD" || s == "LEAK_CANARY_NEW") {
			t.Errorf("LEAK CANARY leaked in OldValue for attr %q", c.Key)
		}
		if s, ok := c.NewValue.(string); ok && (s == "LEAK_CANARY_OLD" || s == "LEAK_CANARY_NEW") {
			t.Errorf("LEAK CANARY leaked in NewValue for attr %q", c.Key)
		}
	}
}

func TestDiff_sensitiveOnCreate(t *testing.T) {
	// Create path: only afterSensitive matters (there is no before).
	after := map[string]any{"password": "LEAK_CANARY_CREATE", "name": "new"}
	afterSens := map[string]any{"password": true}

	changes := Diff(nil, after, nil, nil, afterSens)

	var pw *ChangedAttribute
	for i := range changes {
		if changes[i].Key == "password" {
			pw = &changes[i]
		}
	}
	if pw == nil || !pw.Sensitive {
		t.Fatal("password on create should be Sensitive")
	}
	if pw.NewValue != SensitiveMask {
		t.Errorf("create password NewValue not masked: %v", pw.NewValue)
	}
}

func TestDiff_sensitiveOnDelete(t *testing.T) {
	// Delete path: only beforeSensitive matters.
	before := map[string]any{"password": "LEAK_CANARY_DELETE", "name": "gone"}
	beforeSens := map[string]any{"password": true}

	changes := Diff(before, nil, nil, beforeSens, nil)

	var pw *ChangedAttribute
	for i := range changes {
		if changes[i].Key == "password" {
			pw = &changes[i]
		}
	}
	if pw == nil || !pw.Sensitive {
		t.Fatal("password on delete should be Sensitive")
	}
	if pw.OldValue != SensitiveMask {
		t.Errorf("delete password OldValue not masked: %v", pw.OldValue)
	}
}

func TestDiff_sensitiveAncestorPropagation(t *testing.T) {
	// Whole-attr sensitive (ancestor true) means any child path is sensitive.
	// In the Diff context we only probe at top-level keys per attribute —
	// so a top-level `true` mask marks the attribute sensitive.
	before := map[string]any{"credentials": map[string]any{"user": "x", "pass": "LEAK_CANARY_OLD"}}
	after := map[string]any{"credentials": map[string]any{"user": "x", "pass": "LEAK_CANARY_NEW"}}
	afterSens := map[string]any{"credentials": true}

	changes := Diff(before, after, nil, nil, afterSens)

	var cr *ChangedAttribute
	for i := range changes {
		if changes[i].Key == "credentials" {
			cr = &changes[i]
		}
	}
	if cr == nil || !cr.Sensitive {
		t.Fatal("credentials (whole-subtree sensitive) should be Sensitive")
	}
	// The entire map value must be masked, not emitted raw.
	if cr.OldValue != SensitiveMask || cr.NewValue != SensitiveMask {
		t.Errorf("credentials map should be masked, got old=%v new=%v", cr.OldValue, cr.NewValue)
	}
}

func TestDiff_eitherSideSensitiveIsEnough(t *testing.T) {
	// If before marked sensitive but after doesn't (e.g. rotating a value
	// that was sensitive), still mask — don't leak the pre-rotation value.
	before := map[string]any{"token": "LEAK_CANARY_OLD"}
	after := map[string]any{"token": "not-sensitive-anymore"}
	beforeSens := map[string]any{"token": true}
	afterSens := map[string]any{}

	changes := Diff(before, after, nil, beforeSens, afterSens)

	if len(changes) != 1 || !changes[0].Sensitive {
		t.Fatalf("expected 1 Sensitive change; got %+v", changes)
	}
	if changes[0].OldValue != SensitiveMask || changes[0].NewValue != SensitiveMask {
		t.Error("either-side sensitive should mask BOTH values")
	}
}

func TestDiff_nonSensitivePassesValuesThrough(t *testing.T) {
	// Baseline: attrs NOT marked sensitive should retain their values intact.
	before := map[string]any{"location": "uksouth", "password": "LEAK_CANARY_OLD"}
	after := map[string]any{"location": "ukwest", "password": "LEAK_CANARY_NEW"}
	// Only password is sensitive; location is a normal diffed field.
	afterSens := map[string]any{"password": true}

	changes := Diff(before, after, nil, nil, afterSens)

	var loc *ChangedAttribute
	for i := range changes {
		if changes[i].Key == "location" {
			loc = &changes[i]
		}
	}
	if loc == nil {
		t.Fatal("expected location change")
	}
	if loc.Sensitive {
		t.Error("location should NOT be Sensitive")
	}
	if loc.OldValue != "uksouth" || loc.NewValue != "ukwest" {
		t.Errorf("location values should pass through: old=%v new=%v", loc.OldValue, loc.NewValue)
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
