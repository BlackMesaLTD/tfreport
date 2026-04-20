package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// planJSON represents the top-level structure of terraform show -json output.
type planJSON struct {
	FormatVersion    string               `json:"format_version"`
	TerraformVersion string               `json:"terraform_version"`
	ResourceChanges  []resourceChangeJSON `json:"resource_changes"`
	Configuration    configJSON           `json:"configuration"`
}

type resourceChangeJSON struct {
	Address       string     `json:"address"`
	ModuleAddress string     `json:"module_address"`
	Type          string     `json:"type"`
	Name          string     `json:"name"`
	ProviderName  string     `json:"provider_name"`
	Change        changeJSON `json:"change"`
}

type changeJSON struct {
	Actions         []string        `json:"actions"`
	Before          map[string]any  `json:"before"`
	After           map[string]any  `json:"after"`
	AfterUnknown    any             `json:"after_unknown"`
	BeforeSensitive any             `json:"before_sensitive"`
	AfterSensitive  any             `json:"after_sensitive"`
	Importing       *importingJSON  `json:"importing,omitempty"`
}

// importingJSON mirrors terraform's import marker. Presence of any
// `importing` object on a change means the resource is being imported
// into state; absence means not an import. Orthogonal to Actions: a
// resource can be imported AND updated, imported AND unchanged, etc.
type importingJSON struct {
	ID string `json:"id"`
}

type configJSON struct {
	RootModule rootModuleJSON `json:"root_module"`
}

type rootModuleJSON struct {
	ModuleCalls map[string]moduleCallJSON `json:"module_calls"`
}

type moduleCallJSON struct {
	Source string `json:"source"`
	Module any    `json:"module"`
}

// ParsePlan parses terraform plan JSON bytes into a slice of ResourceChange.
func ParsePlan(data []byte) ([]ResourceChange, error) {
	var plan planJSON
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing plan JSON: %w", err)
	}

	if plan.FormatVersion == "" {
		return nil, fmt.Errorf("missing format_version: input may not be terraform plan JSON")
	}

	changes := make([]ResourceChange, 0, len(plan.ResourceChanges))
	for _, rc := range plan.ResourceChanges {
		action := mapActions(rc.Change.Actions)

		afterUnknown := toStringAnyMap(rc.Change.AfterUnknown)

		change := ResourceChange{
			Address:         rc.Address,
			ModulePath:      rc.ModuleAddress,
			ResourceType:    rc.Type,
			ResourceName:    rc.Name,
			ProviderName:    rc.ProviderName,
			Action:          action,
			Impact:          defaultImpact(action),
			IsImport:        rc.Change.Importing != nil,
			Before:          rc.Change.Before,
			After:           rc.Change.After,
			AfterUnknown:    afterUnknown,
			BeforeSensitive: rc.Change.BeforeSensitive,
			AfterSensitive:  rc.Change.AfterSensitive,
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// mapActions converts the terraform plan action array to an Action enum.
// Plan JSON uses arrays like ["create"], ["update"], ["delete", "create"] (replace).
func mapActions(actions []string) Action {
	if len(actions) == 0 {
		return ActionNoOp
	}

	if len(actions) == 1 {
		switch actions[0] {
		case "create":
			return ActionCreate
		case "update":
			return ActionUpdate
		case "delete":
			return ActionDelete
		case "read":
			return ActionRead
		case "no-op":
			return ActionNoOp
		}
	}

	// ["delete", "create"] = replace (force new)
	if len(actions) == 2 {
		hasDelete := actions[0] == "delete" || actions[1] == "delete"
		hasCreate := actions[0] == "create" || actions[1] == "create"
		if hasDelete && hasCreate {
			return ActionReplace
		}
	}

	return ActionNoOp
}

// extractModulePath extracts the module path from a resource address.
// "module.virtual_network.azurerm_subnet.app" -> "module.virtual_network"
// "azurerm_subnet.app" -> "" (root module)
// "module.a.module.b.azurerm_subnet.app" -> "module.a.module.b"
func extractModulePath(address string) string {
	// Find the last occurrence of a resource type pattern (word.word at the end)
	parts := strings.Split(address, ".")
	if len(parts) < 2 {
		return ""
	}

	// Walk backwards to find where the resource type starts.
	// Module paths always have "module" as a segment.
	// The resource portion is always type.name (last two parts).
	// But resource type can contain underscores, not dots.
	// So the module path is everything before the last type.name pair.

	// Find the last "module." segment by looking for the resource boundary.
	// The resource address format is: [module.name[.module.name...].]type.name
	// Where type never starts with "module".

	// Strategy: walk from the end. The last two segments are type.name.
	// Everything before that is the module path.
	// But we need to handle indexed resources: module.foo.azurerm_subnet.this["key"]

	// Simple approach: find last occurrence of a segment that looks like a
	// terraform resource type (contains underscore or starts with a provider prefix).
	// Actually, the simplest correct approach: module paths consist of
	// "module.X" pairs. Find the last "module.X" pair boundary.

	moduleEnd := -1
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "module" {
			moduleEnd = i + 1 // include the module name
		}
	}

	if moduleEnd < 0 {
		return ""
	}

	return strings.Join(parts[:moduleEnd+1], ".")
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
// from the plan JSON's configuration.root_module.module_calls section.
// Returns an empty map if the configuration section is absent or has no module calls.
func ParseModuleSources(data []byte) map[string]string {
	var plan planJSON
	if err := json.Unmarshal(data, &plan); err != nil {
		return map[string]string{}
	}

	calls := plan.Configuration.RootModule.ModuleCalls
	if len(calls) == 0 {
		return map[string]string{}
	}

	sources := make(map[string]string, len(calls))
	for name, mc := range calls {
		if mc.Source != "" {
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
func toStringAnyMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
