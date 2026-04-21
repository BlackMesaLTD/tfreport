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

// Doc describes plan_counts for cmd/docgen.
func (PlanCounts) Doc() BlockDoc {
	return BlockDoc{
		Name:    "plan_counts",
		Summary: "Terraform-style verb summary (`Plan: 1 to add, 2 to change, 1 to destroy.`). Target-agnostic. Empty plan renders `No changes detected.`",
	}
}

func init() { defaultRegistry.Register(PlanCounts{}) }
