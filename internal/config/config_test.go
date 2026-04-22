package config

import (
	"strings"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Output.MaxResourcesInSummary != 50 {
		t.Errorf("max resources = %d, want 50", cfg.Output.MaxResourcesInSummary)
	}
	if cfg.Output.CodeFormat != "diff" {
		t.Errorf("code_format = %q, want diff", cfg.Output.CodeFormat)
	}
}

func TestParse(t *testing.T) {
	yaml := []byte(`
presets:
  - azurerm
modules:
  virtual_network:
    description: "Managed VNet"
global_attributes:
  tags:
    impact: none
    note: "Cosmetic only"
resources:
  azurerm_virtual_network:
    display_name: "virtual network"
    attributes:
      name:
        impact: critical
        note: "Forces replacement"
impact_defaults:
  replace: critical
  delete: high
  update: medium
  create: low
output:
  max_resources_in_summary: 100
  code_format: hcl
`)

	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(cfg.Presets) != 1 || cfg.Presets[0] != "azurerm" {
		t.Errorf("presets = %v, want [azurerm]", cfg.Presets)
	}

	if cfg.Modules["virtual_network"].Description != "Managed VNet" {
		t.Errorf("module description = %q", cfg.Modules["virtual_network"].Description)
	}

	if cfg.GlobalAttributes["tags"].Impact != "none" {
		t.Errorf("tags impact = %q", cfg.GlobalAttributes["tags"].Impact)
	}

	if cfg.Resources["azurerm_virtual_network"].Attributes["name"].Impact != "critical" {
		t.Errorf("vnet name impact = %q", cfg.Resources["azurerm_virtual_network"].Attributes["name"].Impact)
	}

	if cfg.Output.MaxResourcesInSummary != 100 {
		t.Errorf("max resources = %d, want 100", cfg.Output.MaxResourcesInSummary)
	}
	if cfg.Output.CodeFormat != "hcl" {
		t.Errorf("code_format = %q, want hcl", cfg.Output.CodeFormat)
	}
}

func TestParse_targetsTemplate(t *testing.T) {
	yaml := []byte(`
output:
  targets:
    github-pr-comment:
      template: |
        Custom {{ .Title }}
    markdown:
      sections:
        hide: [footer]
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tc := cfg.Output.Targets["github-pr-comment"]
	if !strings.Contains(tc.Template, "Custom") {
		t.Errorf("template not parsed: %q", tc.Template)
	}
	md := cfg.Output.Targets["markdown"]
	if len(md.Sections.Hide) != 1 || md.Sections.Hide[0] != "footer" {
		t.Errorf("sections.hide = %v, want [footer]", md.Sections.Hide)
	}
}

func TestParse_rejectsTemplateAndFile(t *testing.T) {
	yaml := []byte(`
output:
  targets:
    markdown:
      template: "x"
      template_file: "y.tmpl"
`)
	_, err := Parse(yaml)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("want mutex error, got %v", err)
	}
}

func TestParse_rejectsTemplateAndSections(t *testing.T) {
	yaml := []byte(`
output:
  targets:
    markdown:
      template: "x"
      sections:
        show: [title]
`)
	_, err := Parse(yaml)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("want mutex error, got %v", err)
	}
}

func TestParse_rejectsShowAndHide(t *testing.T) {
	yaml := []byte(`
output:
  targets:
    markdown:
      sections:
        show: [title]
        hide: [footer]
`)
	_, err := Parse(yaml)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("want mutex error, got %v", err)
	}
}

func TestParse_preserveStrict(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		cfg, err := Parse([]byte("output:\n  preserve_strict: true\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Output.PreserveStrict {
			t.Error("preserve_strict: true should parse to cfg.Output.PreserveStrict == true")
		}
	})
	t.Run("default false", func(t *testing.T) {
		cfg, err := Parse([]byte("output: {}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Output.PreserveStrict {
			t.Error("unset preserve_strict should default to false")
		}
	})
}

func TestAttributeImpactResolution(t *testing.T) {
	cfg := Config{
		GlobalAttributes: map[string]AttributeConfig{
			"tags": {Impact: "none"},
		},
		Resources: map[string]ResourceConfig{
			"azurerm_virtual_network": {
				Attributes: map[string]AttributeConfig{
					"name":          {Impact: "critical"},
					"address_space": {Impact: "high"},
				},
			},
		},
	}

	impact, ok := cfg.AttributeImpact("azurerm_virtual_network", "name")
	if !ok || impact != core.ImpactCritical {
		t.Errorf("vnet name = %q/%v, want critical/true", impact, ok)
	}

	impact, ok = cfg.AttributeImpact("azurerm_virtual_network", "tags")
	if !ok || impact != core.ImpactNone {
		t.Errorf("vnet tags = %q/%v, want none/true", impact, ok)
	}

	impact, ok = cfg.AttributeImpact("azurerm_subnet", "tags")
	if !ok || impact != core.ImpactNone {
		t.Errorf("subnet tags = %q/%v, want none/true", impact, ok)
	}

	_, ok = cfg.AttributeImpact("azurerm_subnet", "address_prefixes")
	if ok {
		t.Error("expected not found for subnet address_prefixes")
	}
}

func TestImpactOverrides(t *testing.T) {
	cfg := Default()
	overrides := cfg.ImpactOverrides()

	if overrides[core.ActionReplace] != core.ImpactCritical {
		t.Errorf("replace = %q, want critical", overrides[core.ActionReplace])
	}
	if overrides[core.ActionDelete] != core.ImpactHigh {
		t.Errorf("delete = %q, want high", overrides[core.ActionDelete])
	}
}

func TestModuleDescription(t *testing.T) {
	cfg := Config{
		Modules: map[string]ModuleConfig{
			"vnet": {Description: "My VNet"},
		},
	}

	if got := cfg.ModuleDescription("vnet"); got != "My VNet" {
		t.Errorf("got %q, want %q", got, "My VNet")
	}
	if got := cfg.ModuleDescription("unknown"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestEffectiveOutput_noOverride(t *testing.T) {
	cfg := Config{
		Output: OutputConfig{
			MaxResourcesInSummary: 50,
			CodeFormat:            "diff",
			StepSummaryMaxKB:      800,
		},
	}
	out := cfg.EffectiveOutput("github-step-summary")
	if out.MaxResourcesInSummary != 50 || out.CodeFormat != "diff" || out.StepSummaryMaxKB != 800 {
		t.Errorf("unexpected inherit: %+v", out)
	}
}

func TestEffectiveOutput_perTargetOverride(t *testing.T) {
	max := 10
	kb := 400
	groupSubs := true
	depth := 2
	cfg := Config{
		Output: OutputConfig{
			MaxResourcesInSummary: 50,
			GroupSubmodules:       false,
			SubmoduleDepth:        1,
			StepSummaryMaxKB:      800,
			CodeFormat:            "diff",
			Targets: map[string]TargetConfig{
				"github-pr-comment": {
					CodeFormat:            "plain",
					MaxResourcesInSummary: &max,
				},
				"github-step-summary": {
					StepSummaryMaxKB: &kb,
					GroupSubmodules:  &groupSubs,
					SubmoduleDepth:   &depth,
				},
			},
		},
	}

	comment := cfg.EffectiveOutput("github-pr-comment")
	if comment.CodeFormat != "plain" {
		t.Errorf("pr-comment code_format = %q, want plain", comment.CodeFormat)
	}
	if comment.MaxResourcesInSummary != 10 {
		t.Errorf("pr-comment max = %d, want 10", comment.MaxResourcesInSummary)
	}
	if comment.StepSummaryMaxKB != 800 {
		t.Errorf("pr-comment unrelated knob drifted: %d", comment.StepSummaryMaxKB)
	}

	step := cfg.EffectiveOutput("github-step-summary")
	if step.StepSummaryMaxKB != 400 {
		t.Errorf("step-summary budget = %d, want 400", step.StepSummaryMaxKB)
	}
	if !step.GroupSubmodules {
		t.Error("step-summary group_submodules should have been overridden to true")
	}
	if step.SubmoduleDepth != 2 {
		t.Errorf("step-summary submodule_depth = %d, want 2", step.SubmoduleDepth)
	}
	if step.CodeFormat != "diff" {
		t.Errorf("step-summary code_format = %q, want diff (inherited)", step.CodeFormat)
	}

	// An un-overridden target inherits verbatim.
	md := cfg.EffectiveOutput("markdown")
	if md.MaxResourcesInSummary != 50 || md.CodeFormat != "diff" {
		t.Errorf("markdown drifted from global defaults: %+v", md)
	}
}

func TestParse_targetKnobOverrides(t *testing.T) {
	yaml := []byte(`
output:
  code_format: diff
  max_resources_in_summary: 50
  targets:
    github-pr-comment:
      code_format: plain
      max_resources_in_summary: 5
    github-step-summary:
      step_summary_max_kb: 400
      group_submodules: true
      submodule_depth: 2
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	comment := cfg.Output.Targets["github-pr-comment"]
	if comment.CodeFormat != "plain" {
		t.Errorf("pr-comment code_format = %q", comment.CodeFormat)
	}
	if comment.MaxResourcesInSummary == nil || *comment.MaxResourcesInSummary != 5 {
		t.Errorf("pr-comment max_resources_in_summary = %v", comment.MaxResourcesInSummary)
	}

	step := cfg.Output.Targets["github-step-summary"]
	if step.StepSummaryMaxKB == nil || *step.StepSummaryMaxKB != 400 {
		t.Errorf("step-summary step_summary_max_kb = %v", step.StepSummaryMaxKB)
	}
	if step.GroupSubmodules == nil || !*step.GroupSubmodules {
		t.Errorf("step-summary group_submodules = %v", step.GroupSubmodules)
	}
	if step.SubmoduleDepth == nil || *step.SubmoduleDepth != 2 {
		t.Errorf("step-summary submodule_depth = %v", step.SubmoduleDepth)
	}

	// EffectiveOutput composes correctly against the parsed config.
	eff := cfg.EffectiveOutput("github-pr-comment")
	if eff.CodeFormat != "plain" {
		t.Errorf("effective code_format = %q", eff.CodeFormat)
	}
	if eff.MaxResourcesInSummary != 5 {
		t.Errorf("effective max = %d", eff.MaxResourcesInSummary)
	}
}

func TestLoadDefault(t *testing.T) {
	cfg, _, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Output.MaxResourcesInSummary != 50 {
		t.Errorf("max resources = %d, want 50", cfg.Output.MaxResourcesInSummary)
	}
}
