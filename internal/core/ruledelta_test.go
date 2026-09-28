package core

import (
	"strings"
	"testing"
)

func TestBuildRuleDiff_inlineNSG(t *testing.T) {
	before := []any{
		rule("keep", 100, nil),
		rule("gone", 200, nil),
		rule("prio", 300, nil),
		rule("shape", 400, map[string]any{"source_address_prefix": "10.42.4.0/24"}),
	}
	after := []any{
		rule("keep", 100, nil),
		rule("prio", 310, nil),
		rule("shape", 400, map[string]any{"source_address_prefix": "", "source_address_prefixes": []any{"10.42.4.0/24"}}),
		rule("new", 500, nil),
	}
	rc := &ResourceChange{ResourceType: "azurerm_network_security_group", Action: ActionUpdate,
		Before: map[string]any{"security_rule": before, "tags": map[string]any{}},
		After:  map[string]any{"security_rule": after, "tags": map[string]any{}}}
	got := BuildRuleDiff(rc)
	for _, want := range []string{
		`  # security_rule "gone" — removed: allow inbound tcp port 443,80 from * to 10.0.0.0/8`,
		`  - security_rule "gone" {`,
		`      - access                       = "Allow"`,
		`  # security_rule "new" — added: allow inbound tcp port 443,80 from * to 10.0.0.0/8`,
		`  + security_rule "new" {`,
		`  # security_rule "prio" — priority 300 → 310`,
		`  ~ security_rule "prio" {`,
		`      ~ priority = 300 -> 310`,
		`  # security_rule "shape" — same members, written as a list instead of a string — functionally identical`,
		`      - source_address_prefix   = "10.42.4.0/24" -> null`,
		`      + source_address_prefixes = ["10.42.4.0/24"]`,
		`        # (7 unchanged attributes hidden)`,
		`  # (1 unchanged security_rule hidden)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Symbols sit where TextToDiff expects them.
	diff := TextToDiff(got)
	if !strings.Contains(diff, "\n!   security_rule \"prio\" {") || !strings.Contains(diff, "\n-       source_address_prefix") {
		t.Errorf("TextToDiff did not lift symbols:\n%s", diff)
	}
	// Not an update → nothing.
	rc.Action = ActionCreate
	if BuildRuleDiff(rc) != "" {
		t.Error("create must yield no rule diff")
	}
}

func TestBuildRuleDiff_routeTableAndFlat(t *testing.T) {
	r := func(name, prefix, hop string) map[string]any {
		return map[string]any{"name": name, "address_prefix": prefix, "next_hop_type": hop, "next_hop_in_ip_address": "10.0.0.4"}
	}
	rt := &ResourceChange{ResourceType: "azurerm_route_table", Action: ActionUpdate,
		Before: map[string]any{"route": []any{r("dflt", "0.0.0.0/0", "VirtualAppliance")}},
		After:  map[string]any{"route": []any{r("dflt", "0.0.0.0/0", "VirtualAppliance"), r("dns", "10.9.0.0/16", "VnetLocal")}}}
	got := BuildRuleDiff(rt)
	if !strings.Contains(got, `  + route "dns" {`) || !strings.Contains(got, `  # (1 unchanged route hidden)`) {
		t.Errorf("route table diff:\n%s", got)
	}
	flat := &ResourceChange{ResourceType: "azurerm_network_security_rule", Action: ActionUpdate,
		Before: map[string]any{"name": "intra-in", "priority": 2000.0, "source_address_prefix": "10.1.0.0/24", "source_address_prefixes": []any{}, "protocol": "*"},
		After:  map[string]any{"name": "intra-in", "priority": 2000.0, "source_address_prefix": nil, "source_address_prefixes": []any{"10.1.0.0/24"}, "protocol": "*"}}
	got = BuildRuleDiff(flat)
	for _, want := range []string{
		`  # security_rule "intra-in" — same members, written as a list instead of a string — functionally identical`,
		`  ~ security_rule "intra-in" {`,
		`      - source_address_prefix   = "10.1.0.0/24" -> null`,
		`      + source_address_prefixes = ["10.1.0.0/24"]`,
		`        # (2 unchanged attributes hidden)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRuleDiffs_roundTrip(t *testing.T) {
	r := &Report{RuleDiffs: map[string]string{"a": "  ~ security_rule \"x\" {\n    }"}}
	data, err := MarshalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.RuleDiffs["a"] != r.RuleDiffs["a"] {
		t.Errorf("rule diffs lost in round trip: %q", back.RuleDiffs)
	}
}
