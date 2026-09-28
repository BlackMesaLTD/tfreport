package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// RuleDelta renders the per-rule terraform-style diff that core.BuildRuleDiff
// produced for resources with a registered block set (NSG security_rule,
// route-table route, and their object-style rule resources). Where
// terraform prints a changed rule set as "remove every touched element,
// add every touched element", this pairs elements by name and shows only
// the fields that differ inside each rule, headed by a verdict comment.
//
// Args:
//
//	addresses csv — restrict to these resource addresses; empty → every
//	                resource with a rule diff
//	fence     str — override ctx.Output.CodeFormat for this call only
//
// Returns "" for resources without a rule diff, so templates can fall back
// to text_plan. Charges the shared text budget like text_plan.
type RuleDelta struct{}

func (RuleDelta) Name() string { return "rule_delta" }

func (RuleDelta) Render(ctx *BlockContext, args map[string]any) (string, error) {
	filter := ArgCSV(args, "addresses")
	fenceOverride := ArgString(args, "fence", "")

	r := currentReport(ctx)
	if r == nil || len(r.RuleDiffs) == 0 {
		return "", nil
	}
	var addrs []string
	if len(filter) == 0 {
		for _, mg := range r.ModuleGroups {
			for _, rc := range mg.Changes {
				if _, ok := r.RuleDiffs[rc.Address]; ok {
					addrs = append(addrs, rc.Address)
				}
			}
		}
	} else {
		addrs = filter
	}
	var parts []string
	for _, a := range addrs {
		if d, ok := r.RuleDiffs[a]; ok && d != "" {
			parts = append(parts, d)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	block := strings.Join(parts, "\n\n")

	fence := codeFence(ctx)
	if fenceOverride != "" {
		fence = fmt.Sprintf("```%s", fenceOverride)
	}
	if fenceOverride == "diff" || (fenceOverride == "" && ctx.Output.CodeFormat == "diff") {
		block = core.TextToDiff(block)
	}
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	return fencedBudgeted(ctx, fence, block), nil
}

// Doc describes rule_delta for cmd/docgen.
func (RuleDelta) Doc() BlockDoc {
	return BlockDoc{
		Name:    "rule_delta",
		Summary: "Per-rule terraform-style diff for block-set resources (NSG security_rule, route-table route): elements paired by name, only differing fields shown, verdict comment per rule. Empty for resources without a rule diff.",
		Args: []ArgDoc{
			{Name: "addresses", Type: "csv", Default: "(all resources with a rule diff)", Description: "Restrict to these resource addresses."},
			{Name: "fence", Type: "string", Default: "(from ctx.Output.CodeFormat)", Description: "Override code fence language: `diff`, `hcl`, `terraform`, or any other for plain."},
		},
	}
}

func init() { defaultRegistry.Register(RuleDelta{}) }
