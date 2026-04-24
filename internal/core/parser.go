package core

import (
	"encoding/json"
	"fmt"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
)

// ParsePlan parses terraform plan JSON bytes into a slice of ResourceChange.
// Backed by hashicorp/terraform-json so format-version validation and action
// classification track upstream Terraform.
func ParsePlan(data []byte) ([]ResourceChange, error) {
	plan, err := decodePlan(data)
	if err != nil {
		return nil, err
	}

	changes := make([]ResourceChange, 0, len(plan.ResourceChanges))
	for _, rc := range plan.ResourceChanges {
		if rc == nil || rc.Change == nil {
			continue
		}
		action := mapTfAction(rc.Change.Actions)
		changes = append(changes, ResourceChange{
			Address:         rc.Address,
			ModulePath:      rc.ModuleAddress,
			ResourceType:    rc.Type,
			ResourceName:    rc.Name,
			ProviderName:    rc.ProviderName,
			Action:          action,
			Impact:          defaultImpact(action),
			IsImport:        rc.Change.Importing != nil,
			Before:          toStringAnyMap(rc.Change.Before),
			After:           toStringAnyMap(rc.Change.After),
			AfterUnknown:    toStringAnyMap(rc.Change.AfterUnknown),
			BeforeSensitive: rc.Change.BeforeSensitive,
			AfterSensitive:  rc.Change.AfterSensitive,
		})
	}

	return changes, nil
}

// decodePlan unmarshals JSON and runs terraform-json's format-version check.
// A missing format_version is surfaced with a human-facing hint because users
// frequently pipe the wrong file (terraform output, terraform show without
// -json, etc.) and the default json-library error is opaque.
func decodePlan(data []byte) (*tfjson.Plan, error) {
	var plan tfjson.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing plan JSON: %w", err)
	}
	if plan.FormatVersion == "" {
		return nil, fmt.Errorf("missing format_version: input may not be terraform plan JSON")
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("plan format not supported: %w", err)
	}
	return &plan, nil
}

// mapTfAction collapses terraform-json's Actions slice into our enum.
// Order matters: Replace() must be checked before the individual helpers
// because a replace plan satisfies both Delete() and Create().
func mapTfAction(actions tfjson.Actions) Action {
	switch {
	case actions.Replace():
		return ActionReplace
	case actions.Create():
		return ActionCreate
	case actions.Update():
		return ActionUpdate
	case actions.Delete():
		return ActionDelete
	case actions.Read():
		return ActionRead
	default:
		return ActionNoOp
	}
}

// defaultImpact returns the default impact classification for an action.
func defaultImpact(action Action) Impact {
	switch action {
	case ActionReplace:
		return ImpactCritical
	case ActionDelete:
		return ImpactHigh
	case ActionUpdate:
		return ImpactMedium
	case ActionCreate, ActionRead:
		return ImpactLow
	default:
		return ImpactNone
	}
}

// ParseModuleSources extracts top-level module call names to source URLs
// from the plan's configuration.root_module.module_calls section.
// Returns an empty map if the configuration section is absent or has no
// module calls. Errors during decode are swallowed — callers expect a
// map and module sources are enrichment, not load-bearing.
func ParseModuleSources(data []byte) map[string]string {
	var plan tfjson.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return map[string]string{}
	}
	if plan.Config == nil || plan.Config.RootModule == nil {
		return map[string]string{}
	}
	calls := plan.Config.RootModule.ModuleCalls
	sources := make(map[string]string, len(calls))
	for name, mc := range calls {
		if mc != nil && mc.Source != "" {
			sources[name] = mc.Source
		}
	}
	return sources
}

// ExtractModuleType parses a module source URL and extracts a short module type name.
//
// Supported formats:
//   - Git URL with //modules/X path: "git::https://...//modules/virtual_network?ref=v1" → "virtual_network"
//   - Local path: "./modules/foo" → "foo"
//   - Registry: "hashicorp/consul/aws" → "consul" (middle segment)
//   - Empty string: returns ""
//   - Fallback: returns the last path segment (before any query string)
func ExtractModuleType(source string) string {
	if source == "" {
		return ""
	}

	// Git URL with double-slash subpath: extract from //modules/X or last segment before ?
	if strings.Contains(source, "//") {
		// Split off query string (?ref=...)
		path := source
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		// Find the double-slash subpath
		if idx := strings.Index(path, "//"); idx != -1 {
			subpath := path[idx+2:]
			parts := strings.Split(subpath, "/")
			// Return last non-empty segment
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] != "" {
					return parts[i]
				}
			}
		}
	}

	// Registry format: namespace/name/provider (3 segments, no path separators like ./ or /)
	// e.g. "hashicorp/consul/aws" → "consul"
	if !strings.HasPrefix(source, ".") && !strings.HasPrefix(source, "/") && !strings.Contains(source, "::") {
		parts := strings.Split(source, "/")
		if len(parts) == 3 {
			return parts[1]
		}
	}

	// Local path or other: strip query string, return last path segment
	path := source
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}

	return ""
}

// toStringAnyMap converts an any value to map[string]any if possible.
// terraform-json decodes JSON objects to map[string]interface{}, which is
// the same underlying type — this assertion is cheap and preserves the
// existing downstream contract with differ / preserve / summarizer.
func toStringAnyMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
