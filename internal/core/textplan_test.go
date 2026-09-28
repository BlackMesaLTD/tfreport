package core

import (
	"sort"
	"strings"
	"testing"
)

const sampleTextPlan = `
Terraform will perform the following actions:

  # module.vnet.azurerm_virtual_network.main will be updated in-place
  ~ resource "azurerm_virtual_network" "main" {
        id   = "/subscriptions/.../vnet"
      ~ tags = {
          + "env" = "prod"
        }
    }

  # module.vnet.azurerm_subnet.app will be updated in-place
  ~ resource "azurerm_subnet" "app" {
        id   = "/subscriptions/.../subnet-app"
      ~ address_prefixes = [
          - "10.0.1.0/24",
          + "10.0.2.0/24",
        ]
    }

  # module.nsg["app"].azurerm_network_security_group.nsg will be created
  + resource "azurerm_network_security_group" "nsg" {
        + id       = (known after apply)
        + name     = "nsg-app"
        + location = "westus2"
    }

  # azurerm_resource_group.rg must be replaced
  -/+ resource "azurerm_resource_group" "rg" {
        ~ name     = "rg-old" -> "rg-new"
        ~ location = "eastus" -> "westus2"
    }

Plan: 2 to add, 2 to change, 1 to destroy.
`

func TestParseTextPlan(t *testing.T) {
	blocks := ParseTextPlan(sampleTextPlan)

	// Expect 4 resource blocks.
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}

	// Check expected addresses exist.
	expectedAddrs := []string{
		"module.vnet.azurerm_virtual_network.main",
		"module.vnet.azurerm_subnet.app",
		`module.nsg["app"].azurerm_network_security_group.nsg`,
		"azurerm_resource_group.rg",
	}
	for _, addr := range expectedAddrs {
		block, ok := blocks[addr]
		if !ok {
			t.Errorf("missing block for address %q", addr)
			continue
		}
		// Each block should contain its marker line.
		if !strings.Contains(block, "# "+addr) {
			t.Errorf("block for %q does not contain its marker line", addr)
		}
	}

	// Verify the vnet block contains the tag change but NOT the subnet block.
	vnetBlock := blocks["module.vnet.azurerm_virtual_network.main"]
	if !strings.Contains(vnetBlock, `"env" = "prod"`) {
		t.Error("vnet block missing tag change")
	}
	if strings.Contains(vnetBlock, "address_prefixes") {
		t.Error("vnet block should not contain subnet content")
	}

	// Verify the bracket-address block was parsed correctly.
	nsgBlock := blocks[`module.nsg["app"].azurerm_network_security_group.nsg`]
	if !strings.Contains(nsgBlock, "nsg-app") {
		t.Error("nsg block missing expected content")
	}

	// Verify root resource block.
	rgBlock := blocks["azurerm_resource_group.rg"]
	if !strings.Contains(rgBlock, "rg-new") {
		t.Error("rg block missing expected content")
	}
}

func TestParseTextPlanEmpty(t *testing.T) {
	blocks := ParseTextPlan("")
	if len(blocks) != 0 {
		t.Errorf("expected empty map, got %d entries", len(blocks))
	}
}

func TestParseTextPlanNoMarkers(t *testing.T) {
	blocks := ParseTextPlan("No changes. Your infrastructure matches the configuration.")
	if len(blocks) != 0 {
		t.Errorf("expected empty map, got %d entries", len(blocks))
	}
}

func TestAggregateTextBlocks(t *testing.T) {
	blocks := ParseTextPlan(sampleTextPlan)

	groups := []ModuleGroup{
		{
			Name: "vnet",
			Path: "module.vnet",
			Changes: []ResourceChange{
				{Address: "module.vnet.azurerm_virtual_network.main"},
				{Address: "module.vnet.azurerm_subnet.app"},
			},
		},
		{
			Name: "nsg",
			Path: `module.nsg["app"]`,
			Changes: []ResourceChange{
				{Address: `module.nsg["app"].azurerm_network_security_group.nsg`},
			},
		},
		{
			Name: "(root)",
			Path: "(root)",
			Changes: []ResourceChange{
				{Address: "azurerm_resource_group.rg"},
			},
		},
	}

	aggregated := AggregateTextBlocks(blocks, groups)

	// All three groups should have aggregated text.
	if len(aggregated) != 3 {
		t.Fatalf("expected 3 aggregated groups, got %d", len(aggregated))
	}

	// module.vnet should contain both vnet resources.
	vnetText := aggregated["module.vnet"]
	if !strings.Contains(vnetText, "azurerm_virtual_network") {
		t.Error("vnet aggregate missing virtual_network block")
	}
	if !strings.Contains(vnetText, "azurerm_subnet") {
		t.Error("vnet aggregate missing subnet block")
	}

	// module.nsg["app"] should contain the NSG resource.
	nsgText := aggregated[`module.nsg["app"]`]
	if !strings.Contains(nsgText, "nsg-app") {
		t.Error("nsg aggregate missing nsg block content")
	}

	// (root) should contain the resource group.
	rootText := aggregated["(root)"]
	if !strings.Contains(rootText, "azurerm_resource_group") {
		t.Error("root aggregate missing resource_group block")
	}

	// Verify vnet aggregate has exactly 2 marker lines.
	markerCount := strings.Count(vnetText, "# module.vnet.")
	if markerCount != 2 {
		t.Errorf("expected 2 marker lines in vnet aggregate, got %d", markerCount)
	}
}

func TestAggregateTextBlocksNoMatch(t *testing.T) {
	blocks := map[string]string{
		"module.other.azurerm_thing.x": "# module.other.azurerm_thing.x will be created\n  + resource ...",
	}
	groups := []ModuleGroup{
		{Name: "vnet", Path: "module.vnet"},
	}

	aggregated := AggregateTextBlocks(blocks, groups)
	if len(aggregated) != 0 {
		t.Errorf("expected no aggregated groups, got %d", len(aggregated))
	}
}

func TestBelongsToGroup(t *testing.T) {
	tests := []struct {
		addr      string
		groupPath string
		want      bool
	}{
		{"module.vnet.azurerm_virtual_network.main", "module.vnet", true},
		{"module.vnet.azurerm_subnet.app", "module.vnet", true},
		{"module.other.azurerm_thing.x", "module.vnet", false},
		{"azurerm_resource_group.rg", "(root)", true},
		{"module.vnet.azurerm_virtual_network.main", "(root)", false},
		{`module.nsg["app"].azurerm_nsg.x`, `module.nsg["app"]`, true},
		{"azurerm_resource_group.rg", "", true},
	}

	for _, tt := range tests {
		got := belongsToGroup(tt.addr, tt.groupPath)
		if got != tt.want {
			t.Errorf("belongsToGroup(%q, %q) = %v, want %v", tt.addr, tt.groupPath, got, tt.want)
		}
	}
}

func TestTextToDiff(t *testing.T) {
	input := `  ~ resource "azurerm_virtual_network" "main" {
        id   = "/subscriptions/.../vnet"
      ~ tags = {
          + "env" = "prod"
          - "old" = "tag"
        }
    }`

	got := TextToDiff(input)
	lines := strings.Split(got, "\n")

	// ~ should become ! at column 0 with original indentation preserved after
	if !strings.HasPrefix(lines[0], "!") {
		t.Errorf("line 0: expected ! prefix, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "resource") {
		t.Errorf("line 0: expected resource content, got %q", lines[0])
	}

	// Unchanged line (id =) should get leading space (context line)
	if !strings.HasPrefix(lines[1], " ") {
		t.Errorf("line 1: expected leading space for context, got %q", lines[1])
	}

	// ~ tags should become !
	if !strings.HasPrefix(lines[2], "!") {
		t.Errorf("line 2: expected ! prefix for ~ tags, got %q", lines[2])
	}

	// + should move to column 0
	if !strings.HasPrefix(lines[3], "+") {
		t.Errorf("line 3: expected + prefix, got %q", lines[3])
	}

	// - should move to column 0
	if !strings.HasPrefix(lines[4], "-") {
		t.Errorf("line 4: expected - prefix, got %q", lines[4])
	}
}

func TestTextToDiffNoFalsePositives(t *testing.T) {
	// Values containing +/- after = should not be transformed
	input := `      + cidr_block = "10.0.0.0/8"
        name       = "test-rule-allow+deny"`

	got := TextToDiff(input)

	// The + before = should be converted
	if !strings.HasPrefix(strings.TrimLeft(got, " "), "+") {
		t.Error("expected leading + to be converted")
	}

	// The + in "allow+deny" value should NOT be affected
	if !strings.Contains(got, "allow+deny") {
		t.Error("+ in value after = should not be modified")
	}
}

func TestParseTextPlanDataSource(t *testing.T) {
	text := `
  # data.azurerm_client_config.current will be read during apply
  # (config refers to values not yet known)
  <= data "azurerm_client_config" "current" {
        + client_id    = (known after apply)
        + tenant_id    = (known after apply)
    }

  # module.vnet.azurerm_virtual_network.main will be updated in-place
  ~ resource "azurerm_virtual_network" "main" {
        id = "/subscriptions/.../vnet"
    }
`
	blocks := ParseTextPlan(text)

	if len(blocks) != 2 {
		var addrs []string
		for a := range blocks {
			addrs = append(addrs, a)
		}
		sort.Strings(addrs)
		t.Fatalf("expected 2 blocks, got %d: %v", len(blocks), addrs)
	}

	if _, ok := blocks["data.azurerm_client_config.current"]; !ok {
		t.Error("missing block for data source")
	}
	if _, ok := blocks["module.vnet.azurerm_virtual_network.main"]; !ok {
		t.Error("missing block for vnet resource")
	}
}

const collapseSample = `  # module.nsg["app"].azurerm_network_security_group.main will be updated in-place
  ~ resource "azurerm_network_security_group" "main" {
        id                  = "/subscriptions/x/nsg"
        name                = "nsg-app"
      ~ tags                = {
          - "BusinessUnit"     = "DTS" -> null
            "cost_centre"      = "cc-0001"
            "team"             = "platform"
            "deployed_by"      = "pipeline"
            "Environment"      = "Production"
            "ServiceOwner"     = "Someone"
        }
        # (3 unchanged attributes hidden)
    }`

func TestCollapseUnchanged_keepOne(t *testing.T) {
	got := CollapseUnchanged(collapseSample, 1)
	want := `  # module.nsg["app"].azurerm_network_security_group.main will be updated in-place
  ~ resource "azurerm_network_security_group" "main" {
        id                  = "/subscriptions/x/nsg"
        name                = "nsg-app"
      ~ tags                = {
          - "BusinessUnit"     = "DTS" -> null
            "cost_centre"      = "cc-0001"
            # ... (6 unchanged lines hidden)
    }`
	if got != want {
		t.Errorf("keep=1 mismatch\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func TestCollapseUnchanged_keepZero(t *testing.T) {
	got := CollapseUnchanged(collapseSample, 0)
	want := `  # module.nsg["app"].azurerm_network_security_group.main will be updated in-place
  ~ resource "azurerm_network_security_group" "main" {
        # ... (2 unchanged lines hidden)
      ~ tags                = {
          - "BusinessUnit"     = "DTS" -> null
            # ... (8 unchanged lines hidden)`
	if got != want {
		t.Errorf("keep=0 mismatch\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func TestCollapseUnchanged_disabledAndNoGain(t *testing.T) {
	if got := CollapseUnchanged(collapseSample, -1); got != collapseSample {
		t.Error("keep<0 must return input unchanged")
	}
	// Two context lines between changes with keep=1 → hidden would be 0; no marker.
	small := "      ~ a = 1 -> 2\n        b = 1\n        c = 2\n      ~ d = 1 -> 2"
	if got := CollapseUnchanged(small, 1); got != small {
		t.Errorf("no-gain run must be left alone, got:\n%s", got)
	}
	// Exactly one hidden line would not save anything either.
	three := "      ~ a = 1 -> 2\n        b = 1\n        c = 2\n        e = 3\n      ~ d = 1 -> 2"
	if got := CollapseUnchanged(three, 1); got != three {
		t.Errorf("single-line saving must not collapse, got:\n%s", got)
	}
	if got := CollapseUnchanged("", 1); got != "" {
		t.Error("empty input must stay empty")
	}
}

func TestCollapseUnchanged_keepsMarkersAndSpecialHeaders(t *testing.T) {
	text := `  # azurerm_resource_group.rg must be replaced
  -/+ resource "azurerm_resource_group" "rg" {
        a = 1
        b = 2
        c = 3
        d = 4
      ~ name = "old" -> "new"
    }

  # module.x.data.azurerm_virtual_network.main will be read during apply
  # (depends on a resource or a module with changes pending)
 <= data "azurerm_virtual_network" "main" {
      + id = (known after apply)
    }`
	got := CollapseUnchanged(text, 0)
	for _, must := range []string{
		"must be replaced", `-/+ resource "azurerm_resource_group"`, `<= data "azurerm_virtual_network"`,
		"will be read during apply", `~ name = "old" -> "new"`, "# ... (4 unchanged lines hidden)",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("collapsed output lost %q:\n%s", must, got)
		}
	}
	// Idempotent: collapsing twice must not stack markers.
	if again := CollapseUnchanged(got, 0); again != got {
		t.Errorf("collapse is not idempotent:\n--- once\n%s\n--- twice\n%s", got, again)
	}
}

func TestCollapseUnchanged_thenTextToDiff(t *testing.T) {
	diff := TextToDiff(CollapseUnchanged(collapseSample, 1))
	if !strings.Contains(diff, "\n             # ... (6 unchanged lines hidden)") {
		t.Errorf("marker must survive TextToDiff as a context line:\n%s", diff)
	}
	if !strings.Contains(diff, "\n-           \"BusinessUnit\"") {
		t.Errorf("change line must still get a column-0 symbol:\n%s", diff)
	}
}

func TestCollapseUnchanged_neverGrows(t *testing.T) {
	// Four one-character context lines: the marker would be longer than
	// the lines it hides, so the run must be left verbatim.
	short := "      ~ a = 1 -> 2\n  b\n  c\n  d\n  e\n      ~ f = 1 -> 2"
	if got := CollapseUnchanged(short, 0); got != short {
		t.Errorf("collapse must never grow the block, got:\n%s", got)
	}
	if got := CollapseUnchanged(collapseSample, 0); len(got) >= len(collapseSample) {
		t.Errorf("realistic block should shrink: %d -> %d", len(collapseSample), len(got))
	}
}
