package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// TextPlan renders native terraform plan text blocks, budget-aware.
// Args:
//
//	addresses csv  — restrict to these resource addresses; empty → all
//	fence     str  — override ctx.Output.CodeFormat for this call only
//
// Budget behavior: consumes bytes from ctx.TextBudget. When the remaining
// budget is insufficient, truncates at the last newline boundary and appends
// a "# ... truncated" marker. Once exhausted, returns empty string (caller
// can fall back to synthetic diff).
type TextPlan struct{}

func (TextPlan) Name() string { return "text_plan" }

func (TextPlan) Render(ctx *BlockContext, args map[string]any) (string, error) {
	filter := ArgCSV(args, "addresses")
	fenceOverride := ArgString(args, "fence", "")

	block := collectTextBlocks(ctx, filter)
	if block == "" {
		return "", nil
	}

	fence := codeFence(ctx)
	if fenceOverride != "" {
		fence = fmt.Sprintf("```%s", fenceOverride)
	}

	// Apply diff-format conversion if requested.
	wantDiff := fenceOverride == "diff" || (fenceOverride == "" && ctx.Output.CodeFormat == "diff")
	if wantDiff {
		block = core.TextToDiff(block)
	}
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}

	if ctx.TextBudget == nil || ctx.TextBudget.Remaining >= len(block) {
		if ctx.TextBudget != nil {
			ctx.TextBudget.Remaining -= len(block)
		}
		return fmt.Sprintf("%s\n%s%s", fence, block, "```"), nil
	}

	if ctx.TextBudget.Remaining <= 0 {
		return "", nil
	}
	truncated := block[:ctx.TextBudget.Remaining]
	if lastNL := strings.LastIndex(truncated, "\n"); lastNL > 0 {
		truncated = truncated[:lastNL+1]
	}
	ctx.TextBudget.Remaining = 0
	return fmt.Sprintf("%s\n%s\n# ... truncated (output size limit)\n```", fence, truncated), nil
}

// collectTextBlocks concatenates text-plan blocks for addresses that appear
// in the optional filter (empty filter == all addresses in the report).
func collectTextBlocks(ctx *BlockContext, filter []string) string {
	r := currentReport(ctx)
	if r == nil || len(r.TextPlanBlocks) == 0 {
		return ""
	}

	// No filter → concatenate every block in deterministic (resource-address)
	// order. With filter → only named addresses, in the order given.
	var addrs []string
	if len(filter) == 0 {
		for _, mg := range r.ModuleGroups {
			for _, rc := range mg.Changes {
				if _, ok := r.TextPlanBlocks[rc.Address]; ok {
					addrs = append(addrs, rc.Address)
				}
			}
		}
	} else {
		addrs = filter
	}

	var parts []string
	for _, a := range addrs {
		if block, ok := r.TextPlanBlocks[a]; ok {
			parts = append(parts, block)
		}
	}
	return strings.Join(parts, "\n")
}

// Doc describes text_plan for cmd/docgen.
func (TextPlan) Doc() BlockDoc {
	return BlockDoc{
		Name:    "text_plan",
		Summary: "Native terraform plan text block, budget-aware. Truncates at newline boundaries when ctx.TextBudget would be exceeded.",
		Args: []ArgDoc{
			{Name: "addresses", Type: "csv", Default: "(all resources in report)", Description: "Restrict to these resource addresses; empty renders every address with a text block."},
			{Name: "fence", Type: "string", Default: "(from ctx.Output.CodeFormat)", Description: "Override code fence language: `diff`, `hcl`, `terraform`, or any other for plain."},
		},
	}
}

func init() { defaultRegistry.Register(TextPlan{}) }
