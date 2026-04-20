package blocks

import (
	"fmt"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// RiskHistogram renders an impact-level distribution across all resources
// in scope. Three visual styles:
//
//	style="bar"    (default) \u2014 full markdown table with unicode bars
//	style="table"             \u2014 same as bar minus the Bar column
//	style="inline"            \u2014 single-line form: "\ud83d\udd34 0 \u00b7 \ud83d\udd34 1 \u00b7 \ud83d\udfe1 2 \u00b7 \ud83d\udfe2 1"
//
// Args:
//
//	style         string (default "bar")
//	include_none  bool   (default false) \u2014 include impact=none (no-op) in output
//	max_bar       int    (default 40)     \u2014 cap bar length to avoid overflow
type RiskHistogram struct{}

func (RiskHistogram) Name() string { return "risk_histogram" }

func (RiskHistogram) Render(ctx *BlockContext, args map[string]any) (string, error) {
	style := ArgString(args, "style", "bar")
	includeNone := ArgBool(args, "include_none", false)
	maxBar := ArgInt(args, "max_bar", 40)

	counts := tallyImpacts(ctx)

	type bucket struct {
		label  string
		value  int
		impact core.Impact
	}
	order := []bucket{
		{"🔴 critical", counts[core.ImpactCritical], core.ImpactCritical},
		{"🔴 high", counts[core.ImpactHigh], core.ImpactHigh},
		{"🟡 medium", counts[core.ImpactMedium], core.ImpactMedium},
		{"🟢 low", counts[core.ImpactLow], core.ImpactLow},
	}
	if includeNone {
		order = append(order, bucket{"⚪ none", counts[core.ImpactNone], core.ImpactNone})
	}

	switch style {
	case "inline":
		var parts []string
		for _, b := range order {
			parts = append(parts, fmt.Sprintf("%s %d", firstWord(b.label), b.value))
		}
		return strings.Join(parts, " · "), nil

	case "table":
		var b strings.Builder
		b.WriteString("| Impact | Count |\n")
		b.WriteString("|--------|-------|\n")
		for _, row := range order {
			fmt.Fprintf(&b, "| %s | %d |\n", row.label, row.value)
		}
		return strings.TrimRight(b.String(), "\n"), nil

	case "bar":
		var b strings.Builder
		b.WriteString("| Impact | Count | Bar |\n")
		b.WriteString("|--------|-------|-----|\n")
		for _, row := range order {
			bar := renderBar(row.value, maxBar)
			fmt.Fprintf(&b, "| %s | %d | %s |\n", row.label, row.value, bar)
		}
		return strings.TrimRight(b.String(), "\n"), nil

	default:
		return "", fmt.Errorf("risk_histogram: unknown style %q (valid: bar, table, inline)", style)
	}
}

// tallyImpacts counts resources by impact across all reports in ctx.
func tallyImpacts(ctx *BlockContext) map[core.Impact]int {
	counts := map[core.Impact]int{}
	for _, r := range allReports(ctx) {
		for _, mg := range r.ModuleGroups {
			for _, rc := range mg.Changes {
				counts[rc.Impact]++
			}
		}
	}
	return counts
}

// renderBar returns a bar of solid blocks, capped at cap. Uses count directly
// (no normalization) so bars reflect raw magnitude when differences are
// small; large counts are truncated with a "+" marker.
func renderBar(count, cap int) string {
	if count <= 0 {
		return ""
	}
	if count > cap {
		return strings.Repeat("█", cap) + "+"
	}
	return strings.Repeat("█", count)
}

// firstWord returns the first whitespace-separated token of s. Used to
// extract just the emoji from "🔴 critical" for inline-style rendering.
func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

func init() { defaultRegistry.Register(RiskHistogram{}) }
