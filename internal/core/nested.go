package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// NestedSetSpec describes a set-of-objects attribute (terraform "block set")
// whose elements carry a stable identity, so a change to the set can be
// reported per element ("security_rule[allow-443].priority") instead of as
// one opaque key ("security_rule") that could mean anything from a
// description tweak to every rule being deleted.
//
// ScalarListPairs name attribute pairs that express the same thing in two
// shapes — azurerm's source_address_prefix (string) and
// source_address_prefixes (list) — so a rule rewritten from one shape to
// the other with the same members is reported as a format-only change.
// CaseInsensitive lists attributes compared without regard to case
// ("Tcp" vs "TCP").
type NestedSetSpec struct {
	Attr            string
	IDKey           string
	ScalarListPairs [][2]string
	CaseInsensitive []string
}

// Suffixes appended to a nested element key by ExpandNestedChanges.
const (
	NestedAdded   = "added"   // element present only after
	NestedRemoved = "removed" // element present only before
	NestedOrder   = "order"   // whole set identical after normalisation; elements reordered

	// Cosmetic verdicts: the element's members are identical after
	// normalisation, only the shape changed. The key names the fields
	// involved: <attr>[<id>].[field_a,field_b].<verdict> (single field:
	// <attr>[<id>].field_a.<verdict>).
	NestedStringToList = "string_to_list" // e.g. source_address_prefix "x" → source_address_prefixes ["x"]
	NestedListToString = "list_to_string" // the reverse (console / bulk-script rewrite)
	NestedCaseOnly     = "case_only"      // "Tcp" → "TCP"
	NestedRewritten    = "rewritten"      // mixed directions in one element
)

// cosmeticSuffixes are the verdicts IsCosmeticKey recognises.
var cosmeticSuffixes = []string{NestedStringToList, NestedListToString, NestedCaseOnly, NestedRewritten, NestedOrder}

var nsgRulePairs = [][2]string{
	{"source_address_prefix", "source_address_prefixes"},
	{"destination_address_prefix", "destination_address_prefixes"},
	{"source_port_range", "source_port_ranges"},
	{"destination_port_range", "destination_port_ranges"},
}

var nsgRuleCaseInsensitive = []string{"protocol", "access", "direction"}

// nestedSetSpecs keys resource types to the block sets that get per-element
// expansion. Provider-neutral in mechanism; the table is where provider
// knowledge lives.
var nestedSetSpecs = map[string][]NestedSetSpec{
	"azurerm_network_security_group": {{
		Attr: "security_rule", IDKey: "name",
		ScalarListPairs: nsgRulePairs, CaseInsensitive: nsgRuleCaseInsensitive,
	}},
	"azurerm_route_table": {{
		Attr: "route", IDKey: "name",
		CaseInsensitive: []string{"next_hop_type"},
	}},
}

// flatSpecs applies the same equivalence rules to resources whose
// attributes ARE the rule (object-style modules: one azurerm_network_security_rule
// per rule). Attr/IDKey are unused here.
var flatSpecs = map[string]NestedSetSpec{
	"azurerm_network_security_rule": {Attr: "security_rule", IDKey: "name", ScalarListPairs: nsgRulePairs, CaseInsensitive: nsgRuleCaseInsensitive},
	"azurerm_route":                 {Attr: "route", IDKey: "name", CaseInsensitive: []string{"next_hop_type"}},
}

// ExpandResourceChanges applies ExpandNestedChanges to one resource and,
// for object-style rule resources (one azurerm_network_security_rule per
// rule), prefixes each changed key of an update with "<attr>[<name>]." so
// both module styles produce the same grammar in aggregated tables:
//
//	inline:  azurerm_network_security_group  → security_rule[allow-443].priority
//	object:  azurerm_network_security_rule   → security_rule[allow-443].priority
//
// Create / delete of a rule resource is left as-is: the address and
// action already say which rule appeared or disappeared.
func ExpandResourceChanges(rc *ResourceChange) {
	rc.ChangedAttributes = ExpandNestedChanges(rc.ResourceType, rc.ChangedAttributes)
	spec, ok := flatSpecs[rc.ResourceType]
	if !ok || spec.Attr == "" || rc.Action != ActionUpdate {
		return
	}
	name := elementName(rc.After, spec.IDKey)
	if name == "" {
		name = elementName(rc.Before, spec.IDKey)
	}
	if name == "" {
		return
	}
	prefix := spec.Attr + "[" + name + "]."
	for i := range rc.ChangedAttributes {
		rc.ChangedAttributes[i].Key = prefix + rc.ChangedAttributes[i].Key
	}
}

func elementName(m map[string]any, idKey string) string {
	if m == nil {
		return ""
	}
	s, _ := m[idKey].(string)
	return s
}

// FieldOfAttributeKey returns the field part of an expanded key:
// "security_rule[allow-443].priority" → "priority",
// "security_rule[x].source_address_prefixes.format" → "source_address_prefixes.format".
// Keys without an element expansion return "". Impact resolvers use it so
// an override on a rule resource's own attribute ("priority") still
// applies once the key has been prefixed by ExpandResourceChanges.
func FieldOfAttributeKey(key string) string {
	i := strings.Index(key, "].")
	if i < 0 {
		return ""
	}
	return key[i+2:]
}

// NestedSetSpecsFor returns the block-set specs registered for a resource
// type (nil when the type has none). Exposed for tests and docs.
func NestedSetSpecsFor(resourceType string) []NestedSetSpec {
	return nestedSetSpecs[resourceType]
}

// ExpandNestedChanges rewrites the ChangedAttributes of one resource so that
// registered block sets are reported per element. For every registered set
// attribute whose before/after are both lists of objects carrying IDKey:
//
//	<attr>[<id>].added      element only in after (NewValue = element)
//	<attr>[<id>].removed    element only in before (OldValue = element)
//	<attr>[<id>].<field>    field differs after normalisation (Old/New = field values)
//	<attr>[<id>].[f1,f2].string_to_list   same members; those fields moved from string to list form
//	<attr>[<id>].[f1,f2].list_to_string   the reverse
//	<attr>[<id>].<f>.case_only            letter case only
//	<attr>[<id>].[f1,f2].rewritten        mixed directions in one element
//	<attr>.order            every element identical after normalisation; reordered only
//
// Description carries a short human phrase ("rule added", "100 → 110",
// "same members, written as a list instead of a string"). Attributes that
// are Computed, Sensitive, not lists, or whose elements lack IDKey are
// returned untouched. Flat rule resources (azurerm_network_security_rule)
// get the pair/case equivalence applied to their own top-level attributes:
// a scalar↔list rewrite with the same members collapses to one
// "[<list_attrs>].<verdict>" key.
func ExpandNestedChanges(resourceType string, attrs []ChangedAttribute) []ChangedAttribute {
	if spec, ok := flatSpecs[resourceType]; ok {
		return collapseFlatEquivalents(spec, attrs)
	}
	specs := nestedSetSpecs[resourceType]
	if len(specs) == 0 {
		return attrs
	}
	byAttr := map[string]NestedSetSpec{}
	for _, s := range specs {
		byAttr[s.Attr] = s
	}
	out := make([]ChangedAttribute, 0, len(attrs))
	for _, a := range attrs {
		spec, ok := byAttr[a.Key]
		if !ok || a.Computed || a.Sensitive {
			out = append(out, a)
			continue
		}
		expanded, ok := expandSet(spec, a)
		if !ok {
			out = append(out, a)
			continue
		}
		out = append(out, expanded...)
	}
	return out
}

func expandSet(spec NestedSetSpec, a ChangedAttribute) ([]ChangedAttribute, bool) {
	oldElems, ok1 := elementsByID(a.OldValue, spec.IDKey)
	newElems, ok2 := elementsByID(a.NewValue, spec.IDKey)
	if !ok1 || !ok2 {
		return nil, false
	}
	ids := unionIDs(oldElems, newElems)

	var out []ChangedAttribute
	prefix := a.Key
	reorderedOnly := true
	for _, id := range ids {
		o, inOld := oldElems[id]
		n, inNew := newElems[id]
		key := fmt.Sprintf("%s[%s]", prefix, id)
		switch {
		case inNew && !inOld:
			reorderedOnly = false
			out = append(out, ChangedAttribute{Key: key + "." + NestedAdded, NewValue: n, Description: elementSummary(spec, n)})
		case inOld && !inNew:
			reorderedOnly = false
			out = append(out, ChangedAttribute{Key: key + "." + NestedRemoved, OldValue: o, Description: elementSummary(spec, o)})
		default:
			if reflect.DeepEqual(o, n) {
				continue // untouched element (set was reordered around it)
			}
			reorderedOnly = false
			co, cn := normaliseElement(spec, o), normaliseElement(spec, n)
			fields := diffFields(co, cn)
			if len(fields) == 0 {
				suffix, desc := cosmeticVerdict(spec, o, n)
				out = append(out, ChangedAttribute{Key: key + "." + suffix, OldValue: o, NewValue: n, Description: desc})
				continue
			}
			for _, f := range fields {
				out = append(out, ChangedAttribute{
					Key: key + "." + f, OldValue: o[f], NewValue: n[f],
					Description: fmt.Sprintf("%s → %s", compact(co[f]), compact(cn[f])),
				})
			}
		}
	}
	if len(out) == 0 && reorderedOnly {
		return []ChangedAttribute{{Key: prefix + "." + NestedOrder, OldValue: a.OldValue, NewValue: a.NewValue, Description: "elements reordered only — functionally identical"}}, true
	}
	return out, true
}

// elementsByID converts a list-of-objects attribute value into a map keyed
// by the element's IDKey. Returns false when the value is not such a list,
// any element lacks a string ID, or IDs collide.
func elementsByID(v any, idKey string) (map[string]map[string]any, bool) {
	if v == nil {
		return map[string]map[string]any{}, true
	}
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]map[string]any, len(list))
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, false
		}
		id, ok := m[idKey].(string)
		if !ok || id == "" {
			return nil, false
		}
		if _, dup := out[id]; dup {
			return nil, false
		}
		out[id] = m
	}
	return out, true
}

func unionIDs(a, b map[string]map[string]any) []string {
	seen := map[string]bool{}
	var ids []string
	for id := range a {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for id := range b {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// normaliseElement returns a canonical copy of one element: scalar/list
// pairs merged into a sorted set under the list key, case-insensitive
// fields lower-cased, empty strings / empty lists / nils dropped.
func normaliseElement(spec NestedSetSpec, m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	for _, p := range spec.ScalarListPairs {
		scalar, list := p[0], p[1]
		set := stringSet(out[list])
		if s, ok := out[scalar].(string); ok && s != "" {
			set[s] = true
		}
		delete(out, scalar)
		delete(out, list)
		if len(set) > 0 {
			out[list] = sortedKeys(mapBool(set))
		}
	}
	for _, k := range spec.CaseInsensitive {
		if s, ok := out[k].(string); ok {
			out[k] = strings.ToLower(s)
		}
	}
	for k, v := range out {
		if isEmptyValue(v) {
			delete(out, k)
		}
	}
	return out
}

func stringSet(v any) map[string]bool {
	set := map[string]bool{}
	if list, ok := v.([]any); ok {
		for _, e := range list {
			if s, ok := e.(string); ok && s != "" {
				set[s] = true
			}
		}
	}
	return set
}

func mapBool(m map[string]bool) map[string]any {
	out := make(map[string]any, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// diffFields returns the sorted field names whose canonical values differ.
func diffFields(a, b map[string]any) []string {
	seen := map[string]bool{}
	var fields []string
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	for k := range seen {
		if !reflect.DeepEqual(a[k], b[k]) {
			fields = append(fields, k)
		}
	}
	sort.Strings(fields)
	return fields
}

// elementSummary gives a one-line gist of a rule/route element for the
// Description of added/removed keys, e.g. "Allow Inbound Tcp 443 from 10.0.0.0/8".
func elementSummary(spec NestedSetSpec, m map[string]any) string {
	c := normaliseElement(spec, m)
	var parts []string
	for _, k := range []string{"access", "direction", "protocol"} {
		if s, ok := c[k].(string); ok {
			parts = append(parts, s)
		}
	}
	if v, ok := c["destination_port_ranges"]; ok {
		parts = append(parts, "port "+compact(v))
	}
	if v, ok := c["source_address_prefixes"]; ok {
		parts = append(parts, "from "+compact(v))
	}
	if v, ok := c["destination_address_prefixes"]; ok {
		parts = append(parts, "to "+compact(v))
	}
	if v, ok := c["address_prefix"]; ok {
		parts = append(parts, compact(v)+" via "+compact(c["next_hop_type"]))
		if ip, ok := c["next_hop_in_ip_address"]; ok {
			parts = append(parts, compact(ip))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// compact renders a value for Description text, capped so rule lists never
// blow up a table cell.
func compact(v any) string {
	var s string
	switch t := v.(type) {
	case nil:
		return "∅"
	case string:
		s = t
	case []string:
		s = strings.Join(t, ",")
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		s = fmt.Sprintf("%g", t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprint(v)
		} else {
			s = string(b)
		}
	}
	if s == "" {
		return "∅"
	}
	if len(s) > 48 {
		return s[:45] + "…"
	}
	return s
}

// cosmeticTally accumulates which fields of one rule changed shape and in
// which direction, and turns that into a key suffix + description.
type cosmeticTally struct {
	fields                                       []string
	toList, toString, mixed, reordered, caseOnly int
}

func (t *cosmeticTally) pair(list string, oldHasScalar, newHasScalar, oldHasList, newHasList bool) {
	t.fields = append(t.fields, list)
	switch {
	case oldHasScalar && !newHasScalar && newHasList:
		t.toList++
	case oldHasList && !newHasList && newHasScalar:
		t.toString++
	case oldHasScalar == newHasScalar && oldHasList == newHasList:
		t.reordered++ // same shape, members in a different order
	default:
		t.mixed++
	}
}

func (t *cosmeticTally) caseField(field string) {
	t.fields = append(t.fields, field)
	t.caseOnly++
}

// suffix renders "[f1,f2].<verdict>" (or "f1.<verdict>" for one field)
// and a human description.
func (t *cosmeticTally) suffix() (string, string) {
	sort.Strings(t.fields)
	fields := strings.Join(t.fields, ",")
	if len(t.fields) > 1 {
		fields = "[" + fields + "]"
	}
	var verdict, desc string
	switch {
	case t.mixed > 0 || (t.toList > 0 && t.toString > 0):
		verdict, desc = NestedRewritten, "same members, string/list form rewritten"
	case t.toList > 0:
		verdict, desc = NestedStringToList, "same members, written as a list instead of a string"
	case t.toString > 0:
		verdict, desc = NestedListToString, "same members, written as a string instead of a list"
	case t.reordered > 0:
		verdict, desc = NestedRewritten, "same members, list order changed"
	default:
		verdict, desc = NestedCaseOnly, "letter case only"
	}
	if t.caseOnly > 0 && verdict != NestedCaseOnly {
		desc += " (plus letter case)"
	}
	if fields == "" {
		return verdict, desc + " — functionally identical"
	}
	return fields + "." + verdict, desc + " — functionally identical"
}

// cosmeticVerdict inspects a raw before/after element pair whose
// normalised forms are equal and names the fields whose shape changed.
func cosmeticVerdict(spec NestedSetSpec, o, n map[string]any) (string, string) {
	var t cosmeticTally
	for _, p := range spec.ScalarListPairs {
		scalar, list := p[0], p[1]
		if reflect.DeepEqual(o[scalar], n[scalar]) && reflect.DeepEqual(o[list], n[list]) {
			continue
		}
		t.pair(list, nonEmptyString(o[scalar]), nonEmptyString(n[scalar]), len(stringSet(o[list])) > 0, len(stringSet(n[list])) > 0)
	}
	for _, k := range spec.CaseInsensitive {
		os, ok1 := o[k].(string)
		ns, ok2 := n[k].(string)
		if ok1 && ok2 && os != ns && strings.EqualFold(os, ns) {
			t.caseField(k)
		}
	}
	return t.suffix()
}

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

// collapseFlatEquivalents applies pair/case equivalence to a resource whose
// top-level attributes are the rule. Every scalar/list pair whose two
// halves both changed with equal member sets, and every CaseInsensitive
// field that changed only in case, is dropped and replaced by ONE entry
// naming all of them: "[destination_address_prefixes,source_address_prefixes].string_to_list".
func collapseFlatEquivalents(spec NestedSetSpec, attrs []ChangedAttribute) []ChangedAttribute {
	byKey := map[string]ChangedAttribute{}
	for _, a := range attrs {
		byKey[a.Key] = a
	}
	drop := map[string]bool{}
	var t cosmeticTally
	var firstOld, lastNew any
	for _, p := range spec.ScalarListPairs {
		scalar, list := p[0], p[1]
		sa, hasS := byKey[scalar]
		la, hasL := byKey[list]
		if !hasS || !hasL || sa.Computed || la.Computed || sa.Sensitive || la.Sensitive {
			continue
		}
		before := stringSet(la.OldValue)
		if s, ok := sa.OldValue.(string); ok && s != "" {
			before[s] = true
		}
		after := stringSet(la.NewValue)
		if s, ok := sa.NewValue.(string); ok && s != "" {
			after[s] = true
		}
		if reflect.DeepEqual(before, after) {
			drop[scalar], drop[list] = true, true
			t.pair(list, nonEmptyString(sa.OldValue), nonEmptyString(sa.NewValue), len(stringSet(la.OldValue)) > 0, len(stringSet(la.NewValue)) > 0)
			if firstOld == nil {
				firstOld = sa.OldValue
			}
			lastNew = la.NewValue
		}
	}
	for _, k := range spec.CaseInsensitive {
		a, ok := byKey[k]
		if !ok || a.Computed || a.Sensitive {
			continue
		}
		o, ok1 := a.OldValue.(string)
		n, ok2 := a.NewValue.(string)
		if ok1 && ok2 && o != n && strings.EqualFold(o, n) {
			drop[k] = true
			t.caseField(k)
			if firstOld == nil {
				firstOld, lastNew = o, n
			}
		}
	}
	if len(drop) == 0 {
		return attrs
	}
	out := make([]ChangedAttribute, 0, len(attrs))
	for _, a := range attrs {
		if !drop[a.Key] {
			out = append(out, a)
		}
	}
	suffix, desc := t.suffix()
	return append(out, ChangedAttribute{Key: suffix, OldValue: firstOld, NewValue: lastNew, Description: desc})
}

// BaseAttributeKey strips the per-element expansion from a key produced by
// ExpandNestedChanges: "security_rule[allow-443].priority" → "security_rule",
// "security_rule.order" → "security_rule". Keys without an expansion are
// returned unchanged. Impact resolvers use it so a config override on
// "security_rule" still governs every expanded key.
func BaseAttributeKey(key string) string {
	if i := strings.IndexByte(key, '['); i >= 0 {
		return key[:i]
	}
	if strings.HasSuffix(key, "."+NestedOrder) {
		return key[:strings.LastIndexByte(key, '.')]
	}
	return key
}

// IsCosmeticKey reports whether an expanded key denotes a change with no
// functional effect (string_to_list, list_to_string, case_only, rewritten,
// order). Impact resolvers map these to ImpactNone unless config says
// otherwise.
func IsCosmeticKey(key string) bool {
	for _, s := range cosmeticSuffixes {
		if strings.HasSuffix(key, "."+s) {
			return true
		}
	}
	return false
}

// CosmeticVerdict returns the trailing verdict of a cosmetic key
// ("string_to_list") or "" for any other key.
func CosmeticVerdict(key string) string {
	for _, s := range cosmeticSuffixes {
		if strings.HasSuffix(key, "."+s) {
			return s
		}
	}
	return ""
}

// SummaryKey reduces an expanded key to the shape the plain-English
// summariser groups on, dropping the element id and field list:
//
//	security_rule[allow-443].priority                        → security_rule.priority
//	security_rule[allow-443].added                           → security_rule.added
//	security_rule[x].[a,b].string_to_list                    → security_rule.string_to_list
//	security_rule.order                                      → security_rule.order
//	tags                                                     → tags
//
// Without it a tag-sweep style rewrite of 160 rules produced a key set of
// 160 entries and a sentence to match.
func SummaryKey(key string) string {
	if !strings.Contains(key, "[") {
		return key
	}
	base := BaseAttributeKey(key)
	tail := key[strings.LastIndexByte(key, '.')+1:]
	if tail == "" || tail == key {
		return base
	}
	return base + "." + tail
}
