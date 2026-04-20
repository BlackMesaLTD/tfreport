package presets

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// Load loads a preset by name. It first checks for a builtin preset,
// then falls back to loading from a file path.
func Load(name string) (*Preset, error) {
	// Try builtin first
	data, err := builtinFS.ReadFile("builtin/" + name + ".json")
	if err == nil {
		return parse(data)
	}

	// Try as file path
	data, err = os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("preset %q not found (checked builtin and file path): %w", name, err)
	}

	return parse(data)
}

// DisplayNames extracts a display name map from one or more presets,
// suitable for passing to the summarizer.
func DisplayNames(presets ...*Preset) map[string]string {
	names := make(map[string]string)
	for _, p := range presets {
		for resType, rp := range p.Resources {
			if rp.DisplayName != "" {
				names[resType] = rp.DisplayName
			}
		}
	}
	return names
}

// ForceNewResolver returns an AttributeResolver function that checks preset
// force_new data. If an attribute has force_new=true, it resolves to critical impact.
// Intended to be composed with config-based resolvers via ChainResolvers.
func ForceNewResolver(presets ...*Preset) func(resourceType, attrName string) (bool, bool) {
	return func(resourceType, attrName string) (bool, bool) {
		for _, p := range presets {
			if rp, ok := p.Resources[resourceType]; ok {
				if ap, ok := rp.Attributes[attrName]; ok {
					return ap.ForceNew, true
				}
			}
		}
		return false, false
	}
}

// DescriptionResolver returns a function that looks up attribute descriptions
// from presets. Returns empty string if no description is found.
func DescriptionResolver(presets ...*Preset) func(resourceType, attrName string) string {
	return func(resourceType, attrName string) string {
		for _, p := range presets {
			if rp, ok := p.Resources[resourceType]; ok {
				if ap, ok := rp.Attributes[attrName]; ok && ap.Description != "" {
					return ap.Description
				}
			}
		}
		return ""
	}
}

func parse(data []byte) (*Preset, error) {
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing preset: %w", err)
	}
	return &p, nil
}
