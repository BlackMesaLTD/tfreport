package blocks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tfreport/tfreport/internal/core"
)

// FleetHomogeneity answers "are all these reports identical?" across
// multiple subscriptions. When yes, renders a unified summary + the labels
// the pattern applies to. When no, identifies the majority pattern and
// flags outliers.
//
// Single-report mode: block returns empty string (not meaningful).
//
// Args:
//
//	style       (summary | banner | table; default summary)
//	fingerprint (key_changes | action_counts; default key_changes)
type FleetHomogeneity struct{}

func (FleetHomogeneity) Name() string { return "fleet_homogeneity" }

func (FleetHomogeneity) Render(ctx *BlockContext, args map[string]any) (string, error) {
	style := ArgString(args, "style", "summary")
	fpKind := ArgString(args, "fingerprint", "key_changes")

	// Single-report mode: emit an HTML comment so the user sees a clear
	// signal in raw output / templates without breaking rendered markdown.
	// Comment is invisible in rendered GitHub output.
	if len(ctx.Reports) <= 1 {
		return "<!-- fleet_homogeneity: single-report mode, no comparison possible (block is multi-report only) -->", nil
	}

	fp := func(r *core.Report) string {
		switch fpKind {
		case "action_counts":
			return actionCountsFingerprint(r)
		case "key_changes":
			return keyChangesFingerprint(r)
		default:
			return keyChangesFingerprint(r)
		}
	}

	// Group reports by fingerprint.
	groups := map[string][]*core.Report{}
	var order []string
	for _, r := range ctx.Reports {
		f := fp(r)
		if _, ok := groups[f]; !ok {
			order = append(order, f)
		}
		groups[f] = append(groups[f], r)
	}

	// Homogeneous if one group contains everything.
	if len(order) == 1 {
		return renderHomogeneous(ctx.Reports[0], ctx.Reports, style), nil
	}

	// Divergent: find majority group and outliers.
	sort.SliceStable(order, func(i, j int) bool {
		return len(groups[order[i]]) > len(groups[order[j]])
	})
	majority := groups[order[0]]
	var outliers []*core.Report
	for _, f := range order[1:] {
		outliers = append(outliers, groups[f]...)
	}
	return renderDivergent(majority, outliers, style), nil
}

// renderHomogeneous produces output for the "all reports match" case.
func renderHomogeneous(representative *core.Report, reports []*core.Report, style string) string {
	labels := make([]string, len(reports))
	for i, r := range reports {
		labels[i] = reportLabel(r)
	}

	switch style {
	case "banner":
		return fmt.Sprintf("✅ **Fleet uniform** — all %d subscriptions show identical changes.", len(reports))

	case "table":
		var b strings.Builder
		b.WriteString("| Subscription | Status |\n")
		b.WriteString("|--------------|--------|\n")
		for _, lab := range labels {
			fmt.Fprintf(&b, "| %s | ✅ uniform |\n", lab)
		}
		return strings.TrimRight(b.String(), "\n")

	default: // summary
		var b strings.Builder
		fmt.Fprintf(&b, "✅ **Fleet uniform** — all %d subscriptions show identical changes:\n\n", len(reports))
		for _, kc := range representative.KeyChanges {
			fmt.Fprintf(&b, "- %s\n", kc.Text)
		}
		fmt.Fprintf(&b, "\n<sub>Applies to: %s</sub>", truncLabels(labels, 8))
		return b.String()
	}
}

// renderDivergent produces output for the "reports differ" case.
func renderDivergent(majority, outliers []*core.Report, style string) string {
	switch style {
	case "banner":
		return fmt.Sprintf("⚠️ **Fleet divergent** — %d of %d subscriptions differ from the majority pattern.",
			len(outliers), len(majority)+len(outliers))

	case "table":
		var b strings.Builder
		b.WriteString("| Subscription | Status |\n")
		b.WriteString("|--------------|--------|\n")
		for _, r := range majority {
			fmt.Fprintf(&b, "| %s | ✅ majority |\n", reportLabel(r))
		}
		for _, r := range outliers {
			fmt.Fprintf(&b, "| %s | ⚠️ outlier |\n", reportLabel(r))
		}
		return strings.TrimRight(b.String(), "\n")

	default: // summary
		var b strings.Builder
		fmt.Fprintf(&b, "⚠️ **Fleet divergent** — %d of %d subscriptions differ from the majority pattern.\n\n",
			len(outliers), len(majority)+len(outliers))
		fmt.Fprintf(&b, "**Majority (%d subs):** %s\n\n", len(majority), summarizeActions(majority[0]))
		b.WriteString("**Outliers:**\n")
		for _, r := range outliers {
			fmt.Fprintf(&b, "- **%s** — %s\n", reportLabel(r), summarizeActions(r))
		}
		return strings.TrimRight(b.String(), "\n")
	}
}

// keyChangesFingerprint concatenates sorted key-change texts. Identical key
// changes across reports → identical fingerprint.
func keyChangesFingerprint(r *core.Report) string {
	parts := make([]string, len(r.KeyChanges))
	for i, kc := range r.KeyChanges {
		parts[i] = kc.Text
	}
	sort.Strings(parts)
	return strings.Join(parts, "||")
}

// actionCountsFingerprint hashes the action-count map. Looser than
// key_changes: reports with the same overall shape but different specifics
// collide.
func actionCountsFingerprint(r *core.Report) string {
	keys := make([]string, 0, len(r.ActionCounts))
	for k := range r.ActionCounts {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%d;", k, r.ActionCounts[core.Action(k)])
	}
	return b.String()
}

// summarizeActions renders a compact action-count summary for a single report.
func summarizeActions(r *core.Report) string {
	return actionSummaryLine(r.ActionCounts)
}

// truncLabels joins labels with commas, capping the displayed count at max
// with a "(+N more)" tail.
func truncLabels(labels []string, max int) string {
	if len(labels) <= max {
		return strings.Join(labels, ", ")
	}
	return fmt.Sprintf("%s (+ %d more)", strings.Join(labels[:max], ", "), len(labels)-max)
}

func init() { defaultRegistry.Register(FleetHomogeneity{}) }
