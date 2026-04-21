package core

import "encoding/json"

// reportJSON is the complete serializable form of a Report.
// This is the canonical tfreport interchange format.
type reportJSON struct {
	Label          string            `json:"label,omitempty"`
	ModuleGroups   []moduleGroupJSON `json:"module_groups"`
	KeyChanges     []keyChangeJSON   `json:"key_changes,omitempty"`
	TotalResources int               `json:"total_resources"`
	ActionCounts   map[string]int    `json:"action_counts"`
	MaxImpact      string            `json:"max_impact"`
	ModuleSources  map[string]string `json:"module_sources,omitempty"`
	TextPlanBlocks map[string]string `json:"text_plan_blocks,omitempty"`
	DisplayNames   map[string]string `json:"display_names,omitempty"`
	Custom         map[string]string `json:"custom,omitempty"`
}

type keyChangeJSON struct {
	Text   string `json:"text"`
	Impact string `json:"impact,omitempty"`
}

type moduleGroupJSON struct {
	Name         string         `json:"name"`
	Path         string         `json:"path"`
	Description  string         `json:"description,omitempty"`
	ActionCounts map[string]int `json:"action_counts"`
	Changes      []resourceJSON `json:"changes"`
}

type resourceJSON struct {
	Address           string            `json:"address"`
	ModulePath        string            `json:"module_path"`
	ResourceType      string            `json:"resource_type"`
	ResourceName      string            `json:"resource_name"`
	Action            string            `json:"action"`
	Impact            string            `json:"impact"`
	IsImport          bool              `json:"is_import,omitempty"`
	DisplayLabel      string            `json:"display_label,omitempty"`
	ChangedAttributes []changedAttrJSON `json:"changed_attributes,omitempty"`
}

type changedAttrJSON struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

// MarshalReport serializes a Report to JSON.
func MarshalReport(r *Report) ([]byte, error) {
	jr := reportJSON{
		Label:          r.Label,
		TotalResources: r.TotalResources,
		ActionCounts:   stringifyActionCounts(r.ActionCounts),
		MaxImpact:      string(r.MaxImpact),
		KeyChanges:     marshalKeyChanges(r.KeyChanges),
		ModuleSources:  r.ModuleSources,
		TextPlanBlocks: r.TextPlanBlocks,
		DisplayNames:   r.DisplayNames,
		Custom:         r.Custom,
		ModuleGroups:   make([]moduleGroupJSON, len(r.ModuleGroups)),
	}

	for i, mg := range r.ModuleGroups {
		jmg := moduleGroupJSON{
			Name:         mg.Name,
			Path:         mg.Path,
			Description:  mg.Description,
			ActionCounts: stringifyActionCounts(mg.ActionCounts),
			Changes:      make([]resourceJSON, len(mg.Changes)),
		}
		for j, rc := range mg.Changes {
			jmg.Changes[j] = marshalResource(rc)
		}
		jr.ModuleGroups[i] = jmg
	}

	return json.MarshalIndent(jr, "", "  ")
}

// UnmarshalReport deserializes a Report from JSON.
func UnmarshalReport(data []byte) (*Report, error) {
	var jr reportJSON
	if err := json.Unmarshal(data, &jr); err != nil {
		return nil, err
	}

	r := &Report{
		Label:          jr.Label,
		TotalResources: jr.TotalResources,
		ActionCounts:   parseActionCounts(jr.ActionCounts),
		MaxImpact:      Impact(jr.MaxImpact),
		KeyChanges:     unmarshalKeyChanges(jr.KeyChanges),
		ModuleSources:  jr.ModuleSources,
		TextPlanBlocks: jr.TextPlanBlocks,
		DisplayNames:   jr.DisplayNames,
		Custom:         jr.Custom,
		ModuleGroups:   make([]ModuleGroup, len(jr.ModuleGroups)),
	}

	for i, jmg := range jr.ModuleGroups {
		mg := ModuleGroup{
			Name:         jmg.Name,
			Path:         jmg.Path,
			Description:  jmg.Description,
			ActionCounts: parseActionCounts(jmg.ActionCounts),
			Changes:      make([]ResourceChange, len(jmg.Changes)),
		}
		for j, jrc := range jmg.Changes {
			mg.Changes[j] = unmarshalResource(jrc)
		}
		r.ModuleGroups[i] = mg
	}

	return r, nil
}

func marshalResource(rc ResourceChange) resourceJSON {
	jr := resourceJSON{
		Address:      rc.Address,
		ModulePath:   rc.ModulePath,
		ResourceType: rc.ResourceType,
		ResourceName: rc.ResourceName,
		Action:       string(rc.Action),
		Impact:       string(rc.Impact),
		IsImport:     rc.IsImport,
		DisplayLabel: rc.DisplayLabel,
	}
	for _, attr := range rc.ChangedAttributes {
		jr.ChangedAttributes = append(jr.ChangedAttributes, changedAttrJSON{
			Key:         attr.Key,
			Description: attr.Description,
			Sensitive:   attr.Sensitive,
		})
	}
	return jr
}

func unmarshalResource(jr resourceJSON) ResourceChange {
	rc := ResourceChange{
		Address:      jr.Address,
		ModulePath:   jr.ModulePath,
		ResourceType: jr.ResourceType,
		ResourceName: jr.ResourceName,
		Action:       Action(jr.Action),
		Impact:       Impact(jr.Impact),
		IsImport:     jr.IsImport,
		DisplayLabel: jr.DisplayLabel,
	}
	for _, ja := range jr.ChangedAttributes {
		rc.ChangedAttributes = append(rc.ChangedAttributes, ChangedAttribute{
			Key:         ja.Key,
			Description: ja.Description,
			Sensitive:   ja.Sensitive,
		})
	}
	return rc
}

func marshalKeyChanges(entries []KeyChange) []keyChangeJSON {
	if len(entries) == 0 {
		return nil
	}
	out := make([]keyChangeJSON, len(entries))
	for i, kc := range entries {
		out[i] = keyChangeJSON{Text: kc.Text, Impact: string(kc.Impact)}
	}
	return out
}

func unmarshalKeyChanges(entries []keyChangeJSON) []KeyChange {
	if len(entries) == 0 {
		return nil
	}
	out := make([]KeyChange, len(entries))
	for i, kc := range entries {
		out[i] = KeyChange{Text: kc.Text, Impact: Impact(kc.Impact)}
	}
	return out
}

func stringifyActionCounts(counts map[Action]int) map[string]int {
	if counts == nil {
		return nil
	}
	result := make(map[string]int, len(counts))
	for k, v := range counts {
		result[string(k)] = v
	}
	return result
}

func parseActionCounts(counts map[string]int) map[Action]int {
	if counts == nil {
		return nil
	}
	result := make(map[Action]int, len(counts))
	for k, v := range counts {
		result[Action(k)] = v
	}
	return result
}
