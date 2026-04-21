package core

import (
	"fmt"
	"reflect"
	"sort"
)

// Diff compares before and after maps for a resource change and returns
// the list of changed attributes. Marks attributes as Computed if they
// appear in afterUnknown, and as Sensitive if either sensitivity mask
// flags them. When Sensitive=true, OldValue/NewValue are replaced with
// the SensitiveMask sentinel before return — belt-and-braces, so even
// downstream code that forgets to check .Sensitive sees a safe value.
//
// beforeSensitive / afterSensitive come from terraform's plan JSON
// before_sensitive / after_sensitive fields. Either may be nil (empty
// mask = nothing sensitive).
func Diff(before, after, afterUnknown map[string]any, beforeSensitive, afterSensitive any) []ChangedAttribute {
	if before == nil && after == nil {
		return nil
	}

	var attrs []ChangedAttribute
	switch {
	case before == nil:
		attrs = diffCreate(after, afterUnknown)
	case after == nil:
		attrs = diffDelete(before)
	default:
		attrs = diffUpdate(before, after, afterUnknown)
	}

	// Mask sensitive values. Checked per-key against both masks — either
	// side flagging the key (e.g. rotating a password that WAS sensitive)
	// is enough to mark the attribute Sensitive.
	for i, a := range attrs {
		path := []string{a.Key}
		if isSensitive(afterSensitive, path) || isSensitive(beforeSensitive, path) {
			attrs[i].Sensitive = true
			attrs[i].OldValue = SensitiveMask
			attrs[i].NewValue = SensitiveMask
		}
	}
	return attrs
}

func diffCreate(after, afterUnknown map[string]any) []ChangedAttribute {
	var changes []ChangedAttribute
	keys := sortedKeys(after)

	for _, k := range keys {
		computed := isComputed(afterUnknown, k)
		// Skip attributes that are nil in after AND computed — these are
		// server-side placeholders (e.g., id, guid) that add noise on creates
		if after[k] == nil && computed {
			continue
		}
		changes = append(changes, ChangedAttribute{
			Key:      k,
			OldValue: nil,
			NewValue: after[k],
			Computed: computed,
		})
	}

	// Skip keys that are only in afterUnknown (fully computed) for creates.
	// These are purely server-assigned attributes like "id" that have no
	// user-specified value — including them adds noise to create diffs.

	return changes
}

func diffDelete(before map[string]any) []ChangedAttribute {
	var changes []ChangedAttribute
	keys := sortedKeys(before)

	for _, k := range keys {
		changes = append(changes, ChangedAttribute{
			Key:      k,
			OldValue: before[k],
			NewValue: nil,
		})
	}

	return changes
}

func diffUpdate(before, after, afterUnknown map[string]any) []ChangedAttribute {
	var changes []ChangedAttribute

	// Collect all keys from both maps
	allKeys := make(map[string]bool)
	for k := range before {
		allKeys[k] = true
	}
	for k := range after {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		oldVal, oldExists := before[k]
		newVal, newExists := after[k]
		computed := isComputed(afterUnknown, k)

		// Attribute added
		if !oldExists && newExists {
			changes = append(changes, ChangedAttribute{
				Key:      k,
				OldValue: nil,
				NewValue: newVal,
				Computed: computed,
			})
			continue
		}

		// Attribute removed
		if oldExists && !newExists {
			// Check if it's becoming computed
			if computed {
				changes = append(changes, ChangedAttribute{
					Key:      k,
					OldValue: oldVal,
					NewValue: nil,
					Computed: true,
				})
			} else {
				changes = append(changes, ChangedAttribute{
					Key:      k,
					OldValue: oldVal,
					NewValue: nil,
				})
			}
			continue
		}

		// Both exist — compare values
		if !reflect.DeepEqual(oldVal, newVal) {
			changes = append(changes, ChangedAttribute{
				Key:      k,
				OldValue: oldVal,
				NewValue: newVal,
				Computed: computed,
			})
		}
	}

	return changes
}

// isComputed checks if a key is marked as computed in afterUnknown.
func isComputed(afterUnknown map[string]any, key string) bool {
	if afterUnknown == nil {
		return false
	}
	v, ok := afterUnknown[key]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ChangedAttributeKeys returns just the key names from a slice of changed attributes.
func ChangedAttributeKeys(attrs []ChangedAttribute) []string {
	keys := make([]string, len(attrs))
	for i, a := range attrs {
		keys[i] = a.Key
	}
	return keys
}

// FormatChangedAttribute returns a human-readable string for a changed attribute.
func FormatChangedAttribute(attr ChangedAttribute) string {
	if attr.Computed {
		return fmt.Sprintf("%s (computed)", attr.Key)
	}
	return attr.Key
}
