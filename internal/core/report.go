package core

import "fmt"

// GenerateReport runs the full core pipeline:
// parse -> diff -> group -> classify -> summarize -> report
func GenerateReport(planJSON []byte, opts ReportOptions) (*Report, error) {
	// Step 1: Parse plan JSON
	changes, err := ParsePlan(planJSON)
	if err != nil {
		return nil, err
	}

	// Step 2: Filter no-ops if requested
	if opts.ChangedOnly {
		changes = filterChanged(changes)
	}

	// Step 3: Diff each resource's before/after
	for i := range changes {
		rc := &changes[i]
		rc.ChangedAttributes = Diff(rc.Before, rc.After, rc.AfterUnknown, rc.BeforeSensitive, rc.AfterSensitive)
	}

	// Step 3b: Populate attribute descriptions from presets
	if opts.DescriptionResolver != nil {
		for i := range changes {
			for j := range changes[i].ChangedAttributes {
				desc := opts.DescriptionResolver(changes[i].ResourceType, changes[i].ChangedAttributes[j].Key)
				changes[i].ChangedAttributes[j].Description = desc
			}
		}
	}

	// Step 4: Classify impact
	ClassifyImpact(changes, opts.ImpactOverrides, opts.AttributeResolver)

	// Step 4b: Pre-compute display labels (from Before/After "name" attr)
	for i := range changes {
		changes[i].DisplayLabel = ResourceDisplayLabel(changes[i])
	}

	// Step 5: Group by module
	groups := GroupByModule(changes)

	// Step 5b: Disambiguate colliding module names
	DisambiguateNames(groups)

	// Step 6: Apply module descriptions from config
	for i := range groups {
		if desc, ok := opts.ModuleDescriptions[groups[i].Name]; ok {
			groups[i].Description = desc
		}
	}

	// Step 7: Summarize per group and overall
	var allKeyChanges []KeyChange
	for i := range groups {
		groupSentences := Summarize(groups[i].Changes, opts.DisplayNames)
		allKeyChanges = append(allKeyChanges, groupSentences...)
	}

	// Step 7b: Deduplicate key changes — identical sentences from multiple
	// modules are collapsed with a count suffix. Max impact is preserved
	// across collisions.
	allKeyChanges = deduplicateKeyChanges(allKeyChanges)

	// Step 8: Aggregate
	totalCounts := TotalActionCounts(groups)
	totalResources := 0
	for _, c := range totalCounts {
		totalResources += c
	}

	// Step 9: Extract module sources for type grouping
	moduleSources := ParseModuleSources(planJSON)

	return &Report{
		ModuleGroups:   groups,
		KeyChanges:     allKeyChanges,
		TotalResources: totalResources,
		ActionCounts:   totalCounts,
		MaxImpact:      MaxImpactOverall(groups),
		ModuleSources:  moduleSources,
		DisplayNames:   opts.DisplayNames,
	}, nil
}

// ReportOptions configures the report generation pipeline.
type ReportOptions struct {
	ChangedOnly         bool
	ImpactOverrides     map[Action]Impact
	AttributeResolver   AttributeResolver
	ModuleDescriptions  map[string]string
	DisplayNames        map[string]string
	DescriptionResolver func(resourceType, attrName string) string
}

// deduplicateKeyChanges collapses repeated sentences. If the same Text
// appears N times (from N modules), it's kept once with " (×N modules)"
// appended and the worst-case Impact is retained.
func deduplicateKeyChanges(entries []KeyChange) []KeyChange {
	if len(entries) == 0 {
		return nil
	}

	counts := make(map[string]int)
	impacts := make(map[string]Impact)
	var order []string
	for _, kc := range entries {
		if counts[kc.Text] == 0 {
			order = append(order, kc.Text)
		}
		counts[kc.Text]++
		if ImpactSeverity(kc.Impact) > ImpactSeverity(impacts[kc.Text]) {
			impacts[kc.Text] = kc.Impact
		}
	}

	result := make([]KeyChange, 0, len(order))
	for _, text := range order {
		out := text
		if counts[text] > 1 {
			out = fmt.Sprintf("%s (×%d modules)", text, counts[text])
		}
		result = append(result, KeyChange{Text: out, Impact: impacts[text]})
	}
	return result
}

func filterChanged(changes []ResourceChange) []ResourceChange {
	var filtered []ResourceChange
	for _, rc := range changes {
		if rc.Action != ActionNoOp {
			filtered = append(filtered, rc)
		}
	}
	return filtered
}
