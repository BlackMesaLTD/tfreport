package core

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// BuildRuleDiff renders a terraform-style diff of ONE resource's block-set
// elements (NSG security rules, route-table routes), pairing elements by
// name so the reviewer sees what changed inside each rule instead of
// terraform's remove-everything / add-everything set rendering:
//
//	# security_rule "intra-subnet-communication-in" — same members, written as a list instead of a string — functionally identical
//	~ security_rule "intra-subnet-communication-in" {
//	    - destination_address_prefix   = "10.42.4.0/24" -> null
//	    + destination_address_prefixes = ["10.42.4.0/24"]
//	      # (13 unchanged attributes hidden)
//	  }
//
//	# security_rule "smtp-out" — removed
//	- security_rule "smtp-out" {
//	    - access   = "Allow"
//	    …
//	  }
//
//	# (98 unchanged security_rule hidden)
//
// Uses the same terraform symbol grammar as `terraform show`, so
// TextToDiff and CollapseUnchanged apply unchanged. Object-style rule
// resources (one azurerm_network_security_rule per rule) get the same
// view built from their own before/after. Returns "" when the resource
// has no registered block set, is not an update, or nothing in the set
// changed.
func BuildRuleDiff(rc *ResourceChange) string {
	if rc == nil || rc.Action != ActionUpdate {
		return ""
	}
	if spec, ok := flatSpecs[rc.ResourceType]; ok {
		return flatRuleDiff(spec, rc)
	}
	var sections []string
	for _, spec := range nestedSetSpecs[rc.ResourceType] {
		if s := nestedRuleDiff(spec, rc.Before[spec.Attr], rc.After[spec.Attr]); s != "" {
			sections = append(sections, s)
		}
	}
	return strings.Join(sections, "\n\n")
}

func nestedRuleDiff(spec NestedSetSpec, before, after any) string {
	oldElems, ok1 := elementsByID(before, spec.IDKey)
	newElems, ok2 := elementsByID(after, spec.IDKey)
	if !ok1 || !ok2 {
		return ""
	}
	var blocks []string
	unchanged := 0
	for _, id := range unionIDs(oldElems, newElems) {
		o, inOld := oldElems[id]
		n, inNew := newElems[id]
		switch {
		case inNew && !inOld:
			blocks = append(blocks, elementBlock(spec, id, "+", n, "added: "+elementSummary(spec, n)))
		case inOld && !inNew:
			blocks = append(blocks, elementBlock(spec, id, "-", o, "removed: "+elementSummary(spec, o)))
		case reflect.DeepEqual(o, n):
			unchanged++
		default:
			blocks = append(blocks, changedElementBlock(spec, id, o, n))
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	out := strings.Join(blocks, "\n\n")
	if unchanged > 0 {
		out += fmt.Sprintf("\n\n  # (%d unchanged %s hidden)", unchanged, spec.Attr)
	}
	return out
}

// changedElementBlock renders a `~` block for an element present on both
// sides, one line per raw field difference, headed by the verdict.
func changedElementBlock(spec NestedSetSpec, id string, o, n map[string]any) string {
	co, cn := normaliseElement(spec, o), normaliseElement(spec, n)
	var verdict string
	if fields := diffFields(co, cn); len(fields) == 0 {
		_, verdict = cosmeticVerdict(spec, o, n)
	} else {
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = fmt.Sprintf("%s %s → %s", f, compact(co[f]), compact(cn[f]))
		}
		verdict = strings.Join(parts, ", ")
	}
	keys := unionKeys(o, n)
	var lines []attrLine
	unchanged := 0
	for _, k := range keys {
		if k == spec.IDKey {
			continue
		}
		ov, nv := o[k], n[k]
		if reflect.DeepEqual(ov, nv) {
			if !isEmptyValue(ov) {
				unchanged++
			}
			continue
		}
		switch {
		case isEmptyValue(ov):
			lines = append(lines, attrLine{"+", k, hclValue(nv)})
		case isEmptyValue(nv):
			lines = append(lines, attrLine{"-", k, hclValue(ov) + " -> null"})
		default:
			lines = append(lines, attrLine{"~", k, hclValue(ov) + " -> " + hclValue(nv)})
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  # %s %q — %s\n", spec.Attr, id, verdict)
	fmt.Fprintf(&b, "  ~ %s %q {\n", spec.Attr, id)
	writeAttrLines(&b, lines)
	if unchanged > 0 {
		fmt.Fprintf(&b, "        # (%d unchanged attributes hidden)\n", unchanged)
	}
	b.WriteString("    }")
	return b.String()
}

// elementBlock renders a whole element with a single symbol (+ added,
// - removed), listing every non-empty attribute.
func elementBlock(spec NestedSetSpec, id, sym string, m map[string]any, verdict string) string {
	var lines []attrLine
	for _, k := range sortedKeys(m) {
		if k == spec.IDKey || isEmptyValue(m[k]) {
			continue
		}
		lines = append(lines, attrLine{sym, k, hclValue(m[k])})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  # %s %q — %s\n", spec.Attr, id, verdict)
	fmt.Fprintf(&b, "  %s %s %q {\n", sym, spec.Attr, id)
	writeAttrLines(&b, lines)
	b.WriteString("    }")
	return b.String()
}

// flatRuleDiff renders the object-style rule resource in the same shape,
// from its own before/after maps. Provider bookkeeping attributes that
// never carry rule semantics are skipped from the unchanged count.
func flatRuleDiff(spec NestedSetSpec, rc *ResourceChange) string {
	if rc.Before == nil || rc.After == nil || spec.Attr == "" {
		return ""
	}
	id := elementName(rc.After, spec.IDKey)
	if id == "" {
		id = elementName(rc.Before, spec.IDKey)
	}
	if id == "" {
		return ""
	}
	if reflect.DeepEqual(rc.Before, rc.After) {
		return ""
	}
	return changedElementBlock(spec, id, rc.Before, rc.After)
}

type attrLine struct{ sym, key, value string }

// writeAttrLines aligns the `=` like terraform does.
func writeAttrLines(b *strings.Builder, lines []attrLine) {
	width := 0
	for _, l := range lines {
		if len(l.key) > width {
			width = len(l.key)
		}
	}
	for _, l := range lines {
		fmt.Fprintf(b, "      %s %-*s = %s\n", l.sym, width, l.key, l.value)
	}
}

func unionKeys(a, b map[string]any) []string {
	seen := map[string]struct{}{}
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// hclValue formats a plan-JSON value in terraform's display grammar:
// quoted strings, bare numbers/bools, single-line lists, `null`.
func hclValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = hclValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = strconv.Quote(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedKeys(t)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + " = " + hclValue(t[k])
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		return fmt.Sprint(v)
	}
}
