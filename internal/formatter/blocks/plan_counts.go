package blocks

import (
	"fmt"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// PlanCounts renders the "Plan: N to add, N to change, ..." line.
// Target-agnostic — same output across all targets.
//
// Args:
//
//	report (*core.Report, optional)
//	    Explicit report to count. Required when rendering per-report
//	    inside `{{ range .Reports }}` — pass `$r` so the block knows
//	    which subscription's counts to render. Absent → uses the
//	    context's current report (single-report case or first of many).
//
//	include_imports (bool, default false)
//	    When true, appends ", N imported" to the output if the report
//	    contains any IsImport=true resources. Defaults off for backward
//	    compatibility with the zero-arg property form `{{ .PlanCounts }}`.
type PlanCounts struct{}

func (PlanCounts) Name() string { return "plan_counts" }

func (PlanCounts) Render(ctx *BlockContext, args map[string]any) (string, error) {
	var r *core.Report
	if v, ok := args["report"]; ok && v != nil {
		rr, ok := v.(*core.Report)
		if !ok {
			return "", fmt.Errorf("plan_counts: 'report' arg must be a *core.Report, got %T", v)
		}
		r = rr
	}
	if r == nil {
		r = currentReport(ctx)
	}
	if r == nil || r.TotalResources == 0 {
		return "No changes detected.", nil
	}

	line := planCountsLine(r.ActionCounts)
	if ArgBool(args, "include_imports", false) {
		if n := countImports(r); n > 0 {
			line = fmt.Sprintf("%s, %d imported", line, n)
		}
	}
	return fmt.Sprintf("Plan: %s.", line), nil
}

// countImports tallies IsImport=true resources in the report.
func countImports(r *core.Report) int {
	n := 0
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			if rc.IsImport {
				n++
			}
		}
	}
	return n
}

// Doc describes plan_counts for cmd/docgen.
func (PlanCounts) Doc() BlockDoc {
	return BlockDoc{
		Name:    "plan_counts",
		Summary: "Terraform-style verb summary (`Plan: 1 to add, 2 to change, 1 to destroy.`). Target-agnostic. Empty plan renders `No changes detected.`",
		Args: []ArgDoc{
			{Name: "report", Type: "*core.Report", Default: "(current report)", Description: "Explicit report to count. Required when iterating `range .Reports` in multi-report mode; pass `$r`."},
			{Name: "include_imports", Type: "bool", Default: "false", Description: "Append `, N imported` when the report has any IsImport=true resources."},
		},
	}
}

func init() { defaultRegistry.Register(PlanCounts{}) }
