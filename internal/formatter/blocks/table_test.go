package blocks

import (
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// buildTableCtx produces a BlockContext with a fabricated PlanTree
// suitable for table block tests. Mirrors the shape of a small multi-
// module plan: one root-module resource (create), one nested update,
// one replace, one import-create. Covers every action relevant to the
// `impact` and `action` columns.
func buildTableCtx(t *testing.T) *BlockContext {
	t.Helper()
	changes := []core.ResourceChange{
		{
			Address: "azurerm_resource_group.rg", ModulePath: "",
			ResourceType: "azurerm_resource_group", ResourceName: "rg",
			Action: core.ActionCreate, Impact: core.ImpactLow,
			ChangedAttributes: []core.ChangedAttribute{{Key: "location"}, {Key: "tags"}},
		},
		{
			Address: "module.vnet.azurerm_virtual_network.hub",
			ModulePath: "module.vnet",
			ResourceType: "azurerm_virtual_network", ResourceName: "hub",
			Action: core.ActionUpdate, Impact: core.ImpactMedium,
			ChangedAttributes: []core.ChangedAttribute{{Key: "address_space"}},
		},
		{
			Address: "module.compute.azurerm_network_interface.vm",
			ModulePath: "module.compute",
			ResourceType: "azurerm_network_interface", ResourceName: "vm",
			Action: core.ActionReplace, Impact: core.ImpactCritical,
			ChangedAttributes: []core.ChangedAttribute{{Key: "ip_configuration"}},
		},
		{
			Address: "module.dns.azurerm_private_dns_zone.internal",
			ModulePath: "module.dns",
			ResourceType: "azurerm_private_dns_zone", ResourceName: "internal",
			Action: core.ActionCreate, Impact: core.ImpactLow, IsImport: true,
			ChangedAttributes: []core.ChangedAttribute{{Key: "name"}},
		},
	}
	r := &core.Report{
		ModuleGroups: core.GroupByModule(changes),
		KeyChanges:   []core.KeyChange{{Text: "Replacing network interface", Impact: core.ImpactCritical}},
	}
	return &BlockContext{
		Target: "markdown",
		Report: r,
		Tree:   core.BuildTree(r),
	}
}

func TestTable_RequiresSource(t *testing.T) {
	ctx := buildTableCtx(t)
	_, err := (Table{}).Render(ctx, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "source is required") {
		t.Errorf("want source-required error, got %v", err)
	}
}

func TestTable_UnknownPathErrors(t *testing.T) {
	ctx := buildTableCtx(t)
	_, err := (Table{}).Render(ctx, map[string]any{"source": "bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Errorf("want unknown-kind error, got %v", err)
	}
}

func TestTable_UnsupportedGroupArgErrors(t *testing.T) {
	ctx := buildTableCtx(t)
	_, err := (Table{}).Render(ctx, map[string]any{"source": "resource", "group": "self.action"})
	if err == nil || !strings.Contains(err.Error(), "group arg is not yet supported") {
		t.Errorf("want group-unsupported error, got %v", err)
	}
}

func TestTable_UnknownColumnErrors(t *testing.T) {
	ctx := buildTableCtx(t)
	_, err := (Table{}).Render(ctx, map[string]any{"source": "resource", "columns": "address,bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown column") {
		t.Errorf("want unknown-column error, got %v", err)
	}
}

func TestTable_NilTreeEmitsEmptyOrFallback(t *testing.T) {
	ctx := &BlockContext{Target: "markdown"} // no Tree
	out, err := (Table{}).Render(ctx, map[string]any{"source": "resource", "empty": "no data"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no data" {
		t.Errorf("got %q, want %q", out, "no data")
	}
}

func TestTable_ResourceDefaults(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{"source": "resource"})
	if err != nil {
		t.Fatal(err)
	}

	// Header present
	if !strings.Contains(out, "| Resource | Action | Impact |") {
		t.Errorf("missing default headers:\n%s", out)
	}
	// All four resource addresses present
	for _, addr := range []string{
		"`azurerm_resource_group.rg`",
		"`module.vnet.azurerm_virtual_network.hub`",
		"`module.compute.azurerm_network_interface.vm`",
		"`module.dns.azurerm_private_dns_zone.internal`",
	} {
		if !strings.Contains(out, addr) {
			t.Errorf("missing address %s in:\n%s", addr, out)
		}
	}
	// Replace row should carry critical impact
	if !strings.Contains(out, "critical") {
		t.Errorf("missing critical impact marker:\n%s", out)
	}
}

func TestTable_WhereFilters(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{
		"source": "resource",
		"where":  `self.impact == "critical"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only nic should survive
	if !strings.Contains(out, "network_interface.vm") {
		t.Errorf("nic missing after filter:\n%s", out)
	}
	if strings.Contains(out, "resource_group.rg") {
		t.Errorf("rg unexpectedly present after critical filter:\n%s", out)
	}
}

func TestTable_Sort(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{
		"source": "resource",
		"sort":   "self.name",
	})
	if err != nil {
		t.Fatal(err)
	}
	// rg has the lexicographically-smallest address — it should be the
	// first row after the header/separator.
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("not enough lines:\n%s", out)
	}
	if !strings.Contains(lines[2], "resource_group.rg") {
		t.Errorf("first data row = %q, want rg first", lines[2])
	}
}

func TestTable_Limit(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{
		"source": "resource",
		"limit":  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// header (2 lines) + 2 data rows = 4 newline-separated lines
	dataLines := 0
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "| `") {
			dataLines++
		}
	}
	if dataLines != 2 {
		t.Errorf("got %d data rows, want 2:\n%s", dataLines, out)
	}
}

func TestTable_Heading(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{
		"source":  "resource",
		"heading": "Resources",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "### Resources\n\n") {
		t.Errorf("missing heading prefix:\n%s", out)
	}
}

func TestTable_EmptyResult(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{
		"source": "resource",
		"where":  `self.impact == "nope"`,
		"empty":  "_no matches_",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "_no matches_" {
		t.Errorf("empty output = %q, want %q", out, "_no matches_")
	}
}

func TestTable_AttributeSource(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{
		"source":  "attribute",
		"columns": "key,description",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| Attribute | Description |") {
		t.Errorf("missing attribute headers:\n%s", out)
	}
	for _, k := range []string{"`location`", "`tags`", "`address_space`", "`ip_configuration`", "`name`"} {
		if !strings.Contains(out, k) {
			t.Errorf("missing attr key %s:\n%s", k, out)
		}
	}
}

func TestTable_KeyChangeSource(t *testing.T) {
	ctx := buildTableCtx(t)
	out, err := (Table{}).Render(ctx, map[string]any{"source": "key_change"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Replacing network interface") {
		t.Errorf("missing key change text:\n%s", out)
	}
	if !strings.Contains(out, "critical") {
		t.Errorf("missing impact:\n%s", out)
	}
}

func TestTable_ChainedPathSelector(t *testing.T) {
	ctx := buildTableCtx(t)
	// module_instance > resource excludes root-module rg.
	out, err := (Table{}).Render(ctx, map[string]any{"source": "module_instance > resource"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "resource_group.rg") {
		t.Errorf("rg should be excluded:\n%s", out)
	}
	if !strings.Contains(out, "network_interface.vm") {
		t.Errorf("nic should be included:\n%s", out)
	}
}
