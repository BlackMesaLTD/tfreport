package blocks

import (
	"fmt"

	"github.com/tfreport/tfreport/internal/core"
)

// Title renders the report header. Grammar per target:
//   - markdown:            "# Terraform Plan Report"
//   - github-pr-body:      "## Infrastructure Change Summary"
//   - github-pr-comment:   "### Terraform Plan — N resources"  (N subs when multi)
//   - github-step-summary: "### <emoji> Terraform Plan detected changes to infrastructure."
type Title struct{}

func (Title) Name() string { return "title" }

func (Title) Render(ctx *BlockContext, _ map[string]any) (string, error) {
	reports := allReports(ctx)
	total := totalResources(ctx)

	switch ctx.Target {
	case "markdown":
		return "# Terraform Plan Report", nil

	case "github-pr-body":
		return "## Infrastructure Change Summary", nil

	case "github-pr-comment":
		if len(reports) > 1 {
			return fmt.Sprintf("### Terraform Plan — %d subscriptions, %d resources", len(reports), total), nil
		}
		return fmt.Sprintf("### Terraform Plan — %d resources", total), nil

	case "github-step-summary":
		impact := maxImpactAcross(reports)
		return fmt.Sprintf("### %s Terraform Plan detected changes to infrastructure.", overallEmoji(impact)), nil

	default:
		return "# Terraform Plan Report", nil
	}
}

// maxImpactAcross returns the highest impact across all reports.
func maxImpactAcross(reports []*core.Report) core.Impact {
	var max core.Impact
	for _, r := range reports {
		if core.ImpactSeverity(r.MaxImpact) > core.ImpactSeverity(max) {
			max = r.MaxImpact
		}
	}
	return max
}

func init() { defaultRegistry.Register(Title{}) }
