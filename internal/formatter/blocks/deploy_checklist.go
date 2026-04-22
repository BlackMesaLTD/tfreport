package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
	"github.com/BlackMesaLTD/tfreport/internal/preserve"
)

// DeployChecklist renders a GitHub task-list — one checkbox per report.
// Only meaningful in multi-report mode; degenerates to a single item in
// single-report mode.
//
// Args:
//
//	columns csv (default "subscription,impact,actions")
//	    Columns to render in each checklist line. Available:
//	      subscription — label (or "default")
//	      impact       — r.MaxImpact
//	      actions      — action-summary line ("1 create, 2 update, 1 delete")
//	      key_changes_count — number of key_changes entries
//
//	preserve bool (default false)
//	    Opt-in: wrap each row's checkbox in a preserve region so ticks
//	    survive PR re-renders. Id derived from the report label via
//	    preserve.SlugifyID, namespaced as `deploy:<slug>`. Auto-downgraded
//	    to false when ctx.PriorRegions is nil (no --previous-body-file
//	    supplied) to avoid comment-marker cruft in one-off renders.
type DeployChecklist struct{}

func (DeployChecklist) Name() string { return "deploy_checklist" }

var deployChecklistColumns = []string{"subscription", "impact", "actions", "key_changes_count"}
var deployChecklistHeadings = map[string]string{
	"subscription":       "Subscription",
	"impact":             "Impact",
	"actions":            "Actions",
	"key_changes_count":  "Key Changes",
}

func (DeployChecklist) Render(ctx *BlockContext, args map[string]any) (string, error) {
	cols := defaultCols(ArgCSV(args, "columns"), []string{"subscription", "impact", "actions"})
	if err := validateColumns("deploy_checklist", cols, toSet(deployChecklistColumns)); err != nil {
		return "", err
	}

	reports := allReports(ctx)
	if len(reports) == 0 {
		return "", nil
	}

	// Preservation is opt-in. When enabled but no prior body was supplied
	// (ctx.PriorRegions is nil), silently fall back to the raw-[ ] emission —
	// avoids cruft from markers that will never get reconciled.
	preserveOn := ArgBool(args, "preserve", false) && ctx.PriorRegions != nil

	var b strings.Builder
	b.WriteString("### Deploy Checklist\n")
	for _, r := range reports {
		if preserveOn {
			// GFM task-list detection needs `- [ ] ` (with trailing space)
			// contiguous at the start of the line. Emit the begin-marker on
			// its own line above so the whole token sits inside the region
			// body — user edits to the dash/brackets/space revert on re-render,
			// only the tick character is preserved.
			id := "deploy:" + preserve.SlugifyID(reportLabel(r))
			begin, err := preserve.RenderBegin(id, "checkbox", nil)
			if err != nil {
				return "", fmt.Errorf("deploy_checklist: %w", err)
			}
			end, err := preserve.RenderEnd(id)
			if err != nil {
				return "", fmt.Errorf("deploy_checklist: %w", err)
			}
			b.WriteString(begin)
			b.WriteString("\n- [ ] ")
			b.WriteString(end)
		} else {
			b.WriteString("- [ ] ")
		}
		for i, col := range cols {
			if i > 0 {
				b.WriteString(deployChecklistSeparator(col))
			}
			b.WriteString(renderDeployChecklistCell(r, col))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func renderDeployChecklistCell(r *core.Report, col string) string {
	switch col {
	case "subscription":
		return fmt.Sprintf("**%s**", reportLabel(r))
	case "impact":
		return fmt.Sprintf("(%s)", r.MaxImpact)
	case "actions":
		return actionSummaryLine(r.ActionCounts)
	case "key_changes_count":
		return fmt.Sprintf("%d key changes", len(r.KeyChanges))
	}
	return ""
}

// deployChecklistSeparator returns the separator used before each column
// position after the first. Preserves the existing "**label** (impact) — actions"
// grammar by using space before `(impact)` and " — " before actions.
func deployChecklistSeparator(col string) string {
	switch col {
	case "impact":
		return " "
	case "actions", "key_changes_count":
		return " — "
	}
	return " · "
}

// Doc describes deploy_checklist for cmd/docgen.
func (DeployChecklist) Doc() BlockDoc {
	return BlockDoc{
		Name:    "deploy_checklist",
		Summary: "GitHub task-list checkboxes, one per report. Degenerates to a single item in single-report mode.",
		Args: []ArgDoc{
			{Name: "columns", Type: "csv", Default: "subscription,impact,actions", Description: "Columns rendered per checklist line. Order is preserved; separators adapt to column identity."},
			{Name: "preserve", Type: "bool", Default: "false", Description: "Wrap each row's checkbox in a preserve region so ticks survive PR re-renders. Id is `deploy:<slugified label>`. Silently downgrades to false when tfreport is invoked without `--previous-body-file`. See docs/state-preservation.md."},
		},
		Columns: []ColumnDoc{
			{ID: "subscription", Heading: "Subscription", Description: "Report label (or `default`) in bold."},
			{ID: "impact", Heading: "Impact", Description: "`(r.MaxImpact)` in parentheses."},
			{ID: "actions", Heading: "Actions", Description: "Action-summary line (e.g. `1 create, 2 update, 1 delete`)."},
			{ID: "key_changes_count", Heading: "Key Changes", Description: "`N key changes` where N = len(r.KeyChanges)."},
		},
	}
}

func init() { defaultRegistry.Register(DeployChecklist{}) }
