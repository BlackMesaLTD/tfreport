// Package preserve implements round-trippable user-editable regions in
// rendered output.
//
// A region is a sub-line or multi-line span whose contents belong to the
// human reader once they've touched it: ticks on a GitHub task-list
// checkbox, a picked option in a radio group, a free-text reviewer note,
// or any opaque block. On regeneration tfreport preserves the prior
// content for each region keyed by a stable `id`, so pushes to a PR do
// not wipe human decisions recorded in the PR body or a sticky comment.
//
// Wire format — HTML comments, invisible in rendered markdown:
//
//	<!-- tfreport:preserve-begin id="<id>" kind="<kind>" [attr="val"]... -->
//	<body>
//	<!-- tfreport:preserve-end id="<id>" -->
//
// The begin marker carries kind-specific attributes (e.g. `options="a,b,c"`
// for radio). The end marker echoes `id` only, providing a parse-time guard
// against unbalanced nesting.
package preserve

import (
	"fmt"
	"regexp"
	"strings"
)

// Region is one parsed `preserve` span from a rendered document.
type Region struct {
	// ID is the stable key used to match prior → current on regeneration.
	ID string
	// Kind drives the per-kind merger when reconciling.
	Kind string
	// Attrs are the kind-specific parameters from the begin marker (e.g.
	// `options="a,b"` for radio). Empty map if none.
	Attrs map[string]string
	// Body is the bytes between the begin and end markers, verbatim.
	Body string
	// Start is the byte offset of the begin marker in the source document.
	Start int
	// End is the byte offset just past the end marker.
	End int
}

// Valid ID charset: alphanumerics, dot, underscore, hyphen, colon. Colon
// allows namespaced ids like `deploy:sub-alpha`, `approver:prod`.
var idCharRE = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// ValidateID reports whether id is safe to embed in a preserve marker.
// Returns a non-nil error explaining the first disallowed character.
func ValidateID(id string) error {
	if id == "" {
		return fmt.Errorf("preserve id must not be empty")
	}
	if !idCharRE.MatchString(id) {
		return fmt.Errorf("preserve id %q: only [A-Za-z0-9._:-] allowed", id)
	}
	return nil
}

// SlugifyID replaces any character outside the valid id charset with a
// hyphen. Empty input yields the empty string (callers should guard).
// Use this when deriving an id from user-supplied labels.
func SlugifyID(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-', r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// Match one begin marker; capture raw attribute list.
var beginRE = regexp.MustCompile(`<!--\s*tfreport:preserve-begin\s+([^>]*?)\s*-->`)

// Match one end marker; capture id attribute.
var endRE = regexp.MustCompile(`<!--\s*tfreport:preserve-end\s+id="([^"]*)"\s*-->`)

// Match a single key="value" attribute pair.
var attrRE = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_-]*)="([^"]*)"`)

// ParseRegions extracts every preserve region from text, keyed by id.
// Duplicate ids in a single document are reported as an error — callers
// authoring templates should namespace ids (`deploy:<sub>`, `note:<sub>`)
// to avoid collisions.
//
// Markers that open without a matching close, or close without a matching
// open, produce an error naming the offending id and its byte offset.
func ParseRegions(text string) (map[string]Region, error) {
	out := make(map[string]Region)

	pos := 0
	for pos < len(text) {
		bLoc := beginRE.FindStringSubmatchIndex(text[pos:])
		if bLoc == nil {
			break
		}
		beginStart := pos + bLoc[0]
		beginEnd := pos + bLoc[1]
		attrText := text[pos+bLoc[2] : pos+bLoc[3]]

		attrs := parseAttrs(attrText)
		id := attrs["id"]
		if id == "" {
			return nil, fmt.Errorf("preserve-begin marker at byte %d: missing id attribute", beginStart)
		}
		if err := ValidateID(id); err != nil {
			return nil, fmt.Errorf("preserve-begin marker at byte %d: %w", beginStart, err)
		}
		kind := attrs["kind"]
		delete(attrs, "id")
		delete(attrs, "kind")

		// Find matching end tag AFTER the begin.
		eLoc := endRE.FindStringSubmatchIndex(text[beginEnd:])
		if eLoc == nil {
			return nil, fmt.Errorf("preserve-begin id=%q at byte %d: no matching preserve-end", id, beginStart)
		}
		endID := text[beginEnd+eLoc[2] : beginEnd+eLoc[3]]
		if endID != id {
			return nil, fmt.Errorf("preserve-begin id=%q at byte %d: next preserve-end has id=%q (nesting not supported)", id, beginStart, endID)
		}

		bodyStart := beginEnd
		bodyEnd := beginEnd + eLoc[0]
		regionEnd := beginEnd + eLoc[1]

		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("preserve: duplicate id=%q (second occurrence at byte %d)", id, beginStart)
		}

		out[id] = Region{
			ID:    id,
			Kind:  kind,
			Attrs: attrs,
			Body:  text[bodyStart:bodyEnd],
			Start: beginStart,
			End:   regionEnd,
		}
		pos = regionEnd
	}

	// Detect orphan end markers that come before a matching begin, or after
	// everything was consumed but a stray end appears.
	if stray := endRE.FindStringIndex(text[pos:]); stray != nil {
		offset := pos + stray[0]
		sub := endRE.FindStringSubmatch(text[offset:])
		return nil, fmt.Errorf("preserve-end id=%q at byte %d: no matching preserve-begin", sub[1], offset)
	}

	return out, nil
}

// parseAttrs pulls key="value" pairs out of a begin-tag attribute list.
// Unrecognised syntax is silently ignored; the parser is lenient so that
// authors editing prior bodies by hand don't wreck regeneration.
func parseAttrs(s string) map[string]string {
	out := make(map[string]string)
	for _, m := range attrRE.FindAllStringSubmatch(s, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// RenderBegin builds a `<!-- tfreport:preserve-begin ... -->` marker with
// id, kind, and any kind-specific attrs. Attribute order is stable
// (id → kind → extras in sorted order) so golden file comparisons are
// deterministic.
func RenderBegin(id, kind string, attrs map[string]string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!-- tfreport:preserve-begin id="`)
	b.WriteString(id)
	b.WriteString(`"`)
	if kind != "" {
		b.WriteString(` kind="`)
		b.WriteString(kind)
		b.WriteString(`"`)
	}
	keys := sortedKeys(attrs)
	for _, k := range keys {
		if k == "id" || k == "kind" {
			continue
		}
		b.WriteString(` `)
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(attrs[k])
		b.WriteString(`"`)
	}
	b.WriteString(` -->`)
	return b.String(), nil
}

// RenderEnd builds a `<!-- tfreport:preserve-end id="..." -->` marker.
func RenderEnd(id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	return `<!-- tfreport:preserve-end id="` + id + `" -->`, nil
}

// sortedKeys returns map keys in sorted order for deterministic output.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort (small n)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
