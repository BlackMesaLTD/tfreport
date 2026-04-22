package preserve

import (
	"fmt"
	"strings"
)

// Reconcile walks rendered and, for every preserve region whose id also
// appears in prior, replaces the region body with the kind-specific
// merge of the two. Regions present only in rendered pass through with
// their default body. Regions present only in prior are silently dropped
// (the current render doesn't ask for them).
//
// Reconcile is idempotent: running it twice with the same prior
// produces the same output as running it once.
func Reconcile(rendered string, prior map[string]Region) (string, error) {
	if len(prior) == 0 {
		// Fast path: no prior regions means no merging to do. We still
		// need to verify that the rendered output is parseable so that
		// downstream consumers don't see malformed markers.
		if _, err := ParseRegions(rendered); err != nil {
			return "", err
		}
		return rendered, nil
	}

	current, err := ParseRegions(rendered)
	if err != nil {
		return "", fmt.Errorf("reconcile: parsing rendered output: %w", err)
	}

	if len(current) == 0 {
		return rendered, nil
	}

	// Build ordered slice so we can splice from the back and keep offsets valid.
	regions := make([]Region, 0, len(current))
	for _, r := range current {
		regions = append(regions, r)
	}
	// Sort by Start descending so splices don't shift earlier offsets.
	for i := 1; i < len(regions); i++ {
		for j := i; j > 0 && regions[j-1].Start < regions[j].Start; j-- {
			regions[j-1], regions[j] = regions[j], regions[j-1]
		}
	}

	var b strings.Builder
	b.Grow(len(rendered))
	b.WriteString(rendered)
	out := b.String()

	for _, reg := range regions {
		priorReg, ok := prior[reg.ID]
		if !ok {
			continue
		}
		kind := Resolve(reg.Kind)
		merged := kind.Merge(priorReg, reg)
		if merged == reg.Body {
			continue
		}
		// Splice: keep the begin/end markers intact, replace just the body.
		bodyStart, bodyEnd := findBodyRange(out, reg)
		out = out[:bodyStart] + merged + out[bodyEnd:]
	}
	return out, nil
}

// findBodyRange locates the body offsets for reg in text. The region was
// parsed from text; we re-discover the exact body boundaries by scanning
// for the begin/end markers at reg.Start.
func findBodyRange(text string, reg Region) (int, int) {
	// reg.Start points to the begin marker; reg.End points past the end marker.
	// The body sits between the first `-->` after reg.Start and the
	// `<!-- tfreport:preserve-end` before reg.End.
	beginSlice := text[reg.Start:reg.End]
	// Find the end of the begin marker.
	beginEnd := strings.Index(beginSlice, "-->")
	if beginEnd < 0 {
		return reg.Start, reg.Start
	}
	beginEnd += len("-->")
	// Find the start of the end marker.
	endStart := strings.LastIndex(beginSlice, "<!-- tfreport:preserve-end")
	if endStart < 0 {
		return reg.Start + beginEnd, reg.Start + beginEnd
	}
	return reg.Start + beginEnd, reg.Start + endStart
}
