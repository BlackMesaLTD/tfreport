package blocks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// Banner renders a conditional callout line. The block stays silent (empty
// output) unless at least one configured trigger matches — safe to include
// unconditionally in a template. When no triggers are configured, the
// banner is treated as always-on.
//
// Triggers (OR semantics — any match fires the banner):
//
//	if_impact csv
//	    Fire when the report's MaxImpact is in the set. Compared across
//	    all reports in scope (single-report or multi).
//
//	if_action_gt csv — flat "action:N,action:N" syntax.
//	    Fire when action_count(action) > N. Example:
//	    `if_action_gt="delete:0,replace:0"` fires when either any delete or
//	    any replace exists.
//
// Rendering:
//
//	style string (alert|warn|success|info; default "alert")
//	    Picks a default icon when `icon` is unset:
//	    alert=⛔, warn=⚠️, success=✅, info=ℹ️.
//
//	icon string (default: style-derived)
//	    Override the leading emoji.
//
//	text string (required)
//	    The body of the banner. Static; for dynamic text wrap the block
//	    inside template-level conditionals.
//
// Output grammar: `{icon} **{text}**` on a single line. Callers add
// separators/newlines around it in the template.
type Banner struct{}

func (Banner) Name() string { return "banner" }

func (Banner) Render(ctx *BlockContext, args map[string]any) (string, error) {
	text := ArgString(args, "text", "")
	if text == "" {
		return "", fmt.Errorf("banner: 'text' arg is required")
	}

	style := ArgString(args, "style", "alert")
	if !validBannerStyle(style) {
		return "", fmt.Errorf("banner: unknown style %q (valid: alert, warn, success, info)", style)
	}
	icon := ArgString(args, "icon", defaultBannerIcon(style))

	impactTriggers := parseImpactFilterSet(ArgCSV(args, "if_impact"))
	actionGtTriggers, err := parseActionGtCSV(ArgString(args, "if_action_gt", ""))
	if err != nil {
		return "", err
	}

	hasTriggers := impactTriggers != nil || len(actionGtTriggers) > 0
	fire := !hasTriggers // no triggers → always fire

	if !fire && impactTriggers != nil {
		for _, r := range allReports(ctx) {
			if _, ok := impactTriggers[r.MaxImpact]; ok {
				fire = true
				break
			}
		}
	}
	if !fire && len(actionGtTriggers) > 0 {
		for action, threshold := range actionGtTriggers {
			total := 0
			for _, r := range allReports(ctx) {
				total += r.ActionCounts[core.Action(action)]
			}
			if total > threshold {
				fire = true
				break
			}
		}
	}
	if !fire {
		return "", nil
	}

	return fmt.Sprintf("%s **%s**", icon, text), nil
}

func validBannerStyle(s string) bool {
	switch s {
	case "alert", "warn", "success", "info":
		return true
	}
	return false
}

func defaultBannerIcon(style string) string {
	switch style {
	case "alert":
		return "⛔"
	case "warn":
		return "⚠️"
	case "success":
		return "✅"
	case "info":
		return "ℹ️"
	}
	return ""
}

// parseActionGtCSV parses a flat csv like "delete:0,replace:1" into a map
// {"delete":0,"replace":1}. Returns a helpful error for malformed entries.
func parseActionGtCSV(s string) (map[string]int, error) {
	if s == "" {
		return nil, nil
	}
	out := map[string]int{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		colon := strings.IndexByte(pair, ':')
		if colon < 0 {
			return nil, fmt.Errorf("banner: if_action_gt entry %q must be `action:N`", pair)
		}
		action := strings.TrimSpace(pair[:colon])
		n, err := strconv.Atoi(strings.TrimSpace(pair[colon+1:]))
		if err != nil {
			return nil, fmt.Errorf("banner: if_action_gt entry %q: threshold must be integer", pair)
		}
		out[action] = n
	}
	return out, nil
}

// Doc describes banner for cmd/docgen.
func (Banner) Doc() BlockDoc {
	return BlockDoc{
		Name:    "banner",
		Summary: "Conditional callout line. Returns empty when no trigger matches — safe to include unconditionally. OR semantics across triggers.",
		Args: []ArgDoc{
			{Name: "text", Type: "string", Default: "—", Description: "Required. Banner body."},
			{Name: "style", Type: "string", Default: "alert", Description: "One of `alert`, `warn`, `success`, `info`. Picks the default icon."},
			{Name: "icon", Type: "string", Default: "(style-derived)", Description: "Override the leading emoji."},
			{Name: "if_impact", Type: "csv", Default: "(none)", Description: "Fire when any report's MaxImpact is in the set (e.g. `critical,high`)."},
			{Name: "if_action_gt", Type: "csv", Default: "(none)", Description: "Flat `action:N,action:N` syntax. Fire when action_count(action) > N."},
		},
		Examples: []ExampleDoc{
			{
				Template: `{{ banner "if_action_gt" "delete:0,replace:0" "style" "alert" "text" "Destructive changes detected — review carefully." }}`,
				Rendered: "⛔ **Destructive changes detected — review carefully.**",
			},
		},
	}
}

func init() { defaultRegistry.Register(Banner{}) }
