package presetgen

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/BlackMesaLTD/tfreport/internal/presets"
)

// MergeOptions controls how presets are merged.
type MergeOptions struct {
	Provider       string
	Version        string
	ExistingPreset *presets.Preset            // existing preset to merge with (preserves display_names)
	DocsParsed     map[string]*ParsedResource // from ParseDocsDir
	SchemaFile     string                     // optional: path to terraform providers schema -json output
}

// Merge combines doc-parsed data, optional schema data, and an optional existing
// preset into a single enriched Preset.
func Merge(opts MergeOptions) (*presets.Preset, error) {
	result := &presets.Preset{
		Provider:  opts.Provider,
		Version:   opts.Version,
		Resources: make(map[string]presets.ResourcePreset),
	}

	// Start with existing preset data (preserves display_names and any manual entries)
	if opts.ExistingPreset != nil {
		for resType, rp := range opts.ExistingPreset.Resources {
			result.Resources[resType] = rp
		}
		if result.Provider == "" {
			result.Provider = opts.ExistingPreset.Provider
		}
		if result.Version == "" {
			result.Version = opts.ExistingPreset.Version
		}
	}

	// Overlay doc-parsed attribute data
	if opts.DocsParsed != nil {
		docResources := ToPresetResources(opts.DocsParsed)
		for resType, docRP := range docResources {
			existing, ok := result.Resources[resType]
			if !ok {
				existing = presets.ResourcePreset{}
			}

			// Merge attributes: doc data fills in, doesn't overwrite existing
			if existing.Attributes == nil {
				existing.Attributes = make(map[string]presets.AttributePreset)
			}
			for attrName, docAttr := range docRP.Attributes {
				if _, exists := existing.Attributes[attrName]; !exists {
					existing.Attributes[attrName] = docAttr
				} else {
					// Overlay: fill in missing fields from doc
					ea := existing.Attributes[attrName]
					if ea.Description == "" {
						ea.Description = docAttr.Description
					}
					if !ea.ForceNew && docAttr.ForceNew {
						ea.ForceNew = true
					}
					existing.Attributes[attrName] = ea
				}
			}

			result.Resources[resType] = existing
		}
	}

	// Overlay terraform providers schema data if provided
	if opts.SchemaFile != "" {
		if err := overlaySchemaData(result, opts.SchemaFile, opts.Provider); err != nil {
			return nil, fmt.Errorf("overlaying schema data: %w", err)
		}
	}

	return result, nil
}

// MarshalPreset serializes a Preset to sorted, indented JSON.
func MarshalPreset(p *presets.Preset) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// providerSchema represents the relevant parts of terraform providers schema -json.
type providerSchema struct {
	ProviderSchemas map[string]providerSchemaEntry `json:"provider_schemas"`
}

type providerSchemaEntry struct {
	ResourceSchemas map[string]resourceSchema `json:"resource_schemas"`
}

type resourceSchema struct {
	Block blockSchema `json:"block"`
}

type blockSchema struct {
	Attributes map[string]schemaAttribute `json:"attributes"`
}

type schemaAttribute struct {
	Type        any    `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Optional    bool   `json:"optional"`
	Computed    bool   `json:"computed"`
	Sensitive   bool   `json:"sensitive"`
}

func overlaySchemaData(result *presets.Preset, schemaFile string, provider string) error {
	data, err := os.ReadFile(schemaFile)
	if err != nil {
		return fmt.Errorf("reading schema file: %w", err)
	}

	var schema providerSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("parsing schema JSON: %w", err)
	}

	// Find the provider entry (key format varies: "registry.terraform.io/hashicorp/azurerm" or just "azurerm")
	for providerKey, entry := range schema.ProviderSchemas {
		if !matchesProvider(providerKey, provider) {
			continue
		}

		for resType, resSch := range entry.ResourceSchemas {
			existing, ok := result.Resources[resType]
			if !ok {
				continue // only enrich resources we already know about
			}

			if existing.Attributes == nil {
				existing.Attributes = make(map[string]presets.AttributePreset)
			}

			for attrName, schAttr := range resSch.Block.Attributes {
				ea, exists := existing.Attributes[attrName]
				if !exists {
					ea = presets.AttributePreset{}
				}

				// Schema description fills in if doc didn't have one
				if ea.Description == "" && schAttr.Description != "" {
					ea.Description = schAttr.Description
				}

				if ea != (presets.AttributePreset{}) {
					existing.Attributes[attrName] = ea
				}
			}

			result.Resources[resType] = existing
		}
	}

	return nil
}

func matchesProvider(key, provider string) bool {
	// Exact match
	if key == provider {
		return true
	}
	// Registry format: registry.terraform.io/hashicorp/azurerm
	return len(key) > len(provider) && key[len(key)-len(provider):] == provider
}

// SortedResourceTypes returns resource types sorted alphabetically.
func SortedResourceTypes(p *presets.Preset) []string {
	types := make([]string, 0, len(p.Resources))
	for t := range p.Resources {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
