package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// KeyChanges renders the plain-English key-changes bullet list. Args:
//
//	max    int  — cap number of bullets (default 0 = unlimited)
//	impact csv  — filter: keep only entries whose Impact is in the set
//	              (e.g. "critical,high"). Empty = keep all.
//
// Grammar per target:
//   - markdown: "## Key Changes\n\n- a\n- b\n"
//   - github-pr-body: "**Key changes:**\n- a\n- b"
//   - github-pr-comment: "- a\n- b" (no header; context is the <details>)
//   - github-step-summary: "- a\n- b" (caller may wrap in a <details>)
type KeyChanges struct{}

func (KeyChanges) Name() string { return "key_changes" }

func (KeyChanges) Render(ctx *BlockContext, args map[string]any) (string, error) {
	max := ArgInt(args, "max", 0)
	impactFilter := parseImpactFilter(ArgCSV(args, "impact"))

	var all []core.KeyChange
	for _, r := range allReports(ctx) {
		all = append(all, r.KeyChanges...)
	}
	if impactFilter != nil {
		filtered := all[:0:0]
		for _, kc := range all {
			if _, ok := impactFilter[kc.Impact]; ok {
				filtered = append(filtered, kc)
			}
		}
		all = filtered
	}
	if len(all) == 0 {
		return "", nil
	}

	truncated := false
	total := len(all)
	if max > 0 && len(all) > max {
		all = all[:max]
		truncated = true
	}

	var b strings.Builder
	switch ctx.Target {
	case "markdown":
		fmt.Fprintf(&b, "## Key Changes\n\n")
	case "github-pr-body":
		fmt.Fprintf(&b, "**Key changes:**\n")
	}

	for _, kc := range all {
		fmt.Fprintf(&b, "- %s\n", kc.Text)
	}
	if truncated {
		fmt.Fprintf(&b, "- _... %d more changes_\n", total-max)
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// parseImpactFilter turns a csv like "critical,high" into a set of Impact
// values. Returns nil when input is empty (meaning "keep all").
func parseImpactFilter(csv []string) map[core.Impact]struct{} {
	if len(csv) == 0 {
		return nil
	}
	out := make(map[core.Impact]struct{}, len(csv))
	for _, name := range csv {
		out[core.Impact(name)] = struct{}{}
	}
	return out
}

// Doc describes key_changes for cmd/docgen.
func (KeyChanges) Doc() BlockDoc {
	return BlockDoc{
		Name:    "key_changes",
		Summary: "Plain-English summary bullets, impact-tagged, with optional filter and truncation.",
		Args: []ArgDoc{
			{Name: "max", Type: "int", Default: "0 (no limit)", Description: "Cap number of bullets; extras collapse into a `… N more changes` line."},
			{Name: "impact", Type: "csv", Default: "(all)", Description: "Filter: keep only entries whose Impact is in the csv set (e.g. `critical,high`)."},
		},
	}
}

func init() { defaultRegistry.Register(KeyChanges{}) }
