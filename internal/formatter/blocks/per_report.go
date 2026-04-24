package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// PerReport renders a single "report card" — the declarative replacement
// for the hand-rolled `{{ range $r := .Reports }} <details>…</details>
// {{ end }}` pattern that every *.multi.tmpl default used to carry.
//
// Grammar is picked from ctx.Target:
//
//   - markdown            → "## {label}\n\n**N resources**\n\n{contents}"
//   - github-pr-body      → "<details><summary>{label} — N resources</summary>\n\n{contents}\n\n</details>"
//     (key_changes contents include their own "**Key changes:**" header via the
//     key_changes block's pr-body grammar.)
//   - github-pr-comment   → "<details><summary>{label} — N resources</summary>\n\n{contents}\n\n</details>"
//   - github-step-summary → "<details><summary>{label} — N resources, {impact} impact</summary>\n\n{contents}\n\n</details>"
//
// Args:
//
//	report *core.Report (required)
//	    The report to render. Callers pass `$r` inside
//	    `{{ range $r := .Reports }}{{ per_report "report" $r }}{{ end }}`.
//
//	show csv (default "key_changes")
//	    Inner block names to compose. Supported: key_changes, module_details,
//	    text_plan, instance_detail. Unknown names return a typed error.
//
//	    NOTE: summary_table and changed_resources_table are scheduled for
//	    removal in v0.3.0. If you previously used
//	    `show="changed_resources_table"` or `show="summary_table"` with
//	    per_report, author the inner content as a `{{ table ... }}` call
//	    in a regular template instead — per_report was never able to pass
//	    args into its inner blocks, so the legacy names always rendered
//	    with defaults anyway.
//
//	collapse bool (default: target uses canCollapse)
//	    Force wrap/unwrap in <details>. Rarely set by users.
//
// per_report intentionally does NOT include title / plan_counts / footer —
// those are top-level chrome, rendered once per template, not per report.
type PerReport struct{}

func (PerReport) Name() string { return "per_report" }

// perReportValidShow is the allowlist for the `show=` arg. Narrower than
// the full block registry on purpose — composing chrome blocks
// (title/footer/plan_counts) or zero-arg-failing blocks (table, banner)
// inside a per-report card produces broken output. Users who need those
// should author a full template with explicit block calls.
//
// summary_table and changed_resources_table were formerly accepted here;
// both are scheduled for removal in v0.3.0. Their entries were pulled
// so the allowlist doesn't advertise blocks we intend to delete.
var perReportValidShow = map[string]bool{
	"key_changes":     true,
	"module_details":  true,
	"text_plan":       true,
	"instance_detail": true,
}

func (PerReport) Render(ctx *BlockContext, args map[string]any) (string, error) {
	v, ok := args["report"]
	if !ok || v == nil {
		return "", fmt.Errorf("per_report: 'report' arg is required (pass `$r` from `range .Reports`)")
	}
	r, ok := v.(*core.Report)
	if !ok {
		return "", fmt.Errorf("per_report: 'report' arg must be a *core.Report, got %T", v)
	}
	if r == nil {
		return "", nil
	}

	show := ArgCSV(args, "show")
	if len(show) == 0 {
		show = []string{"key_changes"}
	}
	for _, name := range show {
		if !perReportValidShow[name] {
			return "", fmt.Errorf("per_report: unknown show item %q (valid: instance_detail, key_changes, module_details, text_plan)", name)
		}
	}

	collapse := ArgBool(args, "collapse", canCollapse(ctx.Target))

	// Scope context to just this report so inner blocks operate on it.
	inner := *ctx
	inner.Report = r
	inner.Reports = nil

	contents, err := perReportContents(&inner, show)
	if err != nil {
		return "", err
	}
	contents = strings.TrimSpace(contents)

	label := r.Label
	if label == "" {
		label = "default"
	}

	var b strings.Builder

	if collapse {
		summary := fmt.Sprintf("%s — %d resources", label, r.TotalResources)
		if ctx.Target == "github-step-summary" && r.MaxImpact != "" {
			summary = fmt.Sprintf("%s, %s impact", summary, r.MaxImpact)
		}
		fmt.Fprintf(&b, "<details><summary>%s</summary>\n\n", summary)
		if contents != "" {
			b.WriteString(contents)
			b.WriteString("\n\n")
		}
		b.WriteString("</details>")
	} else {
		fmt.Fprintf(&b, "## %s\n\n", label)
		fmt.Fprintf(&b, "**%d resources**", r.TotalResources)
		if contents != "" {
			b.WriteString("\n\n")
			b.WriteString(contents)
		}
	}

	return b.String(), nil
}

// perReportContents composes inner block output in show-list order.
// key_changes in markdown context emits raw bullets (no "## Key Changes"
// heading) because we're already inside a per-report H2.
func perReportContents(ctx *BlockContext, show []string) (string, error) {
	var parts []string
	for _, name := range show {
		var chunk string
		var err error

		if name == "key_changes" && ctx.Target == "markdown" {
			chunk = renderRawKeyChangesBullets(ctx)
		} else {
			chunk, err = defaultRegistry.Render(name, ctx, nil)
			if err != nil {
				return "", fmt.Errorf("per_report: rendering %q: %w", name, err)
			}
		}

		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			parts = append(parts, chunk)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// renderRawKeyChangesBullets emits key_changes as a plain bullet list with no
// section header, matching the hand-rolled behavior of the pre-migration
// markdown.multi.tmpl default. Used only by PerReport on markdown target.
func renderRawKeyChangesBullets(ctx *BlockContext) string {
	r := currentReport(ctx)
	if r == nil || len(r.KeyChanges) == 0 {
		return ""
	}
	var b strings.Builder
	for _, kc := range r.KeyChanges {
		fmt.Fprintf(&b, "- %s\n", kc.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Doc describes per_report for cmd/docgen.
func (PerReport) Doc() BlockDoc {
	return BlockDoc{
		Name:    "per_report",
		Summary: "One 'report card' section for a single report. Declarative replacement for the {{ range .Reports }}<details>…{{ end }} loop in multi-report templates. Never wraps title/plan_counts/footer (those are top-level chrome).",
		Args: []ArgDoc{
			{Name: "report", Type: "*core.Report", Default: "—", Description: "Required. The report to render; pass `$r` from range .Reports."},
			{Name: "show", Type: "csv", Default: "key_changes", Description: "Inner blocks to compose: key_changes, module_details, text_plan, instance_detail."},
			{Name: "collapse", Type: "bool", Default: "(target uses <details>)", Description: "Force wrap/unwrap in <details>. Rarely set explicitly."},
		},
	}
}

func init() { defaultRegistry.Register(PerReport{}) }
