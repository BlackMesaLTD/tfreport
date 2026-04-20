package presets

// Preset represents a provider preset file.
type Preset struct {
	Provider  string                    `json:"provider"`
	Version   string                    `json:"version"`
	Resources map[string]ResourcePreset `json:"resources"`
}

// ResourcePreset contains enrichment data for a resource type.
type ResourcePreset struct {
	DisplayName string                     `json:"display_name"`
	Attributes  map[string]AttributePreset `json:"attributes,omitempty"`
}

// AttributePreset contains enrichment data for an attribute.
type AttributePreset struct {
	Description string `json:"description,omitempty"`
	ForceNew    bool   `json:"force_new,omitempty"`
}
