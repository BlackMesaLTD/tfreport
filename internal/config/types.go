package config

import "github.com/BlackMesaLTD/tfreport/internal/core"

// Config represents the .tfreport.yml configuration file.
type Config struct {
	Presets                []string                   `yaml:"presets"`
	Modules                map[string]ModuleConfig    `yaml:"modules"`
	ModuleDescriptionsFile string                     `yaml:"module_descriptions_file"`
	GlobalAttributes       map[string]AttributeConfig `yaml:"global_attributes"`
	Resources              map[string]ResourceConfig  `yaml:"resources"`
	ImpactDefaults         map[string]string          `yaml:"impact_defaults"`
	Output                 OutputConfig               `yaml:"output"`
}

// ResourceConfig provides per-resource-type overrides.
type ResourceConfig struct {
	DisplayName string                     `yaml:"display_name,omitempty"`
	Attributes  map[string]AttributeConfig `yaml:"attributes,omitempty"`
}

// ModuleConfig provides team-specific module descriptions.
type ModuleConfig struct {
	Description string `yaml:"description"`
}

// AttributeConfig provides team-specific attribute overrides.
type AttributeConfig struct {
	Impact string `yaml:"impact"`
	Note   string `yaml:"note"`
}

// OutputConfig controls output behavior.
type OutputConfig struct {
	MaxResourcesInSummary int                     `yaml:"max_resources_in_summary"`
	GroupSubmodules       bool                    `yaml:"group_submodules"`
	SubmoduleDepth        int                     `yaml:"submodule_depth"`
	StepSummaryMaxKB      int                     `yaml:"step_summary_max_kb"`
	CodeFormat            string                  `yaml:"code_format"`
	ChangedAttrsDisplay   string                  `yaml:"changed_attrs_display"`
	PreserveAttributes    []string                `yaml:"preserve_attributes"`
	// PreserveStrict controls how tfreport reacts when the body supplied via
	// --previous-body-file contains malformed preserve markers. Default false:
	// emit a ::warning:: to stderr, skip reconciliation, and render as if no
	// prior body was supplied. True: hard-fail with exit 1 so CI can gate on
	// corruption.
	PreserveStrict bool                    `yaml:"preserve_strict"`
	Targets        map[string]TargetConfig `yaml:"targets"`
}

// TargetConfig overrides the output template or section visibility for a
// single target (e.g. "github-step-summary"). Template and TemplateFile are
// mutually exclusive; both are mutually exclusive with Sections.
//
// The pointer-typed fields (MaxResourcesInSummary, GroupSubmodules,
// SubmoduleDepth, StepSummaryMaxKB) and the CodeFormat string override the
// matching fields on OutputConfig for this target only. A nil pointer / empty
// string means "inherit the global value"; use an explicit pointer-to-zero
// (via YAML) to override to zero.
type TargetConfig struct {
	Template     string         `yaml:"template"`
	TemplateFile string         `yaml:"template_file"`
	Sections     SectionsConfig `yaml:"sections"`

	MaxResourcesInSummary *int   `yaml:"max_resources_in_summary,omitempty"`
	GroupSubmodules       *bool  `yaml:"group_submodules,omitempty"`
	SubmoduleDepth        *int   `yaml:"submodule_depth,omitempty"`
	StepSummaryMaxKB      *int   `yaml:"step_summary_max_kb,omitempty"`
	CodeFormat            string `yaml:"code_format,omitempty"`
	ChangedAttrsDisplay   string `yaml:"changed_attrs_display,omitempty"`
}

// SectionsConfig enables the simple "toggle sections on/off" mode against
// the default template. Show is a whitelist; Hide is a blacklist; they are
// mutually exclusive.
type SectionsConfig struct {
	Show []string `yaml:"show"`
	Hide []string `yaml:"hide"`
}

// IsZero reports whether the TargetConfig is entirely unset.
func (t TargetConfig) IsZero() bool {
	return t.Template == "" &&
		t.TemplateFile == "" &&
		t.Sections.IsZero() &&
		t.MaxResourcesInSummary == nil &&
		t.GroupSubmodules == nil &&
		t.SubmoduleDepth == nil &&
		t.StepSummaryMaxKB == nil &&
		t.CodeFormat == "" &&
		t.ChangedAttrsDisplay == ""
}

// IsZero reports whether no section filtering was configured.
func (s SectionsConfig) IsZero() bool {
	return len(s.Show) == 0 && len(s.Hide) == 0
}

// Default returns a Config with sensible defaults.
func Default() Config {
	return Config{
		ImpactDefaults: map[string]string{
			"replace": "critical",
			"delete":  "high",
			"update":  "medium",
			"create":  "low",
		},
		Output: OutputConfig{
			MaxResourcesInSummary: 50,
			CodeFormat:            "diff",
		},
	}
}

// EffectiveOutput returns the OutputConfig for a given target, with any
// per-target overrides applied on top of the global output.* values.
// Resolution order (highest wins): output.targets.<target>.<knob> >
// output.<knob> > zero value (caller-supplied fallback when unset).
func (c Config) EffectiveOutput(target string) OutputConfig {
	out := c.Output
	tc, ok := c.Output.Targets[target]
	if !ok {
		return out
	}
	if tc.MaxResourcesInSummary != nil {
		out.MaxResourcesInSummary = *tc.MaxResourcesInSummary
	}
	if tc.GroupSubmodules != nil {
		out.GroupSubmodules = *tc.GroupSubmodules
	}
	if tc.SubmoduleDepth != nil {
		out.SubmoduleDepth = *tc.SubmoduleDepth
	}
	if tc.StepSummaryMaxKB != nil {
		out.StepSummaryMaxKB = *tc.StepSummaryMaxKB
	}
	if tc.CodeFormat != "" {
		out.CodeFormat = tc.CodeFormat
	}
	if tc.ChangedAttrsDisplay != "" {
		out.ChangedAttrsDisplay = tc.ChangedAttrsDisplay
	}
	return out
}

// ImpactOverrides converts the config impact_defaults into a map usable by the classifier.
func (c Config) ImpactOverrides() map[core.Action]core.Impact {
	if len(c.ImpactDefaults) == 0 {
		return nil
	}

	overrides := make(map[core.Action]core.Impact)
	actionMap := map[string]core.Action{
		"replace": core.ActionReplace,
		"delete":  core.ActionDelete,
		"update":  core.ActionUpdate,
		"create":  core.ActionCreate,
		"read":    core.ActionRead,
	}
	impactMap := map[string]core.Impact{
		"critical": core.ImpactCritical,
		"high":     core.ImpactHigh,
		"medium":   core.ImpactMedium,
		"low":      core.ImpactLow,
		"none":     core.ImpactNone,
	}

	for actionStr, impactStr := range c.ImpactDefaults {
		action, aOk := actionMap[actionStr]
		impact, iOk := impactMap[impactStr]
		if aOk && iOk {
			overrides[action] = impact
		}
	}

	return overrides
}

// ModuleDescription returns the configured description for a module, or empty string.
func (c Config) ModuleDescription(moduleName string) string {
	if mc, ok := c.Modules[moduleName]; ok {
		return mc.Description
	}
	return ""
}

// AttributeImpact resolves the impact override for a specific resource type
// and attribute name. Resolution order:
//  1. resources.<resourceType>.attributes.<attrName> (most specific)
//  2. global_attributes.<attrName>
//  3. Not found (returns "", false)
func (c Config) AttributeImpact(resourceType, attrName string) (core.Impact, bool) {
	impactMap := map[string]core.Impact{
		"critical": core.ImpactCritical,
		"high":     core.ImpactHigh,
		"medium":   core.ImpactMedium,
		"low":      core.ImpactLow,
		"none":     core.ImpactNone,
	}

	// Check resource-specific override first
	if rc, ok := c.Resources[resourceType]; ok {
		if ac, ok := rc.Attributes[attrName]; ok && ac.Impact != "" {
			if impact, ok := impactMap[ac.Impact]; ok {
				return impact, true
			}
		}
	}

	// Check global attributes
	globals := c.GlobalAttributes
	if ac, ok := globals[attrName]; ok && ac.Impact != "" {
		if impact, ok := impactMap[ac.Impact]; ok {
			return impact, true
		}
	}

	return "", false
}

// ResourceDisplayName returns a display name override from config resources, if any.
func (c Config) ResourceDisplayName(resourceType string) string {
	if rc, ok := c.Resources[resourceType]; ok {
		return rc.DisplayName
	}
	return ""
}

// AttributeNoteResolver returns a function that looks up attribute notes
// from config. Resolution order: resource-specific → global_attributes.
func (c Config) AttributeNoteResolver() func(resourceType, attrName string) string {
	return func(resourceType, attrName string) string {
		// Check resource-specific note first
		if rc, ok := c.Resources[resourceType]; ok {
			if ac, ok := rc.Attributes[attrName]; ok && ac.Note != "" {
				return ac.Note
			}
		}

		// Check global attributes
		globals := c.GlobalAttributes
		if ac, ok := globals[attrName]; ok && ac.Note != "" {
			return ac.Note
		}

		return ""
	}
}
