package preserve

import (
	"regexp"
	"slices"
	"strings"
)

// Kind is the per-kind policy for merging a prior region body with the
// current (default) one during reconciliation. Each kind decides whether
// the prior content is valid and either returns the preserved bytes or
// falls back to the current default.
type Kind interface {
	// Name identifies the kind in wire-format `kind="..."` attributes.
	Name() string
	// Merge returns the body that should appear between the preserve
	// markers after reconciliation. `prior` is the body from the
	// previously-rendered document; `current` is the freshly-rendered
	// default body. Either may be empty. Implementations must be pure
	// (deterministic, no side effects).
	Merge(prior Region, current Region) string
}

// Resolve returns the Kind implementation for the given name. Unknown
// names fall back to blockKind (opaque pass-through) so callers aren't
// required to error on kind strings they don't recognise.
func Resolve(name string) Kind {
	switch name {
	case "checkbox":
		return checkboxKind{}
	case "radio":
		return radioKind{}
	case "text":
		return textKind{}
	case "block", "":
		return blockKind{}
	default:
		return blockKind{}
	}
}

// KnownKinds lists every recognised kind name. Used by validators and
// docs to enumerate the supported set.
func KnownKinds() []string {
	return []string{"checkbox", "radio", "text", "block"}
}

// IsKnownKind reports whether name is a built-in kind.
func IsKnownKind(name string) bool {
	return slices.Contains(KnownKinds(), name)
}

// --- checkbox ---------------------------------------------------------------

type checkboxKind struct{}

func (checkboxKind) Name() string { return "checkbox" }

var checkboxValueRE = regexp.MustCompile(`\[[xX ]\]`)

// Merge extracts the tick state from prior (space or x) and substitutes it
// into current.Body. Only the tick character is human-owned; the surrounding
// structure (list marker, brackets, whitespace) belongs to the current render
// so user edits that would break GFM task-list detection revert on regenerate.
//
// Prior must contain exactly one `[x]`, `[X]`, or `[ ]` token; `[X]` normalises
// to `[x]`. Anything else (zero, multiple, garbage) falls back to current.Body.
func (checkboxKind) Merge(prior Region, current Region) string {
	matches := checkboxValueRE.FindAllString(prior.Body, -1)
	if len(matches) != 1 {
		return current.Body
	}
	tok := matches[0]
	if tok == "[X]" {
		tok = "[x]"
	}
	return strings.Replace(current.Body, "[ ]", tok, 1)
}

// --- radio ------------------------------------------------------------------

type radioKind struct{}

func (radioKind) Name() string { return "radio" }

var radioLineRE = regexp.MustCompile(`(?m)^(\s*-\s*)\[([xX ])\]\s+(.+?)\s*$`)

// Merge carries over the prior ticked option ONLY if it's still present
// in the current block's option list. If the picked option vanished
// between runs the block resets to all-unticked — forcing a re-pick is
// safer than silently moving the tick.
func (radioKind) Merge(prior Region, current Region) string {
	picked := firstTickedOption(prior.Body)
	if picked == "" {
		return current.Body
	}
	// Rewrite current body: tick the line whose label matches picked,
	// untick everything else.
	return radioLineRE.ReplaceAllStringFunc(current.Body, func(line string) string {
		sub := radioLineRE.FindStringSubmatch(line)
		if sub == nil {
			return line
		}
		prefix, label := sub[1], sub[3]
		tick := " "
		if strings.TrimSpace(label) == picked {
			tick = "x"
		}
		return prefix + "[" + tick + "] " + label
	})
}

// firstTickedOption returns the label of the first `[x]`/`[X]` line in
// body, or empty if none matched.
func firstTickedOption(body string) string {
	for _, m := range radioLineRE.FindAllStringSubmatch(body, -1) {
		if m[2] == "x" || m[2] == "X" {
			return strings.TrimSpace(m[3])
		}
	}
	return ""
}

// --- text -------------------------------------------------------------------

type textKind struct{}

func (textKind) Name() string { return "text" }

// Merge always returns the prior body verbatim. Text regions are
// opaque strings owned entirely by the human.
func (textKind) Merge(prior Region, current Region) string {
	return prior.Body
}

// --- block ------------------------------------------------------------------

type blockKind struct{}

func (blockKind) Name() string { return "block" }

// Merge always returns the prior body verbatim. Block regions are
// arbitrary multi-line content owned entirely by the human.
func (blockKind) Merge(prior Region, current Region) string {
	return prior.Body
}
