package template

import (
	"fmt"
	"regexp"
	"strings"
)

// blockMarkerRe matches our section markers:  {{/* block: NAME */}}
// Leading/trailing whitespace on the marker line is tolerated.
var blockMarkerRe = regexp.MustCompile(`(?m)^[ \t]*\{\{\s*/\*\s*block:\s*([A-Za-z_][A-Za-z0-9_]*)\s*\*/\s*\}\}[ \t]*\n?`)

// SectionSelector describes which named sections to keep/drop in a default
// template. Exactly one of Show/Hide is used (the caller is responsible for
// validating mutex via config.validate).
type SectionSelector struct {
	Show []string
	Hide []string
}

// IsZero reports whether no filter is requested.
func (s SectionSelector) IsZero() bool { return len(s.Show) == 0 && len(s.Hide) == 0 }

// ApplySections rewrites tmplText by keeping or dropping sections according
// to the selector. Sections are delimited by `{{/* block: NAME */}}` markers
// embedded in the default templates; the marker plus every line up to (but
// not including) the next marker form a section body.
//
// Behavior:
//   - Show is an allow-list. Only sections whose NAME appears in Show are
//     retained; others are dropped entirely. Order is preserved (file order,
//     not Show order — Show is a filter, not a reorder spec).
//   - Hide is a deny-list. Every section except those in Hide is kept.
//   - Unknown names in Show/Hide are silently ignored (forward-compat: new
//     blocks land without breaking old configs).
//
// Non-section prose (text before the first marker) is always preserved.
//
// If the template contains no markers, ApplySections returns it unchanged.
func ApplySections(tmplText string, sel SectionSelector) (string, error) {
	if sel.IsZero() {
		return tmplText, nil
	}
	if len(sel.Show) > 0 && len(sel.Hide) > 0 {
		return "", fmt.Errorf("sections: show and hide are mutually exclusive")
	}

	matches := blockMarkerRe.FindAllStringSubmatchIndex(tmplText, -1)
	if len(matches) == 0 {
		return tmplText, nil
	}

	showSet := make(map[string]struct{}, len(sel.Show))
	for _, s := range sel.Show {
		showSet[s] = struct{}{}
	}
	hideSet := make(map[string]struct{}, len(sel.Hide))
	for _, s := range sel.Hide {
		hideSet[s] = struct{}{}
	}

	var b strings.Builder
	// Preamble: everything before the first marker is always kept.
	b.WriteString(tmplText[:matches[0][0]])

	for i, m := range matches {
		name := tmplText[m[2]:m[3]]
		start := m[0]
		end := len(tmplText)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		section := tmplText[start:end]

		var keep bool
		switch {
		case len(showSet) > 0:
			_, keep = showSet[name]
		case len(hideSet) > 0:
			_, hide := hideSet[name]
			keep = !hide
		default:
			keep = true
		}
		if keep {
			b.WriteString(section)
		}
	}
	return b.String(), nil
}
