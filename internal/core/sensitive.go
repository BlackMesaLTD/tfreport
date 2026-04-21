package core

import "strconv"

// isSensitive reports whether the value at `path` within terraform's
// sensitivity mask is marked sensitive. The mask mirrors the shape of the
// corresponding before/after value tree and may be:
//
//   - bool true   — everything at this subtree root and below is sensitive
//   - bool false  — not sensitive (equivalent to absent)
//   - map         — recurse per-key
//   - slice       — recurse per-index (path elements parsed as integers)
//   - nil         — not sensitive
//
// Propagation: once a `true` is encountered anywhere up the walk from root
// to the supplied path, the result is true. Missing path elements (key
// doesn't exist in mask / index out of range) return false — the safe
// default when the mask doesn't say anything about that subtree.
//
// Malformed masks (unexpected types like a bare string) also return false:
// terraform shouldn't produce those, and defaulting to non-sensitive for a
// malformed mask would be unsafe, so we instead interpret as "mask gives
// no information" — callers are expected to lean on other safety layers
// (the report-JSON round-trip already strips raw values). If we ever wanted
// to fail closed on malformed masks, it'd be a separate API.
func isSensitive(mask any, path []string) bool {
	// A true anywhere on the walk — including at a root or intermediate
	// node — means everything beneath is sensitive.
	if b, ok := mask.(bool); ok {
		return b
	}
	if mask == nil {
		return false
	}
	// Descended to the leaf of the path but the mask still has structure.
	// The structure itself isn't a `true`, so not sensitive.
	if len(path) == 0 {
		return false
	}

	head, tail := path[0], path[1:]
	switch m := mask.(type) {
	case map[string]any:
		child, ok := m[head]
		if !ok {
			return false
		}
		return isSensitive(child, tail)
	case []any:
		// Path step must be an integer index into the slice.
		idx, err := strconv.Atoi(head)
		if err != nil || idx < 0 || idx >= len(m) {
			return false
		}
		return isSensitive(m[idx], tail)
	default:
		// Malformed mask type (e.g. a stray string). Conservative default:
		// we can't say anything meaningful, so don't claim sensitive.
		return false
	}
}
