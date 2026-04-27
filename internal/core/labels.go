package core

import "strings"

// LabelMarker is the trailing description sentinel that identifies a GitHub
// label as tfreport-managed. The applier uses this to filter PR labels for
// reconciliation; humans can strip it via the GitHub UI to orphan a label.
const LabelMarker = " [tfreport]"

// LabelDescriptionText is the static description body that precedes the
// marker. Matches git-labeler.py:130 verbatim for continuity.
const LabelDescriptionText = "Changes Detected in Account."

// Label color constants — GitHub wants 6-char hex with no `#`.
const (
	LabelColorRed   = "d73a4a"
	LabelColorAmber = "ffbf00"
	LabelColorGreen = "00ff00"
)

// LabelSpec is one entry in a labels manifest: the desired GitHub label
// name, color (6-char hex, no `#`), and full description (including the
// trailing LabelMarker so the applier doesn't have to synthesise it).
type LabelSpec struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// charOrder is the canonical ordering inside the bracketed prefix.
// Matches git-labeler.py:30 (`chars = ['+', '~', '-']`).
var charOrder = [...]string{"+", "~", "-"}

// DeriveLabel computes the LabelSpec for a single Report. Returns
// (spec, true) when the report carries enough state to emit a label, and
// (_, false) when emission should be skipped — namely when Report.Label is
// empty (no name to put a marker on) or when no resource change contributes
// any character (all-no-op / all-read plans).
//
// Char rule per ResourceChange:
//   - create  -> '+'
//   - update  -> '~'
//   - delete  -> '-'
//   - replace -> '+' AND '-'  (compositional: replace = delete + create)
//   - read    -> (omit)
//   - no-op   -> (omit, unless IsImport then '~')
//
// Color rule (matches git-labeler.py): any '-' wins red, any '~' wins
// amber, only '+' wins green. Empty char set => skip emission.
func DeriveLabel(r *Report) (LabelSpec, bool) {
	if r == nil || r.Label == "" {
		return LabelSpec{}, false
	}

	chars := charSet(r)
	if len(chars) == 0 {
		return LabelSpec{}, false
	}

	return LabelSpec{
		Name:        formatLabelName(chars, r.Label),
		Color:       colorForChars(chars),
		Description: LabelDescriptionText + LabelMarker,
	}, true
}

// charSet walks every ResourceChange in the report and returns the set of
// distinct chars contributed (using the rules in DeriveLabel's docstring).
func charSet(r *Report) map[string]struct{} {
	chars := make(map[string]struct{}, 3)
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			for _, c := range charsForChange(rc) {
				chars[c] = struct{}{}
			}
		}
	}
	return chars
}

// charsForChange maps one ResourceChange to its contributed char(s).
func charsForChange(rc ResourceChange) []string {
	switch rc.Action {
	case ActionCreate:
		return []string{"+"}
	case ActionUpdate:
		return []string{"~"}
	case ActionDelete:
		return []string{"-"}
	case ActionReplace:
		return []string{"+", "-"}
	case ActionRead:
		return nil
	case ActionNoOp:
		if rc.IsImport {
			return []string{"~"}
		}
		return nil
	default:
		return nil
	}
}

// formatLabelName renders the bracketed prefix in canonical char order and
// concatenates the report label. Output shape matches git-labeler.py:115:
//
//	[ + ~ - ] <label>
func formatLabelName(chars map[string]struct{}, label string) string {
	ordered := make([]string, 0, len(chars))
	for _, c := range charOrder {
		if _, ok := chars[c]; ok {
			ordered = append(ordered, c)
		}
	}
	return "[ " + strings.Join(ordered, " ") + " ] " + label
}

// colorForChars applies the worst-severity-wins rule.
func colorForChars(chars map[string]struct{}) string {
	if _, ok := chars["-"]; ok {
		return LabelColorRed
	}
	if _, ok := chars["~"]; ok {
		return LabelColorAmber
	}
	return LabelColorGreen
}
