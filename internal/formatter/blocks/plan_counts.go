package blocks

import "fmt"

// PlanCounts renders the "Plan: N to add, N to change, ..." line.
// Target-agnostic — same output across all targets.
type PlanCounts struct{}

func (PlanCounts) Name() string { return "plan_counts" }

func (PlanCounts) Render(ctx *BlockContext, _ map[string]any) (string, error) {
	r := currentReport(ctx)
	if r == nil || r.TotalResources == 0 {
		return "No changes detected.", nil
	}
	return fmt.Sprintf("Plan: %s.", planCountsLine(r.ActionCounts)), nil
}

func init() { defaultRegistry.Register(PlanCounts{}) }
