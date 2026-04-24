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
//
// Internally collects entries via a PlanTree query over KeyChange nodes
// with a legacy ModuleGroups-free fallback for contexts without a tree.
// Output is byte-exact identical in both paths.
type KeyChanges struct{}

func (KeyChanges) Name() string { return "key_changes" }

func (KeyChanges) Render(ctx *BlockContext, args map[string]any) (string, error) {
	max := ArgInt(args, "max", 0)
	impactFilter := parseImpactFilter(ArgCSV(args, "impact"))

	all := collectKeyChanges(ctx)
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

// collectKeyChanges pulls every KeyChange from the current context.
// Tree-first: Query("key_change") walks every Report subtree in order,
// emitting KeyChange nodes in their definition order within each report.
// Falls back to the legacy allReports() loop when no tree is bound —
// keeps unit-test contexts and any future non-TemplateFormatter callers
// working byte-exact.
func collectKeyChanges(ctx *BlockContext) []core.KeyChange {
	if ctx.Tree != nil && ctx.Tree.Root != nil {
		return collectKeyChangesFromTree(ctx.Tree)
	}
	return collectKeyChangesFromReports(ctx)
}

func collectKeyChangesFromTree(tree *core.PlanTree) []core.KeyChange {
	nodes := core.Query(tree.Root, core.Path{core.KindKeyChange})
	out := make([]core.KeyChange, 0, len(nodes))
	for _, n := range nodes {
		kc, ok := n.Payload.(*core.KeyChange)
		if !ok || kc == nil {
			continue
		}
		out = append(out, *kc)
	}
	return out
}

func collectKeyChangesFromReports(ctx *BlockContext) []core.KeyChange {
	var all []core.KeyChange
	for _, r := range allReports(ctx) {
		all = append(all, r.KeyChanges...)
	}
	return all
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
