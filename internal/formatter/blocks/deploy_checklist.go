package blocks

import (
	"fmt"
	"strings"
)

// DeployChecklist renders a GitHub checklist per subscription — one task box
// per report. Only meaningful in multi-report mode; degenerates to a single
// item in single-report mode.
type DeployChecklist struct{}

func (DeployChecklist) Name() string { return "deploy_checklist" }

func (DeployChecklist) Render(ctx *BlockContext, _ map[string]any) (string, error) {
	reports := allReports(ctx)
	if len(reports) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("### Deploy Checklist\n")
	for _, r := range reports {
		fmt.Fprintf(&b, "- [ ] **%s** (%s) — %s\n",
			reportLabel(r), r.MaxImpact, actionSummaryLine(r.ActionCounts))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func init() { defaultRegistry.Register(DeployChecklist{}) }
