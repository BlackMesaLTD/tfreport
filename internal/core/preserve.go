package core

import (
	"fmt"
	"strings"
)

// PreserveAttributes walks each allowlisted dotted path on the supplied
// ResourceChange and, when the value at that path is non-sensitive and
// resolvable, stores it in rc.Preserved.
//
// Resolution order:
//
//  1. Try `After` first (the intended post-apply state; the user typically
//     wants the NEW value).
//  2. Fall back to `Before` when the path doesn't resolve in After (handles
//     delete actions where After is nil).
//
// Gating rules (safety model):
//
//   - If either BeforeSensitive or AfterSensitive marks the path sensitive,
//     the key is OMITTED from Preserved entirely (NOT sentinelled). A
//     warning is emitted via `warn(msg)` so operators know why the template
//     saw the `default "—"` fallback. Warning messages contain the path
//     and the resource address but NEVER the value itself.
//   - If the path resolves into AfterUnknown (computed / known after apply),
//     store the literal string KnownAfterApply. This differs from skipping
//     because on creates every resource's `id` is computed — skipping would
//     make `$rc.Preserved.id` uselessly absent for every create row.
//   - Sensitive wins over Computed: a sensitive computed attr is omitted
//     (the most-restrictive output).
//   - Paths that traverse into a list ([]any) stop at the list boundary
//     and return nothing. v1 doesn't support list-element preservation.
//
// Called once per resource from GenerateReport after the Diff pass.
func PreserveAttributes(rc *ResourceChange, paths []string, warn func(msg string)) {
	if len(paths) == 0 {
		return
	}
	if warn == nil {
		warn = func(string) {}
	}

	for _, path := range paths {
		segments := splitPath(path)
		if len(segments) == 0 {
			continue
		}

		// Safety gate: sensitive (EITHER side) → omit + warn, no value read.
		if isSensitive(rc.AfterSensitive, segments) || isSensitive(rc.BeforeSensitive, segments) {
			warn(fmt.Sprintf(
				"tfreport: preserve_attributes: skipped %q on %s — marked sensitive by terraform",
				path, rc.Address,
			))
			continue
		}

		// Computed gate: if the path resolves to true in AfterUnknown, this
		// attr's value is unknown at plan time. Store sentinel, don't walk.
		if isComputedPath(rc.AfterUnknown, segments) {
			addPreserved(rc, path, KnownAfterApply)
			continue
		}

		// Resolve the value: try After, fall back to Before.
		if val, ok := walkPath(rc.After, segments); ok {
			addPreserved(rc, path, val)
			continue
		}
		if val, ok := walkPath(rc.Before, segments); ok {
			addPreserved(rc, path, val)
			continue
		}
		// Path doesn't resolve — silently absent (template uses `default`).
	}
}

// splitPath breaks a dotted allowlist path into segments. Empty or
// all-whitespace input returns nil.
func splitPath(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// walkPath descends `m` along `segments` and returns the leaf value.
// Returns (nil, false) if any segment doesn't resolve or a non-terminal
// step hits a non-map (list or scalar) — list traversal is out of v1 scope.
func walkPath(m map[string]any, segments []string) (any, bool) {
	if m == nil || len(segments) == 0 {
		return nil, false
	}
	cur := any(m)
	for _, seg := range segments {
		mp, ok := cur.(map[string]any)
		if !ok {
			// Hit a non-map (likely a list, or we've already walked past
			// the leaf). v1 doesn't descend into lists.
			return nil, false
		}
		next, exists := mp[seg]
		if !exists {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// isComputedPath reports whether the path resolves to a true bool anywhere
// in the afterUnknown map. AfterUnknown has the same parallel-tree shape
// as the sensitivity masks; we reuse the walker.
func isComputedPath(afterUnknown map[string]any, segments []string) bool {
	if afterUnknown == nil {
		return false
	}
	return isSensitive(any(afterUnknown), segments)
}

// addPreserved puts a value into rc.Preserved, lazily initialising the map.
func addPreserved(rc *ResourceChange, key string, value any) {
	if rc.Preserved == nil {
		rc.Preserved = make(map[string]any)
	}
	rc.Preserved[key] = value
}
