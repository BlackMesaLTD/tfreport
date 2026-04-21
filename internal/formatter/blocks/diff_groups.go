package blocks

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// DiffGroups collapses resources with identical change fingerprints. Two
// resources with the same action + same attribute keys + same before/after
// values produce the same fingerprint and are shown as a single "(×N)" row.
// This is the blocks-layer equivalent of the `dedup.py` post-processor used
// by networks-azure.
//
// Args:
//
//	threshold int (default 2) — only collapse when group size >= threshold
//	actions   csv (default "update,delete,replace") — which actions participate
//
// # Single-report semantics
//
// diff_groups operates on a single report. In multi-report mode (len(Reports) > 1)
// the block returns an error with guidance — pick one:
//
//   - `{{ range $r := .Reports }}{{ diff_groups }}{{ end }}` for per-report dedup
//   - `{{ fleet_homogeneity }}` for cross-report uniformity comparison
//
// We do not silently dedup across the first report only because that produces
// surprising results for users who expected fleet-wide dedup.
//
// # Fingerprint caveat
//
// Fingerprint preserves slice order. `tags=[a,b,c]` and `tags=[b,a,c]` are
// treated as different changes. This matches terraform's own equality
// semantics.
type DiffGroups struct{}

func (DiffGroups) Name() string { return "diff_groups" }

func (DiffGroups) Render(ctx *BlockContext, args map[string]any) (string, error) {
	if len(ctx.Reports) > 1 {
		return "", fmt.Errorf("diff_groups: multi-report mode not supported — wrap in {{ range .Reports }}…{{ end }} for per-report dedup, or use fleet_homogeneity for cross-report uniformity")
	}

	threshold := ArgInt(args, "threshold", 2)
	if threshold < 1 {
		threshold = 1
	}
	actions := parseActionFilter(ArgString(args, "actions", "update,delete,replace"))

	r := currentReport(ctx)
	if r == nil {
		return "", nil
	}

	type bucket struct {
		fingerprint string
		action      core.Action
		attrKeys    []string
		resources   []core.ResourceChange
	}
	buckets := map[string]*bucket{}
	var order []string

	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			if _, ok := actions[rc.Action]; !ok {
				continue
			}
			fp := fingerprint(rc)
			b, ok := buckets[fp]
			if !ok {
				b = &bucket{
					fingerprint: fp,
					action:      rc.Action,
					attrKeys:    core.ChangedAttributeKeys(rc.ChangedAttributes),
				}
				buckets[fp] = b
				order = append(order, fp)
			}
			b.resources = append(b.resources, rc)
		}
	}

	// Split into collapsed and individual rows by threshold.
	var collapsed, individual []*bucket
	for _, fp := range order {
		b := buckets[fp]
		if len(b.resources) >= threshold {
			collapsed = append(collapsed, b)
		} else {
			individual = append(individual, b)
		}
	}

	// Sort collapsed groups by descending count, then by action severity
	sort.SliceStable(collapsed, func(i, j int) bool {
		if len(collapsed[i].resources) != len(collapsed[j].resources) {
			return len(collapsed[i].resources) > len(collapsed[j].resources)
		}
		return actionOrder(collapsed[i].action) < actionOrder(collapsed[j].action)
	})

	if len(collapsed) == 0 && len(individual) == 0 {
		return "", nil
	}

	var out strings.Builder
	if len(collapsed) > 0 {
		out.WriteString("**Deduplicated changes:**\n\n")
		out.WriteString("| Pattern | Count | Sample |\n")
		out.WriteString("|---------|-------|--------|\n")
		for _, b := range collapsed {
			pattern := fmt.Sprintf("%s %s [%s]",
				core.ActionEmoji(b.action), b.action, strings.Join(b.attrKeys, ", "))
			sample := b.resources[0].Address
			fmt.Fprintf(&out, "| %s | %d | `%s` |\n", pattern, len(b.resources), sample)
		}
		out.WriteString("\n")
	}

	if len(individual) > 0 {
		// Count every resource across sub-threshold buckets, not just one
		// per bucket. A bucket with 2 members below threshold=3 still
		// contributes 2 lines.
		total := 0
		for _, b := range individual {
			total += len(b.resources)
		}
		fmt.Fprintf(&out, "_%d resource%s with unique changes:_\n\n", total, plural(total))
		for _, b := range individual {
			for _, rc := range b.resources {
				fmt.Fprintf(&out, "- %s `%s` [%s]\n",
					core.ActionEmoji(rc.Action), rc.Address, strings.Join(b.attrKeys, ", "))
			}
		}
	}

	return strings.TrimRight(out.String(), "\n"), nil
}

// fingerprint returns a stable hash of a resource change's semantics.
// Action + sorted attribute keys + JSON-canonicalized before/after values.
func fingerprint(rc core.ResourceChange) string {
	h := sha1.New()
	fmt.Fprintf(h, "action=%s\n", rc.Action)

	keys := core.ChangedAttributeKeys(rc.ChangedAttributes)
	sort.Strings(keys)
	fmt.Fprintf(h, "keys=%s\n", strings.Join(keys, ","))

	// Fingerprint each changed attribute's before/after. json.Marshal on a
	// map sorts keys, so map ordering is deterministic. Slice ordering is
	// preserved (see docstring).
	for _, k := range keys {
		for _, a := range rc.ChangedAttributes {
			if a.Key != k {
				continue
			}
			old, _ := json.Marshal(a.OldValue)
			new, _ := json.Marshal(a.NewValue)
			fmt.Fprintf(h, "%s: %s -> %s\n", k, old, new)
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// actionOrder ranks actions from most-destructive to least for tie-breaking.
func actionOrder(a core.Action) int {
	switch a {
	case core.ActionReplace:
		return 0
	case core.ActionDelete:
		return 1
	case core.ActionUpdate:
		return 2
	case core.ActionCreate:
		return 3
	case core.ActionRead:
		return 4
	default:
		return 5
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Doc describes diff_groups for cmd/docgen.
func (DiffGroups) Doc() BlockDoc {
	return BlockDoc{
		Name:    "diff_groups",
		Summary: "Collapses resources with identical change fingerprints into grouped rows. Single-report only; use fleet_homogeneity for cross-report uniformity.",
		Args: []ArgDoc{
			{Name: "threshold", Type: "int", Default: "2", Description: "Only collapse when group size ≥ threshold."},
			{Name: "actions", Type: "csv", Default: "update,delete,replace", Description: "Which actions participate in fingerprint grouping."},
		},
	}
}

func init() { defaultRegistry.Register(DiffGroups{}) }
