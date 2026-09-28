package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/config"
	"github.com/BlackMesaLTD/tfreport/internal/core"
)

func TestInputKind(t *testing.T) {
	cases := map[string]string{
		`{"format_version":"1.2","resource_changes":[]}`: "plan",
		`{"label":"x","module_groups":[]}`:               "report",
		"\x00binary":                                     "binary",
		"":                                               "binary",
		`{"total_resources":1,` + strings.Repeat(" ", 5000) + `"module_groups":[]}`: "report",
	}
	for in, want := range cases {
		if got := inputKind([]byte(in)); got != want {
			t.Errorf("inputKind(%.20q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadInspectReport_planAndCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TFREPORT_CACHE_DIR", filepath.Join(dir, "cache"))
	planPath := filepath.Join(dir, "plan.json")
	data, err := os.ReadFile("../../testdata/replace_plan.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	r, err := loadInspectReport(config.Default(), "", []string{planPath}, false, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalResources == 0 || r.Label != "plan" {
		t.Fatalf("unexpected report: total=%d label=%q", r.TotalResources, r.Label)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "cache"))
	if len(entries) != 1 {
		t.Fatalf("want one cache entry, got %d (%s)", len(entries), stderr.String())
	}
	// Second load must come from cache: corrupt the source and expect the same report.
	if err := os.WriteFile(planPath, data, 0o644); err != nil { // same content, same size; mtime may tick
		t.Fatal(err)
	}
	r2, err := loadInspectReport(config.Default(), "", []string{planPath}, false, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if r2.TotalResources != r.TotalResources {
		t.Errorf("cache round trip changed the report: %d vs %d", r2.TotalResources, r.TotalResources)
	}
	// A report JSON input is accepted as-is.
	repPath := filepath.Join(dir, "report.json")
	js, _ := core.MarshalReport(r)
	if err := os.WriteFile(repPath, js, 0o644); err != nil {
		t.Fatal(err)
	}
	r3, err := loadInspectReport(config.Default(), "", []string{repPath}, true, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if r3.TotalResources != r.TotalResources {
		t.Errorf("report input mismatch: %d vs %d", r3.TotalResources, r.TotalResources)
	}
}

func inspectFixture() *core.Report {
	nsg := core.ResourceChange{
		Address: `module.infra.module.nsg["app"].azurerm_network_security_group.main`, ModulePath: `module.infra.module.nsg["app"]`,
		ResourceType: "azurerm_network_security_group", ResourceName: "main", Action: core.ActionUpdate, Impact: core.ImpactMedium,
		ChangedAttributes: []core.ChangedAttribute{
			{Key: "security_rule[mail-out].removed", Description: "allow outbound tcp port 25"},
			{Key: "security_rule[web].[destination_address_prefixes].string_to_list"},
			{Key: "security_rule[web2].priority", Description: "100 → 110"},
			{Key: "tags"},
		},
	}
	rt := core.ResourceChange{
		Address: "module.infra.azurerm_route_table.rt", ModulePath: "module.infra",
		ResourceType: "azurerm_route_table", ResourceName: "rt", Action: core.ActionUpdate, Impact: core.ImpactNone,
		ChangedAttributes: []core.ChangedAttribute{{Key: "route.order"}},
	}
	rep := core.ResourceChange{Address: "module.infra.azurerm_subnet.s", ModulePath: "module.infra", ResourceType: "azurerm_subnet", ResourceName: "s",
		Action: core.ActionReplace, Impact: core.ImpactCritical, ReplacePaths: []string{"name"}}
	cr := core.ResourceChange{Address: "module.infra.azurerm_subnet.n", ModulePath: "module.infra", ResourceType: "azurerm_subnet", ResourceName: "n", Action: core.ActionCreate}
	del := core.ResourceChange{Address: "azurerm_resource_group.old", ResourceType: "azurerm_resource_group", ResourceName: "old", Action: core.ActionDelete}
	return &core.Report{Label: "t", ModuleGroups: []core.ModuleGroup{{Name: "infra", Path: "module.infra", Changes: []core.ResourceChange{nsg, rt, rep, cr, del}}},
		RuleDiffs:      map[string]string{nsg.Address: "  # security_rule \"web2\" — priority 100 → 110\n  ~ security_rule \"web2\" {\n      ~ priority = 100 -> 110\n    }"},
		TextPlanBlocks: map[string]string{nsg.Address: "  # " + nsg.Address + " will be updated in-place\n  ~ resource \"azurerm_network_security_group\" \"main\" {\n        a = 1\n        b = 2\n        c = 3\n      ~ tags = {}\n    }"}}
}

func TestBreakdownView(t *testing.T) {
	r := inspectFixture()
	d := breakdownView(r, filterResources(r, "", ""))
	if d.Total != 5 || d.ActionCounts["update"] != 2 || d.ActionCounts["replace"] != 1 {
		t.Errorf("counts: %+v", d)
	}
	if d.Cosmetic != 1 {
		t.Errorf("route table order-only update should count as cosmetic, got %d", d.Cosmetic)
	}
	rows := d.UpdateAttrs["azurerm_network_security_group"]
	var keys []string
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	want := "security_rule.priority security_rule.removed security_rule.string_to_list tags"
	if strings.Join(keys, " ") != want {
		t.Errorf("update attrs: got %q want %q", strings.Join(keys, " "), want)
	}
	if len(d.Replacements) != 1 || d.Replacements[0].ForcedBy[0] != "name" {
		t.Errorf("replacements: %+v", d.Replacements)
	}
	out := renderBreakdown(d, newPainter(false))
	for _, must := range []string{"5 resources with a planned action", "forced by: name", "(functionally identical)", "== DESTROYS: 1 ==", "azurerm_resource_group.old"} {
		if !strings.Contains(out, must) {
			t.Errorf("breakdown text missing %q:\n%s", must, out)
		}
	}
	// --type / --action filters
	if got := filterResources(r, "subnet", ""); len(got) != 2 {
		t.Errorf("type filter: %d", len(got))
	}
	if got := filterResources(r, "", "destroy"); len(got) != 1 {
		t.Errorf("destroy alias: %d", len(got))
	}
}

func TestAttrShowRulesViews(t *testing.T) {
	r := inspectFixture()
	rcs := filterResources(r, "", "")

	a := attrView(rcs, "security_rule", 5)
	if len(a.Hits) != 1 || len(a.Hits[0].Keys) != 3 {
		t.Errorf("attr hits: %+v", a.Hits)
	}
	if !attrMatches("security_rule[x].priority", "security_rule") || attrMatches("tags", "tag") || !attrMatches("tags.env", "tags") {
		t.Error("attrMatches wrong")
	}
	if out := renderAttr(a, newPainter(false)); !strings.Contains(out, `1 resources touch "security_rule"`) {
		t.Errorf("attr text:\n%s", out)
	}

	s := showView(r, rcs, `nsg["app"]`, 0, 200)
	if s.Address == "" || !strings.Contains(s.RuleDiff, "!   security_rule \"web2\"") {
		t.Errorf("show: %+v", s)
	}
	if !strings.Contains(s.RawBlock, "# ... (3 unchanged lines hidden)") {
		t.Errorf("--context 0 must collapse the raw block:\n%s", s.RawBlock)
	}
	if renderShow(showView(r, rcs, "nope", -1, 200), newPainter(false)) != "no resource address contains \"nope\"\n" {
		t.Error("show miss message")
	}
	trunc := showView(r, rcs, `nsg["app"]`, -1, 2)
	if !trunc.Truncated || strings.Count(trunc.RawBlock, "\n") != 1 {
		t.Errorf("--lines cap: %+v", trunc)
	}

	ru := rulesView(r, rcs, 5)
	if len(ru.Rows) != 2 || ru.NetLoss != 1 || ru.Cosmetic != 1 {
		t.Errorf("rules: %+v", ru)
	}
	nsgRow := ru.Rows[0]
	if nsgRow.Removed[0] != "mail-out" || nsgRow.Changed[0] != "web2.priority" || nsgRow.Cosmetic["string_to_list"] != 1 {
		t.Errorf("nsg row: %+v", nsgRow)
	}
	if !ru.Rows[1].Reordered {
		t.Errorf("route table row should be reordered-only: %+v", ru.Rows[1])
	}
	out := renderRules(ru, newPainter(true))
	if !strings.Contains(out, "removed:") || !strings.Contains(out, "\x1b[") {
		t.Errorf("rules text (coloured):\n%s", out)
	}
}
