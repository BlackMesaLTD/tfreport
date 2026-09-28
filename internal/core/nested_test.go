package core

import (
	"strings"
	"testing"
)

func rule(name string, prio float64, over map[string]any) map[string]any {
	m := map[string]any{
		"name": name, "priority": prio, "access": "Allow", "direction": "Inbound", "protocol": "Tcp",
		"description": "", "source_address_prefix": "*", "source_address_prefixes": []any{},
		"destination_address_prefix": "", "destination_address_prefixes": []any{"10.0.0.0/8"},
		"source_port_range": "*", "source_port_ranges": []any{},
		"destination_port_range": "", "destination_port_ranges": []any{"443", "80"},
	}
	for k, v := range over {
		m[k] = v
	}
	return m
}

func keysOf(attrs []ChangedAttribute) string {
	var ks []string
	for _, a := range attrs {
		ks = append(ks, a.Key)
	}
	return strings.Join(ks, " ")
}

func TestExpandNestedChanges_nsgRules(t *testing.T) {
	before := []any{rule("keep", 100, nil), rule("gone", 200, nil), rule("prio", 300, nil), rule("shape", 400, nil)}
	after := []any{
		rule("shape", 400, map[string]any{ // same members, string→list and case only
			"destination_address_prefix": "10.0.0.0/8", "destination_address_prefixes": []any{},
			"destination_port_range": "", "destination_port_ranges": []any{"80", "443"},
			"protocol": "TCP",
		}),
		rule("prio", 310, map[string]any{"description": "moved"}),
		rule("keep", 100, nil),
		rule("new", 500, nil),
	}
	got := ExpandNestedChanges("azurerm_network_security_group", []ChangedAttribute{
		{Key: "security_rule", OldValue: before, NewValue: after},
		{Key: "tags", OldValue: map[string]any{"a": "1"}, NewValue: map[string]any{"a": "2"}},
	})
	want := "security_rule[gone].removed security_rule[new].added security_rule[prio].description security_rule[prio].priority security_rule[shape].format tags"
	if keysOf(got) != want {
		t.Fatalf("keys mismatch\n got: %s\nwant: %s", keysOf(got), want)
	}
	byKey := map[string]ChangedAttribute{}
	for _, a := range got {
		byKey[a.Key] = a
	}
	if d := byKey["security_rule[prio].priority"].Description; d != "300 → 310" {
		t.Errorf("priority description: %q", d)
	}
	if d := byKey["security_rule[new].added"].Description; !strings.Contains(d, "allow inbound tcp port 443,80") {
		t.Errorf("added description: %q", d)
	}
	if !IsCosmeticKey("security_rule[shape].format") || IsCosmeticKey("security_rule[prio].priority") {
		t.Error("IsCosmeticKey wrong")
	}
	if BaseAttributeKey("security_rule[prio].priority") != "security_rule" || BaseAttributeKey("security_rule.order") != "security_rule" || BaseAttributeKey("tags") != "tags" {
		t.Error("BaseAttributeKey wrong")
	}
}

func TestExpandNestedChanges_reorderOnly(t *testing.T) {
	a, b := rule("a", 100, nil), rule("b", 200, nil)
	got := ExpandNestedChanges("azurerm_network_security_group", []ChangedAttribute{
		{Key: "security_rule", OldValue: []any{a, b}, NewValue: []any{b, a}},
	})
	if keysOf(got) != "security_rule.order" {
		t.Errorf("want order-only key, got %s", keysOf(got))
	}
}

func TestExpandNestedChanges_untouchedCases(t *testing.T) {
	in := []ChangedAttribute{
		{Key: "security_rule", OldValue: []any{map[string]any{"priority": 1.0}}, NewValue: []any{}}, // no name → untouched
		{Key: "security_rule", Computed: true},
		{Key: "location", OldValue: "a", NewValue: "b"},
	}
	if got := ExpandNestedChanges("azurerm_network_security_group", in); keysOf(got) != "security_rule security_rule location" {
		t.Errorf("got %s", keysOf(got))
	}
	if got := ExpandNestedChanges("azurerm_subnet", in); keysOf(got) != "security_rule security_rule location" {
		t.Errorf("unknown type must be untouched, got %s", keysOf(got))
	}
	// Every rule removed: 2 removed keys, never a bare "security_rule".
	got := ExpandNestedChanges("azurerm_network_security_group", []ChangedAttribute{
		{Key: "security_rule", OldValue: []any{rule("a", 1, nil), rule("b", 2, nil)}, NewValue: []any{}},
	})
	if keysOf(got) != "security_rule[a].removed security_rule[b].removed" {
		t.Errorf("got %s", keysOf(got))
	}
}

func TestExpandNestedChanges_routeTable(t *testing.T) {
	r := func(name, prefix, hop string) map[string]any {
		return map[string]any{"name": name, "address_prefix": prefix, "next_hop_type": hop, "next_hop_in_ip_address": "10.0.0.4"}
	}
	got := ExpandNestedChanges("azurerm_route_table", []ChangedAttribute{
		{Key: "route", OldValue: []any{r("dflt", "0.0.0.0/0", "VirtualAppliance"), r("x", "10.1.0.0/16", "VnetLocal")},
			NewValue: []any{r("dflt", "0.0.0.0/0", "virtualappliance"), r("x", "10.2.0.0/16", "VnetLocal")}},
	})
	if keysOf(got) != "route[dflt].format route[x].address_prefix" {
		t.Errorf("got %s", keysOf(got))
	}
}

func TestExpandNestedChanges_flatRuleResource(t *testing.T) {
	got := ExpandNestedChanges("azurerm_network_security_rule", []ChangedAttribute{
		{Key: "source_address_prefix", OldValue: "10.0.0.0/8", NewValue: ""},
		{Key: "source_address_prefixes", OldValue: []any{}, NewValue: []any{"10.0.0.0/8"}},
		{Key: "protocol", OldValue: "Tcp", NewValue: "TCP"},
		{Key: "priority", OldValue: 100.0, NewValue: 110.0},
	})
	if keysOf(got) != "priority source_address_prefixes.format protocol.format" {
		t.Errorf("got %s", keysOf(got))
	}
	// Real membership change stays as-is.
	got = ExpandNestedChanges("azurerm_network_security_rule", []ChangedAttribute{
		{Key: "source_address_prefix", OldValue: "10.0.0.0/8", NewValue: ""},
		{Key: "source_address_prefixes", OldValue: []any{}, NewValue: []any{"10.0.0.0/8", "10.1.0.0/8"}},
	})
	if keysOf(got) != "source_address_prefix source_address_prefixes" {
		t.Errorf("membership change must not collapse, got %s", keysOf(got))
	}
}

func TestExpandNestedChanges_roundTripAndImpact(t *testing.T) {
	rc := ResourceChange{ResourceType: "azurerm_network_security_group", Action: ActionUpdate,
		ChangedAttributes: ExpandNestedChanges("azurerm_network_security_group", []ChangedAttribute{
			{Key: "security_rule", OldValue: []any{rule("a", 1, nil)}, NewValue: []any{rule("a", 1, map[string]any{"protocol": "TCP"})}},
		})}
	resolver := func(rt, key string) (Impact, bool) {
		if IsCosmeticKey(key) {
			return ImpactNone, true
		}
		return "", false
	}
	ClassifyImpact([]ResourceChange{rc}, nil, resolver)
	// The single format-only change resolves through the resolver to none.
	changes := []ResourceChange{rc}
	ClassifyImpact(changes, nil, resolver)
	if changes[0].Impact != ImpactNone {
		t.Errorf("format-only NSG change should be impact none, got %s", changes[0].Impact)
	}
}

func TestExpandResourceChanges_objectStyleRule(t *testing.T) {
	rc := ResourceChange{
		ResourceType: "azurerm_network_security_rule", Action: ActionUpdate,
		Before: map[string]any{"name": "allow-443", "priority": 100.0},
		After:  map[string]any{"name": "allow-443", "priority": 110.0},
		ChangedAttributes: []ChangedAttribute{
			{Key: "priority", OldValue: 100.0, NewValue: 110.0},
			{Key: "source_address_prefix", OldValue: "10.0.0.0/8", NewValue: ""},
			{Key: "source_address_prefixes", OldValue: []any{}, NewValue: []any{"10.0.0.0/8"}},
		},
	}
	ExpandResourceChanges(&rc)
	if got := keysOf(rc.ChangedAttributes); got != "security_rule[allow-443].priority security_rule[allow-443].source_address_prefixes.format" {
		t.Errorf("got %s", got)
	}
	if FieldOfAttributeKey("security_rule[allow-443].priority") != "priority" || FieldOfAttributeKey("priority") != "" {
		t.Error("FieldOfAttributeKey wrong")
	}
	if !IsCosmeticKey("security_rule[allow-443].source_address_prefixes.format") {
		t.Error("prefixed format key must stay cosmetic")
	}
	// Creates keep plain keys: the address already names the rule.
	rc2 := ResourceChange{ResourceType: "azurerm_network_security_rule", Action: ActionCreate,
		After: map[string]any{"name": "new"}, ChangedAttributes: []ChangedAttribute{{Key: "priority"}}}
	ExpandResourceChanges(&rc2)
	if keysOf(rc2.ChangedAttributes) != "priority" {
		t.Errorf("create must be untouched, got %s", keysOf(rc2.ChangedAttributes))
	}
}
